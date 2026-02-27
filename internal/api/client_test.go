package api_test

import (
	"context"
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
			{"ticker": "AAPL_US_EQ", "quantity": 3.0, "averagePrice": 173.20, "currentPrice": 182.50},
		})
	}))
	defer srv.Close()

	c := api.NewClient("test-key", srv.URL, srv.Client())
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

	c := api.NewClient("my-secret-key", srv.URL, srv.Client())
	_, _, err := c.FetchPositions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "my-secret-key" {
		t.Errorf("Authorization header: got %q, want %q", gotAuth, "my-secret-key")
	}
}

func TestClient_FetchPositions_Non200(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := api.NewClient("bad-key", srv.URL, srv.Client())
	_, _, err := c.FetchPositions(context.Background())
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
}
