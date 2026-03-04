package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gorilla/websocket"
	"github.com/ko5tas/t212/internal/api"
)

// WSMessage matches the BroadcastMessage sent by the poller.
type WSMessage struct {
	Timestamp       time.Time            `json:"timestamp"`
	Positions       []api.Position       `json:"positions"`
	ClosedPositions []api.ClosedPosition `json:"closedPositions"`
}

// ClosedSortColumn identifies which column the closed positions table is sorted by.
type ClosedSortColumn int

const (
	ClosedSortTicker ClosedSortColumn = iota
	ClosedSortName
	ClosedSortExchange
	ClosedSortReturn
	ClosedSortReturnPct
	closedSortColumnCount
)

// SortColumn identifies which column the table is sorted by.
type SortColumn int

const (
	SortTicker SortColumn = iota
	SortName
	SortReturn
	SortReturnPct
	SortNetROI
	SortQuantity
	SortCurrentPrice
	SortAvgPrice
	SortProfitPerShare
	SortMarketValue
	SortExchange
	sortColumnCount // sentinel
)

// String returns the column header name.
func (s SortColumn) String() string {
	switch s {
	case SortTicker:
		return "TICKER"
	case SortName:
		return "NAME"
	case SortReturn:
		return "RETURN"
	case SortReturnPct:
		return "RETURN %"
	case SortNetROI:
		return "NET ROI %"
	case SortQuantity:
		return "QTY"
	case SortCurrentPrice:
		return "CURR PRICE"
	case SortAvgPrice:
		return "AVG PRICE"
	case SortProfitPerShare:
		return "PROFIT/SHR"
	case SortMarketValue:
		return "MKT VALUE"
	case SortExchange:
		return "EXCHANGE"
	}
	return ""
}

// Model is the bubbletea model. All state transitions happen via ApplyMessage
// so they are testable without bubbletea I/O.
type Model struct {
	positions       []api.Position
	closedPositions []api.ClosedPosition
	showClosed      bool
	updated         time.Time
	err             error
	conn            *websocket.Conn
	cursor          int
	sortCol         SortColumn
	sortAsc         bool
	closedSortCol   ClosedSortColumn
	closedSortAsc   bool
}

// NewModel returns an empty Model.
func NewModel() Model {
	return Model{
		positions:       []api.Position{},
		closedPositions: []api.ClosedPosition{},
		sortCol:         SortMarketValue,
		closedSortCol:   ClosedSortReturn,
	}
}

// Positions returns the current positions.
func (m Model) Positions() []api.Position { return m.positions }

// LastUpdated returns the timestamp of the last received message.
func (m Model) LastUpdated() time.Time { return m.updated }

// Cursor returns the current cursor position (row index).
func (m Model) Cursor() int { return m.cursor }

// SortCol returns the current sort column.
func (m Model) SortCol() SortColumn { return m.sortCol }

// SortAsc returns whether sorting is ascending.
func (m Model) SortAsc() bool { return m.sortAsc }

// ShowClosed returns whether the closed positions tab is active.
func (m Model) ShowClosed() bool { return m.showClosed }

