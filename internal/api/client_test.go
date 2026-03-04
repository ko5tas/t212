package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/ko5tas/t212/internal/api"
)

func TestClient_FetchPositions_Success(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}
		w.Header().Set("x-ratelimit-remaining", "59")
		w.Header().Set("x-ratelimit-reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"instrument":       map[string]any{"ticker": "AAPL_US_EQ", "name": "Apple"},
				"quantity":         3.0,
				"averagePricePaid": 173.20,
				"currentPrice":    182.50,
				"walletImpact":    map[string]any{"currentValue": 412.05},
			},
		})
	}))
	defer srv.Close()

	c := api.NewClient("test-key", "test-secret", srv.URL, srv.Client())
	positions, rl, err := c.FetchPositions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
	if positions[0].Ticker != "AAPL_US_EQ" {
		t.Errorf("ticker: got %q, want AAPL_US_EQ", positions[0].Ticker)
	}
	if positions[0].Name != "Apple" {
		t.Errorf("name: got %q, want Apple", positions[0].Name)
	}
	if positions[0].Currency != "USD" {
		t.Errorf("currency: got %q, want USD", positions[0].Currency)
	}
	if positions[0].CurrentValueGBP != 412.05 {
		t.Errorf("CurrentValueGBP: got %v, want 412.05", positions[0].CurrentValueGBP)
	}
	if rl.Remaining != 59 {
		t.Errorf("ratelimit remaining: got %d, want 59", rl.Remaining)
	}
}

func TestClient_FetchPositions_AuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("x-ratelimit-remaining", "1")
		w.Header().Set("x-ratelimit-reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
		json.NewEncoder(w).Encode([]api.Position{})
	}))
	defer srv.Close()

	c := api.NewClient("my-key-id", "my-secret", srv.URL, srv.Client())
	_, _, err := c.FetchPositions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("my-key-id:my-secret"))
	if gotAuth != want {
		t.Errorf("Authorization header: got %q, want %q", gotAuth, want)
	}
}

func TestClient_FetchPositions_Non200(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := api.NewClient("bad-key", "bad-secret", srv.URL, srv.Client())
	_, _, err := c.FetchPositions(context.Background())
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
}

func TestClient_FetchPositions_GBXConversion(t *testing.T) {
	// UK/LSE tickers (SYMBOL_EQ, no country code) are priced in GBX (pence).
	// Currency is inferred from the ticker suffix; prices divided by 100 → GBP.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-ratelimit-remaining", "59")
		w.Header().Set("x-ratelimit-reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"instrument":       map[string]any{"ticker": "LLOY_EQ", "name": "Lloyds Banking Group"},
				"quantity":         1000.0,
				"averagePricePaid": 5500.0,  // 5500 GBX = £55.00
				"currentPrice":     5612.0,  // 5612 GBX = £56.12
				"walletImpact":     map[string]any{"currentValue": 56120.0},
			},
		})
	}))
	defer srv.Close()

	c := api.NewClient("test-key", "test-secret", srv.URL, srv.Client())
	positions, _, err := c.FetchPositions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
	p := positions[0]
	if p.Ticker != "LLOY_EQ" {
		t.Errorf("ticker: got %q, want LLOY_EQ", p.Ticker)
	}
	if p.Currency != "GBP" {
		t.Errorf("currency: got %q, want GBP (GBX normalised)", p.Currency)
	}
	// Prices must be divided by 100: GBX → GBP
	if p.AveragePrice != 55.00 {
		t.Errorf("AveragePrice: got %v, want 55.00 (5500 GBX ÷ 100)", p.AveragePrice)
	}
	if p.CurrentPrice != 56.12 {
		t.Errorf("CurrentPrice: got %v, want 56.12 (5612 GBX ÷ 100)", p.CurrentPrice)
	}
	wantProfit := 56.12 - 55.00
	if diff := p.ProfitPerShare - wantProfit; diff > 0.001 || diff < -0.001 {
		t.Errorf("ProfitPerShare: got %v, want ~%v", p.ProfitPerShare, wantProfit)
	}
	wantMV := 1000.0 * 56.12
	if diff := p.MarketValue - wantMV; diff > 0.001 || diff < -0.001 {
		t.Errorf("MarketValue: got %v, want ~%v", p.MarketValue, wantMV)
	}
}

func TestClient_FetchInstruments(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/equity/metadata/instruments" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"ticker": "AAPL_US_EQ", "currencyCode": "USD", "workingScheduleId": 1},
			{"ticker": "LLOY_EQ", "currencyCode": "GBX", "workingScheduleId": 2},
			{"ticker": "AMp_EQ", "currencyCode": "EUR", "workingScheduleId": 3},
		})
	}))
	defer srv.Close()

	c := api.NewClient("k", "s", srv.URL, srv.Client())
	instruments, err := c.FetchInstruments(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instruments) != 3 {
		t.Fatalf("expected 3 instruments, got %d", len(instruments))
	}
	if instruments[2].Ticker != "AMp_EQ" {
		t.Errorf("ticker: got %q, want AMp_EQ", instruments[2].Ticker)
	}
	if instruments[2].CurrencyCode != "EUR" {
		t.Errorf("currency: got %q, want EUR", instruments[2].CurrencyCode)
	}
	if instruments[2].WorkingScheduleID != 3 {
		t.Errorf("workingScheduleId: got %d, want 3", instruments[2].WorkingScheduleID)
	}
}

