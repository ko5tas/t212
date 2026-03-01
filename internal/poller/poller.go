package poller

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/ko5tas/t212/internal/api"
	"github.com/ko5tas/t212/internal/filter"
	"github.com/ko5tas/t212/internal/history"
	"github.com/ko5tas/t212/internal/hub"
	"github.com/ko5tas/t212/internal/store"
)

const pollInterval = time.Minute

// BroadcastMessage is the JSON payload sent to WebSocket subscribers on each poll.
type BroadcastMessage struct {
	Timestamp time.Time      `json:"timestamp"`
	Positions []api.Position `json:"positions"`
}

// Poller polls the T212 API, updates the store, and broadcasts filtered positions.
type Poller struct {
	client       *api.Client
	store        *store.Store
	hub          *hub.Hub
	threshold    float64
	notifier     Notifier
	prevAbove    map[string]bool
	interval     time.Duration
	historyStore *history.Store
	refreshCh    chan string // ticker or "" for all
}

// Option configures a Poller.
type Option func(*Poller)

// WithHistoryStore attaches a history store for return data.
func WithHistoryStore(hs *history.Store) Option {
	return func(p *Poller) { p.historyStore = hs }
}

// WithRefreshChan attaches a channel for receiving refresh requests.
func WithRefreshChan(ch chan string) Option {
	return func(p *Poller) { p.refreshCh = ch }
}

// New creates a Poller. notifier may be nil (no alerts sent).
func New(client *api.Client, s *store.Store, h *hub.Hub, threshold float64, n Notifier, opts ...Option) *Poller {
	p := &Poller{
		client:    client,
		store:     s,
		hub:       h,
		threshold: threshold,
		notifier:  n,
		prevAbove: make(map[string]bool),
		interval:  pollInterval,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// NewForTesting creates a Poller with a custom poll interval. Intended for tests only.
func NewForTesting(client *api.Client, s *store.Store, h *hub.Hub, threshold float64, n Notifier, interval time.Duration, opts ...Option) *Poller {
	p := New(client, s, h, threshold, n, opts...)
	p.interval = interval
	return p
}

// Run starts the polling loop. Blocks until ctx is cancelled.
// An immediate poll is performed before waiting for the first tick.
func (p *Poller) Run(ctx context.Context) {
	if p.historyStore != nil {
		p.refreshHistory(ctx, "")
	}
	p.poll(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	var historyTicker *time.Ticker
	if p.historyStore != nil {
		historyTicker = time.NewTicker(time.Hour)
		defer historyTicker.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		case t := <-p.refreshChOrNil():
			p.refreshHistory(ctx, t)
			p.poll(ctx)
		case <-p.historyTickerOrNil(historyTicker):
			p.refreshHistory(ctx, "")
			p.poll(ctx)
		}
	}
}

func (p *Poller) poll(ctx context.Context) {
	positions, rl, err := p.client.FetchPositions(ctx)
	if err != nil {
		slog.Error("fetch positions failed", "err", err)
		return
	}

	slog.Debug("fetched positions", "count", len(positions), "ratelimit_remaining", rl.Remaining)

	p.store.Set(positions)

	filtered := filter.Apply(positions, p.threshold)
	p.sendNotifications(filtered)
	p.attachReturns(filtered)
	p.broadcast(filtered)
}

func (p *Poller) broadcast(filtered []api.Position) {
	msg := BroadcastMessage{
		Timestamp: time.Now().UTC(),
		Positions: filtered,
	}
	b, err := json.Marshal(msg)
	if err != nil {
		slog.Error("marshal broadcast", "err", err)
		return
	}
	p.hub.Broadcast(b)
}

func (p *Poller) sendNotifications(filtered []api.Position) {
	if p.notifier == nil {
		return
	}

	nowAbove := make(map[string]api.Position, len(filtered))
	for _, pos := range filtered {
		nowAbove[pos.Ticker] = pos
	}

	// Detect edge: entered threshold.
	for ticker, pos := range nowAbove {
		if !p.prevAbove[ticker] {
			p.notifier.Notify(ticker, true, pos.ProfitPerShare, pos.CurrencySymbol())
		}
	}

	// Detect edge: exited threshold.
	for ticker := range p.prevAbove {
		if _, ok := nowAbove[ticker]; !ok {
			p.notifier.Notify(ticker, false, 0, "")
		}
	}

	// Update previous state.
	newPrev := make(map[string]bool, len(nowAbove))
	for ticker := range nowAbove {
		newPrev[ticker] = true
	}
	p.prevAbove = newPrev
}

func (p *Poller) attachReturns(positions []api.Position) {
	if p.historyStore == nil {
		return
	}
	for i := range positions {
		positions[i].Returns = p.historyStore.Get(positions[i].Ticker)
	}
}

func (p *Poller) refreshChOrNil() <-chan string {
	if p.refreshCh == nil {
		return nil
	}
	return p.refreshCh
}

func (p *Poller) historyTickerOrNil(t *time.Ticker) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

func (p *Poller) refreshHistory(ctx context.Context, ticker string) {
	if p.historyStore == nil {
		return
	}
	slog.Info("refreshing history", "ticker", ticker)

	orders, err := p.client.FetchOrderHistory(ctx, ticker)
	if err != nil {
		slog.Error("fetch order history", "err", err)
		return
	}
	divs, err := p.client.FetchDividendHistory(ctx, ticker)
	if err != nil {
		slog.Error("fetch dividend history", "err", err)
		return
	}

	positions := p.store.Get()

	if ticker == "" {
		ordersByTicker := make(map[string][]api.HistoricalOrder)
		for _, o := range orders {
			ordersByTicker[o.Order.Ticker] = append(ordersByTicker[o.Order.Ticker], o)
		}
		divsByTicker := make(map[string][]api.DividendItem)
		for _, d := range divs {
			divsByTicker[d.Ticker] = append(divsByTicker[d.Ticker], d)
		}
		all := make(map[string]api.ReturnInfo)
		tickers := make(map[string]bool)
		for t := range ordersByTicker {
			tickers[t] = true
		}
		for t := range divsByTicker {
			tickers[t] = true
		}
		for t := range tickers {
			pps, qty := findPosition(positions, t)
			all[t] = history.ComputeReturns(ordersByTicker[t], divsByTicker[t], pps, qty)
		}
		p.historyStore.SetAll(all)
	} else {
		pps, qty := findPosition(positions, ticker)
		ri := history.ComputeReturns(orders, divs, pps, qty)
		p.historyStore.Set(ticker, ri)
	}
}

func findPosition(positions []api.Position, ticker string) (profitPerShare, quantity float64) {
	for _, p := range positions {
		if p.Ticker == ticker {
			return p.ProfitPerShare, p.Quantity
		}
	}
	return 0, 0
}
