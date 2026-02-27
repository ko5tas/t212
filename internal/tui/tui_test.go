package tui_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ko5tas/t212/internal/api"
	"github.com/ko5tas/t212/internal/tui"
)

func TestModel_UpdateFromMessage(t *testing.T) {
	m := tui.NewModel()

	positions := []api.Position{
		{
			Ticker:         "AAPL_US_EQ",
			Quantity:       3,
			AveragePrice:   173.20,
			CurrentPrice:   182.50,
			ProfitPerShare: 9.30,
			MarketValue:    547.50,
		},
	}
	payload := tui.WSMessage{
		Timestamp: time.Now(),
		Positions: positions,
	}
	b, _ := json.Marshal(payload)

	updated := m.ApplyMessage(b)
	if len(updated.Positions()) != 1 {
		t.Fatalf("expected 1 position, got %d", len(updated.Positions()))
	}
	if updated.Positions()[0].Ticker != "AAPL_US_EQ" {
		t.Errorf("ticker: got %q", updated.Positions()[0].Ticker)
	}
}

func TestModel_InvalidMessageIgnored(t *testing.T) {
	m := tui.NewModel()
	updated := m.ApplyMessage([]byte("not json"))
	if len(updated.Positions()) != 0 {
		t.Error("invalid message should leave positions empty")
	}
}

func TestModel_EmptyPositions(t *testing.T) {
	m := tui.NewModel()

	payload := tui.WSMessage{
		Timestamp: time.Now(),
		Positions: []api.Position{},
	}
	b, _ := json.Marshal(payload)

	updated := m.ApplyMessage(b)
	if updated.Positions() == nil {
		t.Error("positions should be empty slice, not nil")
	}
	if len(updated.Positions()) != 0 {
		t.Errorf("expected 0 positions, got %d", len(updated.Positions()))
	}
}

func TestModel_TimestampUpdated(t *testing.T) {
	m := tui.NewModel()
	ts := time.Date(2026, 2, 27, 12, 34, 56, 0, time.UTC)

	payload := tui.WSMessage{
		Timestamp: ts,
		Positions: []api.Position{},
	}
	b, _ := json.Marshal(payload)

	updated := m.ApplyMessage(b)
	if !updated.LastUpdated().Equal(ts) {
		t.Errorf("timestamp: got %v, want %v", updated.LastUpdated(), ts)
	}
}
