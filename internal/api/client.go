package api

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	positionsPath       = "/api/v0/equity/positions"
	orderHistoryPath    = "/api/v0/equity/history/orders"
	dividendHistoryPath = "/api/v0/equity/history/dividends"
	instrumentsPath     = "/api/v0/equity/metadata/instruments"
	exchangesPath       = "/api/v0/equity/metadata/exchanges"
	historyPageLimit    = 50
	historyRateDelay    = 11 * time.Second // stay under 6 req/min
)

// Client is a Trading 212 API client.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	authHeader  string
	instruments map[string]InstrumentMeta // ticker → metadata
	exchanges   map[int]string            // workingScheduleId → exchange name
}

// NewClient creates a Client. apiKeyID and apiSecret are combined as HTTP Basic auth.
// If httpClient is nil, a default client with TLS 1.3 is used.
func NewClient(apiKeyID, apiSecret, baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS13,
				},
			},
		}
	}
	auth := base64.StdEncoding.EncodeToString([]byte(apiKeyID + ":" + apiSecret))
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		authHeader: "Basic " + auth,
	}
}

// FetchPositions calls GET /api/v0/equity/positions and returns parsed positions
// plus rate limit metadata. Positions have Compute() called before return.
func (c *Client) FetchPositions(ctx context.Context) ([]Position, RateLimitInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+positionsPath, nil)
	if err != nil {
		return nil, RateLimitInfo{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, RateLimitInfo{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, RateLimitInfo{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	// The T212 API wire format nests the ticker inside an "instrument" object
	// and uses "averagePricePaid" for the average price.
	// UK/LSE instruments (ticker ending in _EQ without a country code) are
	// priced in GBX (pence); prices are divided by 100 to normalise to GBP.
	type wirePosition struct {
		Instrument struct {
			Ticker string `json:"ticker"`
			Name   string `json:"name"`
		} `json:"instrument"`
		Quantity     float64 `json:"quantity"`
		AveragePrice float64 `json:"averagePricePaid"`
		CurrentPrice float64 `json:"currentPrice"`
		WalletImpact struct {
			CurrentValue float64 `json:"currentValue"`
		} `json:"walletImpact"`
	}
	var raw []wirePosition
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, RateLimitInfo{}, fmt.Errorf("decode response: %w", err)
	}

	positions := make([]Position, len(raw))
	for i, r := range raw {
		avg := r.AveragePrice
		curr := r.CurrentPrice

		currency := ""
		exchange := ""
		if meta, ok := c.instruments[r.Instrument.Ticker]; ok {
			currency = meta.CurrencyCode
			if name, ok := c.exchanges[meta.WorkingScheduleID]; ok {
				exchange = name
			}
		}
		if currency == "" {
			currency = inferCurrency(r.Instrument.Ticker) // fallback
		}

		if currency == "GBX" {
			avg /= 100
			curr /= 100
			currency = "GBP"
		}
		positions[i] = Position{
			Ticker:          r.Instrument.Ticker,
			Name:            r.Instrument.Name,
			Currency:        currency,
			Exchange:        exchange,
			Quantity:        r.Quantity,
			AveragePrice:    avg,
			CurrentPrice:    curr,
			CurrentValueGBP: r.WalletImpact.CurrentValue,
		}
		positions[i].Compute()
	}

	rl := parseRateLimit(resp)
	return positions, rl, nil
}

// inferCurrency determines the currency from a T212 ticker suffix.
// UK/LSE tickers use the pattern SYMBOL_EQ and are priced in GBX (pence).
// Other exchanges include a 2-letter country code: SYMBOL_US_EQ (USD), etc.
func inferCurrency(ticker string) string {
	if !strings.HasSuffix(ticker, "_EQ") {
		return ""
	}
	base := strings.TrimSuffix(ticker, "_EQ")
	if i := strings.LastIndex(base, "_"); i >= 0 {
		cc := base[i+1:]
		if len(cc) == 2 {
			switch cc {
			case "US":
				return "USD"
			case "DE", "FR", "NL":
				return "EUR"
			}
			return ""
		}
	}
	// No country code before _EQ → UK/LSE → GBX
	return "GBX"
}

// shortExchangeName abbreviates well-known exchange names for display.
var exchangeAbbreviations = map[string]string{
	"London Stock Exchange": "LSE",
}

func shortExchangeName(name string) string {
	if short, ok := exchangeAbbreviations[name]; ok {
		return short
	}
	return name
}

func parseRateLimit(resp *http.Response) RateLimitInfo {
	remaining, _ := strconv.Atoi(resp.Header.Get("x-ratelimit-remaining"))
	resetUnix, _ := strconv.ParseInt(resp.Header.Get("x-ratelimit-reset"), 10, 64)
	return RateLimitInfo{
		Remaining: remaining,
		Reset:     time.Unix(resetUnix, 0),
	}
}

// FetchInstruments calls GET /api/v0/equity/metadata/instruments and returns
// metadata for all instruments.
func (c *Client) FetchInstruments(ctx context.Context) ([]InstrumentMeta, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+instrumentsPath, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var instruments []InstrumentMeta
	if err := json.NewDecoder(resp.Body).Decode(&instruments); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return instruments, nil
}

// FetchExchanges calls GET /api/v0/equity/metadata/exchanges and returns
// exchange metadata extracted from the workingSchedules array.
func (c *Client) FetchExchanges(ctx context.Context) ([]ExchangeMeta, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+exchangesPath, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var raw []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		WorkingSchedules []struct {
			ID int `json:"id"`
		} `json:"workingSchedules"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	// Flatten: each workingSchedule ID maps to its parent exchange name.
	var exchanges []ExchangeMeta
	for _, ex := range raw {
		for _, ws := range ex.WorkingSchedules {
			exchanges = append(exchanges, ExchangeMeta{ID: ws.ID, Name: ex.Name})
		}
	}
	return exchanges, nil
}

// LoadMetadata fetches instrument and exchange metadata and caches it on the
// client for use in FetchPositions. Failures are logged but not fatal — the
// client falls back to inferCurrency when metadata is unavailable.
func (c *Client) LoadMetadata(ctx context.Context) error {
	instruments, err := c.FetchInstruments(ctx)
	if err != nil {
		slog.Warn("failed to fetch instruments metadata", "err", err)
		return nil
	}
	c.instruments = make(map[string]InstrumentMeta, len(instruments))
	for _, inst := range instruments {
		c.instruments[inst.Ticker] = inst
	}

	exchanges, err := c.FetchExchanges(ctx)
	if err != nil {
		slog.Warn("failed to fetch exchanges metadata", "err", err)
		return nil
	}
	c.exchanges = make(map[int]string, len(exchanges))
	for _, ex := range exchanges {
		c.exchanges[ex.ID] = shortExchangeName(ex.Name)
	}

	slog.Info("loaded instrument metadata", "instruments", len(c.instruments), "exchanges", len(c.exchanges))
	return nil
}

// LookupInstrument returns the name and exchange for a ticker from cached metadata.
func (c *Client) LookupInstrument(ticker string) (name, exchange string) {
	meta, ok := c.instruments[ticker]
	if !ok {
		return "", ""
	}
	name = meta.Name
	if exName, ok := c.exchanges[meta.WorkingScheduleID]; ok {
		exchange = exName
	}
	return name, exchange
}

// FetchOrderHistory fetches all order fills, paginating automatically.
// Pass "" for ticker to fetch all stocks.
func (c *Client) FetchOrderHistory(ctx context.Context, ticker string) ([]HistoricalOrder, error) {
	path := orderHistoryPath + fmt.Sprintf("?limit=%d", historyPageLimit)
	if ticker != "" {
		path += "&ticker=" + ticker
	}
	var all []HistoricalOrder
	for {
		page, nextPath, rl, err := fetchHistoryPage[HistoricalOrder](c, ctx, path)
		if err != nil {
			return nil, fmt.Errorf("fetch order history: %w", err)
		}
		all = append(all, page...)
		if nextPath == nil {
			break
		}
		path = *nextPath
		if err := rateLimitWait(ctx, rl); err != nil {
			return nil, err
		}
	}
	return all, nil
}

// FetchDividendHistory fetches all dividend payments, paginating automatically.
// Pass "" for ticker to fetch all stocks.
func (c *Client) FetchDividendHistory(ctx context.Context, ticker string) ([]DividendItem, error) {
	path := dividendHistoryPath + fmt.Sprintf("?limit=%d", historyPageLimit)
	if ticker != "" {
		path += "&ticker=" + ticker
	}
	var all []DividendItem
	for {
		page, nextPath, rl, err := fetchHistoryPage[DividendItem](c, ctx, path)
		if err != nil {
			return nil, fmt.Errorf("fetch dividend history: %w", err)
		}
		all = append(all, page...)
		if nextPath == nil {
			break
		}
		path = *nextPath
		if err := rateLimitWait(ctx, rl); err != nil {
			return nil, err
		}
	}
	return all, nil
}

// rateLimitWait sleeps only when the rate limit is nearly exhausted.
// If remaining > 1 or no rate limit headers were present, proceeds immediately.
// Otherwise waits until the reset time (capped at historyRateDelay).
func rateLimitWait(ctx context.Context, rl RateLimitInfo) error {
	if rl.Remaining > 1 {
		return nil
	}
	// No rate limit headers present (zero values) — proceed immediately.
	if rl.Remaining == 0 && rl.Reset.IsZero() {
		return nil
	}
	delay := time.Until(rl.Reset)
	if delay <= 0 {
		return nil
	}
	if delay > historyRateDelay {
		delay = historyRateDelay
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

func fetchHistoryPage[T any](c *Client, ctx context.Context, path string) ([]T, *string, RateLimitInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, nil, RateLimitInfo{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, RateLimitInfo{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, RateLimitInfo{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var page PaginatedResponse[T]
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, nil, RateLimitInfo{}, fmt.Errorf("decode: %w", err)
	}
	return page.Items, page.NextPagePath, parseRateLimit(resp), nil
}
