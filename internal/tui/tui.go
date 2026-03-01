package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gorilla/websocket"
	"github.com/ko5tas/t212/internal/api"
)

// WSMessage matches the BroadcastMessage sent by the poller.
type WSMessage struct {
	Timestamp time.Time      `json:"timestamp"`
	Positions []api.Position `json:"positions"`
}

// Model is the bubbletea model. All state transitions happen via ApplyMessage
// so they are testable without bubbletea I/O.
type Model struct {
	positions []api.Position
	updated   time.Time
	err       error
	conn      *websocket.Conn
	cursor    int
}

// NewModel returns an empty Model.
func NewModel() Model {
	return Model{positions: []api.Position{}}
}

// Positions returns the current filtered positions.
func (m Model) Positions() []api.Position { return m.positions }

// LastUpdated returns the timestamp of the last received message.
func (m Model) LastUpdated() time.Time { return m.updated }

// Cursor returns the current cursor position (row index).
func (m Model) Cursor() int { return m.cursor }

// ApplyMessage parses a raw WebSocket JSON message and returns an updated Model.
// Invalid JSON is silently ignored (model unchanged).
func (m Model) ApplyMessage(raw []byte) Model {
	var msg WSMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return m
	}
	if msg.Positions == nil {
		msg.Positions = []api.Position{}
	}
	m.positions = msg.Positions
	m.updated = msg.Timestamp
	return m
}

// --- bubbletea plumbing ---

type msgReceived []byte
type errMsg error

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update handles bubbletea messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		switch v.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			if m.conn != nil && len(m.positions) > 0 && m.cursor < len(m.positions) {
				ticker := m.positions[m.cursor].Ticker
				m.conn.WriteMessage(websocket.TextMessage,
					[]byte(`{"action":"refresh","ticker":"`+ticker+`"}`))
			}
		case "R":
			if m.conn != nil {
				m.conn.WriteMessage(websocket.TextMessage,
					[]byte(`{"action":"refresh_all"}`))
			}
		case "j", "down":
			if m.cursor < len(m.positions)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		}
	case msgReceived:
		m = m.ApplyMessage(v)
	case errMsg:
		m.err = v
		return m, tea.Quit
	}
	return m, nil
}

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#94a3b8"))
	profitStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80")).Bold(true)
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e2e8f0"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#475569"))
)

// View renders the TUI.
func (m Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n", m.err)
	}

	out := titleStyle.Render("T212 Dashboard") + "\n"
	out += dimStyle.Render("Positions with profit > 1/share  [r: refresh stock | R: refresh all | j/k: navigate | q: quit]") + "\n\n"

	if len(m.positions) == 0 {
		out += dimStyle.Render("No positions above threshold") + "\n"
	} else {
		out += fmt.Sprintf("  %-20s %10s %10s %10s %10s %12s %13s %14s %14s\n",
			headerStyle.Render("TICKER"),
			headerStyle.Render("RETURN"),
			headerStyle.Render("RETURN %"),
			headerStyle.Render("NET ROI %"),
			headerStyle.Render("QTY"),
			headerStyle.Render("AVG PRICE"),
			headerStyle.Render("CURR PRICE"),
			headerStyle.Render("PROFIT/SHR"),
			headerStyle.Render("MKT VALUE"),
		)
		for i, p := range m.positions {
			sym := p.CurrencySymbol()
			marker := "  "
			if i == m.cursor {
				marker = "> "
			}
			retStr := fmt.Sprintf("%10s %10s %10s", "--", "--", "--")
			if p.Returns != nil {
				retStr = fmt.Sprintf("%10.2f %9.2f%% %9.2f%%",
					p.Returns.Return, p.Returns.ReturnPct, p.Returns.NetROIPct)
			}
			out += fmt.Sprintf("%s%-20s %s %10.4f %s%11.2f %s%12.2f %s %s%13.2f\n",
				marker,
				p.Ticker,
				retStr,
				p.Quantity,
				sym, p.AveragePrice,
				sym, p.CurrentPrice,
				profitStyle.Render(fmt.Sprintf("%s%+12.2f", sym, p.ProfitPerShare)),
				sym, p.MarketValue,
			)
		}
	}

	if !m.updated.IsZero() {
		out += "\n" + dimStyle.Render("Last updated: "+m.updated.Local().Format("15:04:05"))
	}
	return out
}

// Run connects to the WebSocket server and runs the bubbletea program.
// This function is not unit-tested (requires a live server and terminal).
func Run(ctx context.Context, wsURL string) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", wsURL, err)
	}
	defer conn.Close()

	m := NewModel()
	m.conn = conn
	p := tea.NewProgram(m, tea.WithAltScreen())

	go func() {
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				// Normal closure (user quit) — don't log as error.
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					return
				}
				slog.Error("ws read error", "err", err)
				p.Send(errMsg(err))
				return
			}
			p.Send(msgReceived(raw))
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}
