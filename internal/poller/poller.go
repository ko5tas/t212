package poller

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/ko5tas/t212/internal/api"
	"github.com/ko5tas/t212/internal/history"
	"github.com/ko5tas/t212/internal/hub"
	"github.com/ko5tas/t212/internal/store"
)

const pollInterval = time.Minute

// BroadcastMessage is the JSON payload sent to WebSocket subscribers on each poll.
type BroadcastMessage struct {
	Timestamp       time.Time            `json:"timestamp"`
	Positions       []api.Position       `json:"positions"`
	ClosedPositions []api.ClosedPosition `json:"closedPositions"`
}

// Poller polls the T212 API, updates the store, and broadcasts filtered positions.
type Poller struct {
	client       *api.Client
	store        *store.Store
	hub          *hub.Hub
	threshold    float64
	notifier     Notifier
	mu           sync.Mutex // guards prevAbove, prevBelow
	prevAbove    map[string]bool
	prevBelow    map[string]bool
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
		prevBelow: make(map[string]bool),
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
// History is fetched in the background so positions appear immediately.
func (p *Poller) Run(ctx context.Context) {
	p.poll(ctx)

	if p.historyStore != nil {
		go func() {
			p.refreshHistory(ctx, "")
			p.poll(ctx) // re-broadcast with returns attached
		}()
	}

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

	p.attachReturns(positions)
	p.sendNotifications(positions)
	closed := p.buildClosedPositions(positions)
	p.broadcast(positions, closed)
}

func (p *Poller) buildClosedPositions(openPositions []api.Position) []api.ClosedPosition {
	if p.historyStore == nil {
		return nil
	}
	open := make(map[string]bool, len(openPositions))
	for _, pos := range openPositions {
		open[pos.Ticker] = true
	}
	var closed []api.ClosedPosition
	for _, ticker := range p.historyStore.Tickers() {
		if open[ticker] {
			continue
		}
		ri := p.historyStore.Get(ticker)
		name, exchange := p.client.LookupInstrument(ticker)
		closed = append(closed, api.ClosedPosition{
			Ticker:   ticker,
			Name:     name,
			Exchange: exchange,
			Returns:  ri,
		})
	}
	return closed
}

func (p *Poller) broadcast(filtered []api.Position, closed []api.ClosedPosition) {
	msg := BroadcastMessage{
		Timestamp:       time.Now().UTC(),
		Positions:       filtered,
		ClosedPositions: closed,
	}
	b, err := json.Marshal(msg)
	if err != nil {
		slog.Error("marshal broadcast", "err", err)
		return
	}
	p.hub.Broadcast(b)
}

func (p *Poller) sendNotifications(positions []api.Position) {
	if p.notifier == nil {
		return
	}

	nowAbove := make(map[string]api.Position)
	nowBelow := make(map[string]api.Position)
	for _, pos := range positions {
		if pos.Returns == nil {
			continue
		}
		if pos.CurrentValueGBP > pos.Returns.TotalBought+p.threshold {
			nowAbove[pos.Ticker] = pos
		}
		if pos.Returns.TotalBought > 0 && pos.CurrentValueGBP < pos.Returns.TotalBought {
			nowBelow[pos.Ticker] = pos
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Detect edge: crossed above profit threshold.
	for ticker, pos := range nowAbove {
		if !p.prevAbove[ticker] {
			profit := pos.CurrentValueGBP - pos.Returns.TotalBought
			pct := profit / pos.Returns.TotalBought * 100
			p.notifier.Notify(ticker, pos.Name, true, profit, pct)
		}
	}

	// Detect edge: return went negative.
	for ticker, pos := range nowBelow {
		if !p.prevBelow[ticker] {
			loss := pos.Returns.TotalBought - pos.CurrentValueGBP
			pct := (pos.CurrentValueGBP - pos.Returns.TotalBought) / pos.Returns.TotalBought * 100
			p.notifier.Notify(ticker, pos.Name, false, loss, pct)
		}
	}

	// Update previous state.
	newAbove := make(map[string]bool, len(nowAbove))
	for ticker := range nowAbove {
		newAbove[ticker] = true
	}
	p.prevAbove = newAbove

	newBelow := make(map[string]bool, len(nowBelow))
	for ticker := range nowBelow {
		newBelow[ticker] = true
	}
	p.prevBelow = newBelow
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
			valGBP := findCurrentValueGBP(positions, t)
			all[t] = history.ComputeReturns(ordersByTicker[t], divsByTicker[t], valGBP)
		}
		p.historyStore.SetAll(all)
	} else {
		valGBP := findCurrentValueGBP(positions, ticker)
		ri := history.ComputeReturns(orders, divs, valGBP)
		p.historyStore.Set(ticker, ri)
	}
}

func findCurrentValueGBP(positions []api.Position, ticker string) float64 {
	for _, p := range positions {
		if p.Ticker == ticker {
			return p.CurrentValueGBP
		}
	}
	return 0
}