// ClosedPositions returns the current closed positions.
func (m Model) ClosedPositions() []api.ClosedPosition { return m.closedPositions }

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
	if msg.ClosedPositions == nil {
		msg.ClosedPositions = []api.ClosedPosition{}
	}
	m.positions = msg.Positions
	m.closedPositions = msg.ClosedPositions
	m.updated = msg.Timestamp
	m.sortPositions()
	m.sortClosedPositions()
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
		case "tab":
			m.showClosed = !m.showClosed
			m.cursor = 0
		case "r":
			if !m.showClosed && m.conn != nil && len(m.positions) > 0 && m.cursor < len(m.positions) {
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
			maxIdx := len(m.positions) - 1
			if m.showClosed {
				maxIdx = len(m.closedPositions) - 1
			}
			if m.cursor < maxIdx {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "s":
			if m.showClosed {
				m.closedSortCol = (m.closedSortCol + 1) % closedSortColumnCount
				m.closedSortAsc = false
				m.sortClosedPositions()
			} else {
				m.sortCol = (m.sortCol + 1) % sortColumnCount
				m.sortAsc = false
				m.sortPositions()
			}
		case "S":
			if m.showClosed {
				m.closedSortAsc = !m.closedSortAsc
				m.sortClosedPositions()
			} else {
				m.sortAsc = !m.sortAsc
				m.sortPositions()
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

func (m *Model) sortPositions() {
	col := m.sortCol
	asc := m.sortAsc
	sort.SliceStable(m.positions, func(i, j int) bool {
		less := posLess(m.positions[i], m.positions[j], col)
		if asc {
			return less
		}
		return !less
	})
}

func returnVal(p api.Position) float64 {
	if p.Returns == nil {
		return 0
	}
	return p.Returns.Return
}

func returnPctVal(p api.Position) float64 {
	if p.Returns == nil {
		return 0
	}
	return p.Returns.ReturnPct
}

func netROIVal(p api.Position) float64 {
	if p.Returns == nil {
		return 0
	}
	return p.Returns.NetROIPct
}

func posLess(a, b api.Position, col SortColumn) bool {
	switch col {
	case SortTicker:
		return a.Ticker < b.Ticker
	case SortName:
		return a.Name < b.Name
	case SortReturn:
		return returnVal(a) < returnVal(b)
	case SortReturnPct:
		return returnPctVal(a) < returnPctVal(b)
	case SortNetROI:
		return netROIVal(a) < netROIVal(b)
	case SortQuantity:
		return a.Quantity < b.Quantity
	case SortCurrentPrice:
		return a.CurrentPrice < b.CurrentPrice
	case SortAvgPrice:
		return a.AveragePrice < b.AveragePrice
	case SortProfitPerShare:
		return a.ProfitPerShare < b.ProfitPerShare
	case SortMarketValue:
		return a.CurrentValueGBP < b.CurrentValueGBP
	case SortExchange:
		return a.Exchange < b.Exchange
	}
	return false
}

func (m *Model) sortClosedPositions() {
	col := m.closedSortCol
	asc := m.closedSortAsc
	sort.SliceStable(m.closedPositions, func(i, j int) bool {
		less := closedPosLess(m.closedPositions[i], m.closedPositions[j], col)
		if asc {
			return less
		}
		return !less
	})
}

func closedReturnVal(p api.ClosedPosition) float64 {
	if p.Returns == nil {
		return 0
	}
	return p.Returns.Return
}

func closedReturnPctVal(p api.ClosedPosition) float64 {
	if p.Returns == nil {
		return 0
	}
	return p.Returns.ReturnPct
}

func closedPosLess(a, b api.ClosedPosition, col ClosedSortColumn) bool {
	switch col {
	case ClosedSortTicker:
		return a.Ticker < b.Ticker
	case ClosedSortName:
		return a.Name < b.Name
	case ClosedSortExchange:
		return a.Exchange < b.Exchange
	case ClosedSortReturn:
		return closedReturnVal(a) < closedReturnVal(b)
	case ClosedSortReturnPct:
		return closedReturnPctVal(a) < closedReturnPctVal(b)
	}
	return false
}

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#94a3b8"))
	profitStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80")).Bold(true)
	profitBlinkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80")).Bold(true).Blink(true)
	lossStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171")).Bold(true)
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e2e8f0"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#475569"))
	totalStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e2e8f0")).
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(lipgloss.Color("#475569")).
			PaddingTop(0)
)

func (m Model) renderHeader(name string, col SortColumn, width int) string {
	indicator := ""
	if m.sortCol == col {
		if m.sortAsc {
			indicator = " ▲"
		} else {
			indicator = " ▼"
		}
	}
	padded := fmt.Sprintf("%-*s", width, name+indicator)
	return headerStyle.Render(padded)
}

func (m Model) renderClosedHeader(name string, col ClosedSortColumn, width int) string {
	indicator := ""
	if m.closedSortCol == col {
		if m.closedSortAsc {
			indicator = " ▲"
		} else {
			indicator = " ▼"
		}
	}
	padded := fmt.Sprintf("%-*s", width, name+indicator)
	return headerStyle.Render(padded)
}

