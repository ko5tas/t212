package api

// InstrumentMeta holds metadata for a single instrument from the T212
// /api/v0/equity/metadata/instruments endpoint.
type InstrumentMeta struct {
	Ticker            string `json:"ticker"`
	Name              string `json:"name"`
	CurrencyCode      string `json:"currencyCode"`
	WorkingScheduleID int    `json:"workingScheduleId"`
}

// ExchangeMeta holds metadata for a single exchange (working schedule) from the
// T212 /api/v0/equity/metadata/exchanges endpoint.
type ExchangeMeta struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
