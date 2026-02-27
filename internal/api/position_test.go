package api_test

import (
	"encoding/json"
	"testing"

	"github.com/ko5tas/t212/internal/api"
)

func TestPosition_UnmarshalJSON(t *testing.T) {
	raw := `{
		"ticker": "AAPL_US_EQ",
		"quantity": 3.0,
		"averagePrice": 173.20,
		"currentPrice": 182.50
	}`

	var p api.Position
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Ticker != "AAPL_US_EQ" {
		t.Errorf("ticker: got %q, want %q", p.Ticker, "AAPL_US_EQ")
	}
	if p.Quantity != 3.0 {
		t.Errorf("quantity: got %v, want 3.0", p.Quantity)
	}
	if p.AveragePrice != 173.20 {
		t.Errorf("averagePrice: got %v, want 173.20", p.AveragePrice)
	}
	if p.CurrentPrice != 182.50 {
		t.Errorf("currentPrice: got %v, want 182.50", p.CurrentPrice)
	}
}

func TestPosition_Computed(t *testing.T) {
	p := api.Position{
		Ticker:       "AAPL_US_EQ",
		Quantity:     3.0,
		AveragePrice: 173.20,
		CurrentPrice: 182.50,
	}
	p.Compute()

	want := 182.50 - 173.20 // 9.30
	if diff := p.ProfitPerShare - want; diff > 0.001 || diff < -0.001 {
		t.Errorf("ProfitPerShare: got %v, want ~%v", p.ProfitPerShare, want)
	}

	wantMV := 3.0 * 182.50 // 547.50
	if diff := p.MarketValue - wantMV; diff > 0.001 || diff < -0.001 {
		t.Errorf("MarketValue: got %v, want ~%v", p.MarketValue, wantMV)
	}
}