// View renders the TUI.
func (m Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n", m.err)
	}

	out := titleStyle.Render("T212 Dashboard") + "\n"
	out += dimStyle.Render("[Tab: switch view | r: refresh stock | R: refresh all | j/k: navigate | s/S: sort | q: quit]") + "\n"

	activeLabel := "Active"
	closedLabel := "Closed"
	if m.showClosed {
		activeLabel = dimStyle.Render("Active")
		closedLabel = titleStyle.Render("[Closed]")
	} else {
		activeLabel = titleStyle.Render("[Active]")
		closedLabel = dimStyle.Render("Closed")
	}
	out += activeLabel + "  " + closedLabel + "\n\n"

	if m.showClosed {
		return out + m.viewClosed()
	}

	if len(m.positions) == 0 {
		out += dimStyle.Render("No positions") + "\n"
	} else {
		out += "     " +
			m.renderHeader("TICKER", SortTicker, 16) + " " +
			m.renderHeader("NAME", SortName, 24) + " " +
			m.renderHeader("MKT VALUE", SortMarketValue, 20) + " " +
			m.renderHeader("EXCHANGE", SortExchange, 16) + " " +
			m.renderHeader("RETURN", SortReturn, 10) + " " +
			m.renderHeader("RETURN %", SortReturnPct, 10) + " " +
			m.renderHeader("NET ROI %", SortNetROI, 10) + " " +
			m.renderHeader("QTY", SortQuantity, 10) + " " +
			m.renderHeader("CURR PRICE", SortCurrentPrice, 13) + " " +
			m.renderHeader("AVG PRICE", SortAvgPrice, 12) + " " +
			m.renderHeader("PROFIT/SHR", SortProfitPerShare, 14) + "\n"
		var totalReturn, totalBought, totalValueGBP float64
		for i, p := range m.positions {
			sym := p.CurrencySymbol()
			marker := " "
			if i == m.cursor {
				marker = ">"
			}
			retStr := fmt.Sprintf("%10s %10s %10s", "--", "--", "--")
			if p.Returns != nil {
				retVal := fmt.Sprintf("%10.2f", p.Returns.Return)
				retPct := fmt.Sprintf("%9.2f%%", p.Returns.ReturnPct)
				roiPct := fmt.Sprintf("%9.2f%%", p.Returns.NetROIPct)
				if p.Returns.ReturnPct > 50 {
					retVal = profitStyle.Render(retVal)
					retPct = profitStyle.Render(retPct)
				} else if p.Returns.Return < 0 {
					retVal = lossStyle.Render(retVal)
					retPct = lossStyle.Render(retPct)
				}
				if p.Returns.NetROIPct > 50 {
					roiPct = profitStyle.Render(roiPct)
				} else if p.Returns.NetROIPct < 0 {
					roiPct = lossStyle.Render(roiPct)
				}
				retStr = fmt.Sprintf("%s %s %s", retVal, retPct, roiPct)
				totalReturn += p.Returns.Return
				totalBought += p.Returns.TotalBought
			}
			ppsStr := fmt.Sprintf("%s%+12.2f", sym, p.ProfitPerShare)
			if p.ProfitPerShare >= 0 {
				ppsStr = profitStyle.Render(ppsStr)
			} else {
				ppsStr = lossStyle.Render(ppsStr)
			}
			name := p.Name
			if len(name) > 22 {
				name = name[:22]
			}
			totalValueGBP += p.CurrentValueGBP
			mvStr := fmt.Sprintf("£%.2f", p.CurrentValueGBP)
			if p.Currency != "" && p.Currency != "GBP" {
				mvStr += fmt.Sprintf(" (%s%.2f)", sym, p.MarketValue)
			}
			mvStr = fmt.Sprintf("%-20s", mvStr)
			if p.Returns != nil && p.CurrentValueGBP > p.Returns.TotalBought+1 {
				mvStr = profitBlinkStyle.Render(mvStr)
			}
			exchStr := fmt.Sprintf("%-16s", p.Exchange)
			out += fmt.Sprintf("%s%3d %-16s %-24s %s %s %s %10.4f %s%12.2f %s%11.2f %s\n",
				marker,
				i+1,
				p.Ticker,
				name,
				mvStr,
				exchStr,
				retStr,
				p.Quantity,
				sym, p.CurrentPrice,
				sym, p.AveragePrice,
				ppsStr,
			)
		}
		// Totals row
		var totalRetPct float64
		if totalBought > 0 {
			totalRetPct = totalReturn / totalBought * 100
		}
		totalRetStr := fmt.Sprintf("%10.2f", totalReturn)
		totalPctStr := fmt.Sprintf("%9.2f%%", totalRetPct)
		if totalRetPct > 50 {
			totalRetStr = profitStyle.Render(totalRetStr)
			totalPctStr = profitStyle.Render(totalPctStr)
		} else if totalReturn < 0 {
			totalRetStr = lossStyle.Render(totalRetStr)
			totalPctStr = lossStyle.Render(totalPctStr)
		}
		totalValGBPStr := fmt.Sprintf("%-20s", fmt.Sprintf("£%.2f", totalValueGBP))
		totalsLine := fmt.Sprintf("     %-16s %-24s %s %-16s %s %s %10s %10s %13s %12s %14s",
			"TOTAL",
			"",
			totalValGBPStr,
			"",
			totalRetStr,
			totalPctStr,
			"", "", "", "", "",
		)
		out += totalStyle.Render(totalsLine) + "\n"
	}

	if !m.updated.IsZero() {
		out += "\n" + dimStyle.Render("Last updated: "+m.updated.Local().Format("15:04:05"))
	}
	return out
}

