package api

import "time"

// ReturnInfo holds realised return data for a single position.
type ReturnInfo struct {
	TotalBought    float64 `json:"totalBought"`
	TotalSold      float64 `json:"totalSold"`
	TotalDividends float64 `json:"totalDividends"`
	Return         float64 `json:"return"`
	ReturnPct      float64 `json:"returnPct"`
}

// HistoricalOrder is one item from GET /api/v0/equity/history/orders.
type HistoricalOrder struct {
	Fill struct {
		Price    float64   `json:"price"`
		Quantity float64   `json:"quantity"`
		FilledAt time.Time `json:"filledAt"`
		Impact   struct {
			NetValue float64 `json:"netValue"`
			Currency string  `json:"currency"`
		} `json:"walletImpact"`
	} `json:"fill"`
	Order struct {
		Ticker string `json:"ticker"`
		Side   string `json:"side"`
		Status string `json:"status"`
	} `json:"order"`
}

// DividendItem is one item from GET /api/v0/equity/history/dividends.
type DividendItem struct {
	Amount float64   `json:"amount"`
	Ticker string    `json:"ticker"`
	PaidOn time.Time `json:"paidOn"`
}

// PaginatedResponse wraps the T212 paginated list format.
type PaginatedResponse[T any] struct {
	Items        []T     `json:"items"`
	NextPagePath *string `json:"nextPagePath"`
}
