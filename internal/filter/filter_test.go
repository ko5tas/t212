package filter_test

import (
	"testing"

	"github.com/ko5tas/t212/internal/api"
	"github.com/ko5tas/t212/internal/filter"
)

func pos(ticker string, avg, current, qty float64) api.Position {
	p := api.Position{
		Ticker:       ticker,
		Quantity:     qty,
		AveragePrice: avg,
		CurrentPrice: current,
	}
	p.Compute()
	return p
}

func TestApply(t *testing.T) {
	threshold := 1.00

	tests := []struct {
		name      string
		positions []api.Position
		want      []string // expected tickers in result
	}{
		{
			name:      "well above threshold",
			positions: []api.Position{pos("AAPL", 173.20, 182.50, 3)},
			want:      []string{"AAPL"},
		},
		{
			name:      "just above threshold (1.01)",
			positions: []api.Position{pos("MSFT", 100.00, 101.01, 1)},
			want:      []string{"MSFT"},
		},
		{
			name:      "exactly at threshold (1.00) — excluded",
			positions: []api.Position{pos("GOOG", 100.00, 101.00, 1)},
			want:      []string{},
		},
		{
			name:      "just below threshold (0.99)",
			positions: []api.Position{pos("AMZN", 100.00, 100.99, 1)},
			want:      []string{},
		},
		{
			name:      "zero profit",
			positions: []api.Position{pos("META", 100.00, 100.00, 1)},
			want:      []string{},
		},
		{
			name:      "negative profit (loss)",
			positions: []api.Position{pos("TSLA", 200.00, 190.00, 5)},
			want:      []string{},
		},
		{
			name: "mixed — only profitable ones returned",
			positions: []api.Position{
				pos("AAPL", 173.20, 182.50, 3),
				pos("TSLA", 200.00, 190.00, 5),
				pos("MSFT", 100.00, 105.00, 2),
			},
			want: []string{"AAPL", "MSFT"},
		},
		{
			name:      "empty input",
			positions: []api.Position{},
			want:      []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filter.Apply(tt.positions, threshold)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d positions, want %d: %+v", len(got), len(tt.want), got)
			}
			for i, p := range got {
				if p.Ticker != tt.want[i] {
					t.Errorf("position[%d]: got ticker %q, want %q", i, p.Ticker, tt.want[i])
				}
			}
		})
	}
}