func (m Model) viewClosed() string {
	out := ""
	if len(m.closedPositions) == 0 {
		out += dimStyle.Render("No closed positions") + "\n"
	} else {
		out += "     " +
			m.renderClosedHeader("TICKER", ClosedSortTicker, 16) + " " +
			m.renderClosedHeader("NAME", ClosedSortName, 30) + " " +
			m.renderClosedHeader("EXCHANGE", ClosedSortExchange, 20) + " " +
			m.renderClosedHeader("RETURN", ClosedSortReturn, 12) + " " +
			m.renderClosedHeader("RETURN %", ClosedSortReturnPct, 10) + "\n"
		var totalReturn, totalBought float64
		for i, p := range m.closedPositions {
			marker := " "
			if i == m.cursor {
				marker = ">"
			}
			retVal := fmt.Sprintf("%12s", "--")
			retPct := fmt.Sprintf("%10s", "--")
			if p.Returns != nil {
				rv := fmt.Sprintf("%12.2f", p.Returns.Return)
				rp := fmt.Sprintf("%9.2f%%", p.Returns.ReturnPct)
				if p.Returns.ReturnPct > 50 {
					rv = profitStyle.Render(rv)
					rp = profitStyle.Render(rp)
				} else if p.Returns.Return < 0 {
					rv = lossStyle.Render(rv)
					rp = lossStyle.Render(rp)
				}
				retVal = rv
				retPct = rp
				totalReturn += p.Returns.Return
				totalBought += p.Returns.TotalBought
			}
			name := p.Name
			if len(name) > 28 {
				name = name[:28]
			}
			out += fmt.Sprintf("%s%3d %-16s %-30s %-20s %s %s\n",
				marker,
				i+1,
				p.Ticker,
				name,
				p.Exchange,
				retVal,
				retPct,
			)
		}
		// Totals row
		var totalRetPct float64
		if totalBought > 0 {
			totalRetPct = totalReturn / totalBought * 100
		}
		totalRetStr := fmt.Sprintf("%12.2f", totalReturn)
		totalPctStr := fmt.Sprintf("%9.2f%%", totalRetPct)
		if totalRetPct > 50 {
			totalRetStr = profitStyle.Render(totalRetStr)
			totalPctStr = profitStyle.Render(totalPctStr)
		} else if totalReturn < 0 {
			totalRetStr = lossStyle.Render(totalRetStr)
			totalPctStr = lossStyle.Render(totalPctStr)
		}
		totalsLine := fmt.Sprintf("     %-16s %-30s %-20s %s %s",
			"TOTAL", "", "", totalRetStr, totalPctStr,
		)
		out += totalStyle.Render(totalsLine) + "\n"
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
