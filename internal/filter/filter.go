package filter

import "github.com/ko5tas/t212/internal/api"

// Apply returns only the positions where profit-per-share strictly exceeds threshold.
func Apply(positions []api.Position, threshold float64) []api.Position {
	out := make([]api.Position, 0, len(positions))
	for _, p := range positions {
		if p.ProfitPerShare > threshold {
			out = append(out, p)
		}
	}
	return out
}
