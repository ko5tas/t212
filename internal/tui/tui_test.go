package tui_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

func TestModel_NullPositionsInJSON(t *testing.T) {
	m := tui.NewModel()
	updated := m.ApplyMessage([]byte(`{"timestamp":"2026-02-27T00:00:00Z","positions":null}`))
	if updated.Positions() == nil {
		t.Error("positions should be empty slice, not nil when JSON has null")
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

func TestModel_ViewShowsReturnColumns(t *testing.T) {
	m := tui.NewModel()
	ri := &api.ReturnInfo{Return: 42.30, ReturnPct: 42.30, NetROIPct: 65.08}
	payload := tui.WSMessage{
		Timestamp: time.Now(),
		Positions: []api.Position{
			{
				Ticker: "AAPL_US_EQ", Currency: "USD", Quantity: 3,
				AveragePrice: 173.20, CurrentPrice: 182.50,
				ProfitPerShare: 9.30, MarketValue: 547.50,
				Returns: ri,
			},
		},
	}
	b, _ := json.Marshal(payload)
	updated := m.ApplyMessage(b)
	view := updated.View()

	if !strings.Contains(view, "RETURN") {
		t.Error("view should contain RETURN header")
	}
	if !strings.Contains(view, "42.30") {
		t.Error("view should contain return value 42.30")
	}
	if !strings.Contains(view, "65.08") {
		t.Error("view should contain NetROI value 65.08")
	}
}

func TestModel_ViewShowsPlaceholderWhenNoReturns(t *testing.T) {
	m := tui.NewModel()
	payload := tui.WSMessage{
		Timestamp: time.Now(),
		Positions: []api.Position{
			{
				Ticker: "AAPL_US_EQ", Currency: "USD", Quantity: 3,
				AveragePrice: 173.20, CurrentPrice: 182.50,
				ProfitPerShare: 9.30, MarketValue: 547.50,
			},
		},
	}
	b, _ := json.Marshal(payload)
	updated := m.ApplyMessage(b)
	view := updated.View()

	if !strings.Contains(view, "--") {
		t.Error("view should contain -- placeholder when Returns is nil")
	}
}

func TestModel_ViewEmptyMessage(t *testing.T) {
	m := tui.NewModel()
	payload := tui.WSMessage{
		Timestamp: time.Now(),
		Positions: []api.Position{},
	}
	b, _ := json.Marshal(payload)
	updated := m.ApplyMessage(b)
	view := updated.View()

	if !strings.Contains(view, "No positions") {
		t.Error("view should show 'No positions' when empty")
	}
}

func TestModel_ViewColumnOrder(t *testing.T) {
	m := tui.NewModel()
	payload := tui.WSMessage{
		Timestamp: time.Now(),
		Positions: []api.Position{
			{
				Ticker: "AAPL_US_EQ", Currency: "USD", Quantity: 3,
				AveragePrice: 173.20, CurrentPrice: 182.50,
				ProfitPerShare: 9.30, MarketValue: 547.50,
			},
		},
	}
	b, _ := json.Marshal(payload)
	updated := m.ApplyMessage(b)
	view := updated.View()

	// Current Price should appear before Avg Price in the header
	currIdx := strings.Index(view, "CURR PRICE")
	avgIdx := strings.Index(view, "AVG PRICE")
	if currIdx < 0 || avgIdx < 0 {
		t.Fatal("view should contain both CURR PRICE and AVG PRICE headers")
	}
	if currIdx > avgIdx {
		t.Error("CURR PRICE should appear before AVG PRICE")
	}
}

func TestModel_CursorMovement(t *testing.T) {
	m := tui.NewModel()
	payload := tui.WSMessage{
		Timestamp: time.Now(),
		Positions: []api.Position{
			{Ticker: "AAPL_US_EQ", ProfitPerShare: 5},
			{Ticker: "MSFT_US_EQ", ProfitPerShare: 3},
		},
	}
	b, _ := json.Marshal(payload)
	m = m.ApplyMessage(b)

	// Move down
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model := m2.(tui.Model)
	if model.Cursor() != 1 {
		t.Errorf("cursor should be 1 after j, got %d", model.Cursor())
	}

	// Move up
	m3, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	model2 := m3.(tui.Model)
	if model2.Cursor() != 0 {
		t.Errorf("cursor should be 0 after k, got %d", model2.Cursor())
	}
}

func TestModel_SortCycleColumn(t *testing.T) {
	m := tui.NewModel()
	payload := tui.WSMessage{
		Timestamp: time.Now(),
		Positions: []api.Position{
			{Ticker: "AAPL_US_EQ", ProfitPerShare: 5, Quantity: 10},
			{Ticker: "MSFT_US_EQ", ProfitPerShare: 3, Quantity: 20},
		},
	}
	b, _ := json.Marshal(payload)
	m = m.ApplyMessage(b)

	// Default sort is ProfitPerShare desc
	if m.SortCol() != tui.SortProfitPerShare {
		t.Errorf("default sort column should be ProfitPerShare, got %v", m.SortCol())
	}

	// Press 's' to cycle to next column
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	model := m2.(tui.Model)
	if model.SortCol() != tui.SortMarketValue {
		t.Errorf("sort column after s should be MarketValue, got %v", model.SortCol())
	}
}

func TestModel_SortToggleDirection(t *testing.T) {
	m := tui.NewModel()
	payload := tui.WSMessage{
		Timestamp: time.Now(),
		Positions: []api.Position{
			{Ticker: "AAPL_US_EQ", ProfitPerShare: 5},
			{Ticker: "MSFT_US_EQ", ProfitPerShare: 3},
		},
	}
	b, _ := json.Marshal(payload)
	m = m.ApplyMessage(b)

	// Default: desc
	if m.SortAsc() {
		t.Error("default sort should be descending")
	}

	// Press 'S' to toggle to ascending
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	model := m2.(tui.Model)
	if !model.SortAsc() {
		t.Error("sort should be ascending after S")
	}

	// Positions should now be in ascending order (3 before 5)
	if model.Positions()[0].ProfitPerShare != 3 {
		t.Errorf("ascending sort: first position should have ProfitPerShare=3, got %v",
			model.Positions()[0].ProfitPerShare)
	}
}

func TestModel_SortIndicatorInView(t *testing.T) {
	m := tui.NewModel()
	payload := tui.WSMessage{
		Timestamp: time.Now(),
		Positions: []api.Position{
			{Ticker: "AAPL_US_EQ", ProfitPerShare: 5},
		},
	}
	b, _ := json.Marshal(payload)
	m = m.ApplyMessage(b)
	view := m.View()

	// Default sort column (ProfitPerShare) should have a sort indicator
	if !strings.Contains(view, "▼") {
		t.Error("view should contain ▼ indicator for default desc sort")
	}
}
