package api

// Position is a single open position returned by the T212 /equity/positions endpoint.
// ProfitPerShare and MarketValue are computed fields — call Compute() after unmarshalling.
type Position struct {
	Ticker          string  `json:"ticker"`
	Name            string  `json:"name"`
	Currency        string  `json:"currency"`
	Quantity        float64 `json:"quantity"`
	AveragePrice    float64 `json:"averagePrice"`
	CurrentPrice    float64 `json:"currentPrice"`
	CurrentValueGBP float64 `json:"currentValueGBP"`
	Exchange        string  `json:"exchange"`
	ProfitPerShare  float64 `json:"profitPerShare"`
	MarketValue     float64     `json:"marketValue"`
	Returns         *ReturnInfo `json:"returns,omitempty"`
}

// ClosedPosition represents a previously held instrument with realised returns.
type ClosedPosition struct {
	Ticker   string      `json:"ticker"`
	Name     string      `json:"name"`
	Exchange string      `json:"exchange"`
	Returns  *ReturnInfo `json:"returns,omitempty"`
}

// CurrencySymbol returns the display symbol for the position's currency.
func (p Position) CurrencySymbol() string {
	switch p.Currency {
	case "GBP":
		return "£"
	case "USD":
		return "$"
	case "EUR":
		return "€"
	default:
		return p.Currency + " "
	}
}

// Compute populates the derived fields ProfitPerShare and MarketValue.
func (p *Position) Compute() {
	p.ProfitPerShare = p.CurrentPrice - p.AveragePrice
	p.MarketValue = p.Quantity * p.CurrentPrice
}
