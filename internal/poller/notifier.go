package poller

// Notifier is implemented by anything that can send threshold-crossing alerts.
type Notifier interface {
	Notify(ticker, name string, entered bool, amount, returnPct float64)
}
