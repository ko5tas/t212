package poller

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/ko5tas/t212/internal/api"
	"github.com/ko5tas/t212/internal/filter"
	"github.com/ko5tas/t212/internal/hub"
	"github.com/ko5tas/t212/internal/store"
)

const pollInterval = time.Second

// BroadcastMessage is the JSON payload sent to WebSocket subscribers on each poll.
type BroadcastMessage struct {
	Timestamp time.Time      `json:"timestamp"`
	Positions []api.Position `json:"positions"`
}

// Poller polls the T212 API, updates the store, and broadcasts filtered positions.
type Poller struct {
	client    *api.Client
	store     *store.Store
	hub       *hub.Hub
	threshold float64
	notifier  Notifier
	prevAbove map[string]bool
}

// New creates a Poller. notifier may be nil (no alerts sent).
func New(client *api.Client, s *store.Store, h *hub.Hub, threshold float64, n Notifier) *Poller {
	return &Poller{
		client:    client,
		store:     s,
		hub:       h,
		threshold: threshold,
		notifier:  n,
		prevAbove: make(map[string]bool),
	}
}

// Run starts the polling loop. Blocks until ctx is cancelled.
// An immediate poll is performed before waiting for the first tick.
func (p *Poller) Run(ctx context.Context) {
	p.poll(ctx)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
			p.notifier.Notify(ticker, true, pos.ProfitPerShare)
		}
	}

	// Detect edge: exited threshold.
	for ticker := range p.prevAbove {
		if _, ok := nowAbove[ticker]; !ok {
			p.notifier.Notify(ticker, false, 0)
		}
	}

	// Update previous state.
	newPrev := make(map[string]bool, len(nowAbove))
	for ticker := range nowAbove {
		newPrev[ticker] = true
	}
	p.prevAbove = newPrev
}
