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
			{"instrument": map[string]any{"ticker": "AAPL_US_EQ"}, "quantity": 3.0, "averagePricePaid": 173.20, "currentPrice": 182.50}, // T212 wire format
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
	if positions[0].Currency != "USD" {
		t.Errorf("currency: got %q, want USD", positions[0].Currency)
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
				"instrument":       map[string]any{"ticker": "LLOY_EQ"},
				"quantity":         1000.0,
				"averagePricePaid": 5500.0, // 5500 GBX = £55.00
				"currentPrice":     5612.0, // 5612 GBX = £56.12
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
