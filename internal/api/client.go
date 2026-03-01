package api

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	positionsPath       = "/api/v0/equity/positions"
	orderHistoryPath    = "/api/v0/equity/history/orders"
	dividendHistoryPath = "/api/v0/equity/history/dividends"
	historyPageLimit    = 50
	historyRateDelay    = 11 * time.Second // stay under 6 req/min
)

// Client is a Trading 212 API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
	authHeader string
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
		} `json:"instrument"`
		Quantity     float64 `json:"quantity"`
		AveragePrice float64 `json:"averagePricePaid"`
		CurrentPrice float64 `json:"currentPrice"`
	}
	var raw []wirePosition
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, RateLimitInfo{}, fmt.Errorf("decode response: %w", err)
	}

	positions := make([]Position, len(raw))
	for i, r := range raw {
		avg := r.AveragePrice
		curr := r.CurrentPrice
		currency := inferCurrency(r.Instrument.Ticker)
		if currency == "GBX" {
			avg /= 100
			curr /= 100
			currency = "GBP"
		}
		positions[i] = Position{
			Ticker:       r.Instrument.Ticker,
			Currency:     currency,
			Quantity:     r.Quantity,
			AveragePrice: avg,
			CurrentPrice: curr,
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

func parseRateLimit(resp *http.Response) RateLimitInfo {
	remaining, _ := strconv.Atoi(resp.Header.Get("x-ratelimit-remaining"))
	resetUnix, _ := strconv.ParseInt(resp.Header.Get("x-ratelimit-reset"), 10, 64)
	return RateLimitInfo{
		Remaining: remaining,
		Reset:     time.Unix(resetUnix, 0),
	}
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
		page, nextPath, err := fetchHistoryPage[HistoricalOrder](c, ctx, path)
		if err != nil {
			return nil, fmt.Errorf("fetch order history: %w", err)
		}
		all = append(all, page...)
		if nextPath == nil {
			break
		}
		path = *nextPath
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(historyRateDelay):
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
		page, nextPath, err := fetchHistoryPage[DividendItem](c, ctx, path)
		if err != nil {
			return nil, fmt.Errorf("fetch dividend history: %w", err)
		}
		all = append(all, page...)
		if nextPath == nil {
			break
		}
		path = *nextPath
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(historyRateDelay):
		}
	}
	return all, nil
}

func fetchHistoryPage[T any](c *Client, ctx context.Context, path string) ([]T, *string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var page PaginatedResponse[T]
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, nil, fmt.Errorf("decode: %w", err)
	}
	return page.Items, page.NextPagePath, nil
}