func TestClient_FetchExchanges(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/equity/metadata/exchanges" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		// Real API returns exchanges with nested workingSchedules arrays.
		// Each workingSchedule ID maps to the parent exchange name.
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "name": "NYSE", "workingSchedules": []map[string]any{
				{"id": 10, "timeEvents": []any{}},
				{"id": 11, "timeEvents": []any{}},
			}},
			{"id": 2, "name": "London Stock Exchange", "workingSchedules": []map[string]any{
				{"id": 20, "timeEvents": []any{}},
			}},
			{"id": 3, "name": "Euronext Paris", "workingSchedules": []map[string]any{
				{"id": 30, "timeEvents": []any{}},
			}},
		})
	}))
	defer srv.Close()

	c := api.NewClient("k", "s", srv.URL, srv.Client())
	exchanges, err := c.FetchExchanges(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 4 total: NYSE has 2 working schedules, LSE has 1, Euronext has 1
	if len(exchanges) != 4 {
		t.Fatalf("expected 4 flattened exchanges, got %d", len(exchanges))
	}
	// Verify working schedule IDs map to correct exchange names
	scheduleMap := make(map[int]string)
	for _, ex := range exchanges {
		scheduleMap[ex.ID] = ex.Name
	}
	if scheduleMap[10] != "NYSE" {
		t.Errorf("schedule 10: got %q, want NYSE", scheduleMap[10])
	}
	if scheduleMap[20] != "London Stock Exchange" {
		t.Errorf("schedule 20: got %q, want London Stock Exchange", scheduleMap[20])
	}
	if scheduleMap[30] != "Euronext Paris" {
		t.Errorf("schedule 30: got %q, want Euronext Paris", scheduleMap[30])
	}
}

func TestClient_LoadMetadata_PopulatesCache(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/equity/metadata/instruments":
			json.NewEncoder(w).Encode([]map[string]any{
				{"ticker": "AMp_EQ", "currencyCode": "EUR", "workingScheduleId": 3},
				{"ticker": "LLOY_EQ", "currencyCode": "GBX", "workingScheduleId": 2},
			})
		case "/api/v0/equity/metadata/exchanges":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "name": "London Stock Exchange", "workingSchedules": []map[string]any{
					{"id": 2, "timeEvents": []any{}},
				}},
				{"id": 2, "name": "Euronext Paris", "workingSchedules": []map[string]any{
					{"id": 3, "timeEvents": []any{}},
				}},
			})
		case "/api/v0/equity/positions":
			w.Header().Set("x-ratelimit-remaining", "59")
			w.Header().Set("x-ratelimit-reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
			json.NewEncoder(w).Encode([]map[string]any{
				{
					"instrument":       map[string]any{"ticker": "AMp_EQ", "name": "Dassault Aviation"},
					"quantity":         1.0,
					"averagePricePaid": 200.0,
					"currentPrice":     210.0,
					"walletImpact":     map[string]any{"currentValue": 180.0},
				},
			})
		}
	}))
	defer srv.Close()

	c := api.NewClient("k", "s", srv.URL, srv.Client())
	if err := c.LoadMetadata(context.Background()); err != nil {
		t.Fatalf("LoadMetadata: %v", err)
	}

	positions, _, err := c.FetchPositions(context.Background())
	if err != nil {
		t.Fatalf("FetchPositions: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
	p := positions[0]
	if p.Currency != "EUR" {
		t.Errorf("currency: got %q, want EUR (from metadata, not inferCurrency)", p.Currency)
	}
	if p.Exchange != "Euronext Paris" {
		t.Errorf("exchange: got %q, want Euronext Paris", p.Exchange)
	}
}

func TestClient_FetchPositions_FallbackWithoutMetadata(t *testing.T) {
	// Without LoadMetadata, FetchPositions falls back to inferCurrency.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-ratelimit-remaining", "59")
		w.Header().Set("x-ratelimit-reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"instrument":       map[string]any{"ticker": "AAPL_US_EQ", "name": "Apple"},
				"quantity":         1.0,
				"averagePricePaid": 173.20,
				"currentPrice":     182.50,
				"walletImpact":     map[string]any{"currentValue": 150.0},
			},
		})
	}))
	defer srv.Close()

	c := api.NewClient("k", "s", srv.URL, srv.Client())
	positions, _, err := c.FetchPositions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if positions[0].Currency != "USD" {
		t.Errorf("currency: got %q, want USD (inferCurrency fallback)", positions[0].Currency)
	}
	if positions[0].Exchange != "" {
		t.Errorf("exchange: got %q, want empty (no metadata)", positions[0].Exchange)
	}
}

