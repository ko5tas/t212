package poller

// Notifier is implemented by anything that can send threshold-crossing alerts.
type Notifier interface {
	Notify(ticker string, entered bool, profitPerShare float64)
}
