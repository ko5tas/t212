package api_test

import (
	"encoding/json"
	"testing"

	"github.com/ko5tas/t212/internal/api"
)

func TestPosition_UnmarshalJSON(t *testing.T) {
	// Position uses standard JSON tags for the internal/broadcast format.
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
		Ticker:       "AAPL_US_EQ", // struct field name unchanged; only JSON tag changed
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

func TestPosition_ReturnsNilByDefault(t *testing.T) {
	p := api.Position{Ticker: "AAPL_US_EQ"}
	if p.Returns != nil {
		t.Error("Returns should be nil by default")
	}
}

func TestPosition_ReturnsInJSON(t *testing.T) {
	ri := &api.ReturnInfo{Return: 42.30, ReturnPct: 42.30, NetROIPct: 65.08}
	p := api.Position{Ticker: "AAPL_US_EQ", Returns: ri}
	b, _ := json.Marshal(p)
	var got api.Position
	json.Unmarshal(b, &got)
	if got.Returns == nil {
		t.Fatal("Returns should not be nil after unmarshal")
	}
	if got.Returns.Return != 42.30 {
		t.Errorf("Return: got %v, want 42.30", got.Returns.Return)
	}
}