func TestClient_LookupInstrument(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/equity/metadata/instruments":
			json.NewEncoder(w).Encode([]map[string]any{
				{"ticker": "AAPL_US_EQ", "name": "Apple Inc", "currencyCode": "USD", "workingScheduleId": 10},
				{"ticker": "LLOY_EQ", "name": "Lloyds Banking Group", "currencyCode": "GBX", "workingScheduleId": 20},
			})
		case "/api/v0/equity/metadata/exchanges":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "name": "NYSE", "workingSchedules": []map[string]any{{"id": 10}}},
				{"id": 2, "name": "London Stock Exchange", "workingSchedules": []map[string]any{{"id": 20}}},
			})
		}
	}))
	defer srv.Close()

	c := api.NewClient("k", "s", srv.URL, srv.Client())
	if err := c.LoadMetadata(context.Background()); err != nil {
		t.Fatalf("LoadMetadata: %v", err)
	}

	name, exchange := c.LookupInstrument("AAPL_US_EQ")
	if name != "Apple Inc" {
		t.Errorf("name: got %q, want Apple Inc", name)
	}
	if exchange != "NYSE" {
		t.Errorf("exchange: got %q, want NYSE", exchange)
	}

	name, exchange = c.LookupInstrument("LLOY_EQ")
	if name != "Lloyds Banking Group" {
		t.Errorf("name: got %q, want Lloyds Banking Group", name)
	}
	if exchange != "LSE" {
		t.Errorf("exchange: got %q, want LSE (abbreviated)", exchange)
	}

	// Unknown ticker
	name, exchange = c.LookupInstrument("UNKNOWN_EQ")
	if name != "" || exchange != "" {
		t.Errorf("unknown ticker: got name=%q exchange=%q, want empty", name, exchange)
	}
}

func TestClient_FetchOrderHistory(t *testing.T) {
	page := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/equity/history/orders" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("x-ratelimit-remaining", "5")
		w.Header().Set("x-ratelimit-reset", strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10))
		page++
		if page == 1 {
			next := "/api/v0/equity/history/orders?cursor=123&limit=50"
			json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{
						"fill": map[string]any{
							"price": 150.0, "quantity": 0.5, "filledAt": "2025-11-17T10:00:00Z",
							"walletImpact": map[string]any{"netValue": 75.0, "currency": "GBP"},
						},
						"order": map[string]any{"ticker": "GOOGL_US_EQ", "side": "BUY", "status": "FILLED"},
					},
				},
				"nextPagePath": next,
			})
		} else {
			json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{
						"fill": map[string]any{
							"price": 155.0, "quantity": 0.2, "filledAt": "2025-12-01T10:00:00Z",
							"walletImpact": map[string]any{"netValue": 31.0, "currency": "GBP"},
						},
						"order": map[string]any{"ticker": "GOOGL_US_EQ", "side": "SELL", "status": "FILLED"},
					},
				},
				"nextPagePath": nil,
			})
		}
	}))
	defer srv.Close()

	c := api.NewClient("k", "s", srv.URL, srv.Client())
	orders, err := c.FetchOrderHistory(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("expected 2 orders, got %d", len(orders))
	}
	if orders[0].Order.Side != "BUY" {
		t.Errorf("order[0] side: got %q, want BUY", orders[0].Order.Side)
	}
	if orders[1].Fill.Impact.NetValue != 31.0 {
		t.Errorf("order[1] netValue: got %v, want 31.0", orders[1].Fill.Impact.NetValue)
	}
}

func TestClient_FetchOrderHistory_ByTicker(t *testing.T) {
	var gotTicker string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTicker = r.URL.Query().Get("ticker")
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "nextPagePath": nil})
	}))
	defer srv.Close()

	c := api.NewClient("k", "s", srv.URL, srv.Client())
	c.FetchOrderHistory(context.Background(), "AAPL_US_EQ")
	if gotTicker != "AAPL_US_EQ" {
		t.Errorf("ticker param: got %q, want AAPL_US_EQ", gotTicker)
	}
}

func TestClient_FetchDividendHistory(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/equity/history/dividends" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"amount": 3.50, "ticker": "GOOGL_US_EQ", "paidOn": "2025-12-15T00:00:00Z"},
			},
			"nextPagePath": nil,
		})
	}))
	defer srv.Close()

	c := api.NewClient("k", "s", srv.URL, srv.Client())
	divs, err := c.FetchDividendHistory(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(divs) != 1 {
		t.Fatalf("expected 1 dividend, got %d", len(divs))
	}
	if divs[0].Amount != 3.50 {
		t.Errorf("amount: got %v, want 3.50", divs[0].Amount)
	}
	if divs[0].Ticker != "GOOGL_US_EQ" {
		t.Errorf("ticker: got %q, want GOOGL_US_EQ", divs[0].Ticker)
	}
}
