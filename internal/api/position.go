package api

// Position is a single open position returned by the T212 /equity/positions endpoint.
// ProfitPerShare and MarketValue are computed fields — call Compute() after unmarshalling.
type Position struct {
	Ticker         string  `json:"instrument"`
	Quantity       float64 `json:"quantity"`
	AveragePrice   float64 `json:"averagePricePaid"`
	CurrentPrice   float64 `json:"currentPrice"`
	ProfitPerShare float64 `json:"profitPerShare"`
	MarketValue    float64 `json:"marketValue"`
}

// Compute populates the derived fields ProfitPerShare and MarketValue.
func (p *Position) Compute() {
	p.ProfitPerShare = p.CurrentPrice - p.AveragePrice
	p.MarketValue = p.Quantity * p.CurrentPrice
}
