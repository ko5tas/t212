package history_test

import (
	"math"
	"testing"

	"github.com/ko5tas/t212/internal/api"
	"github.com/ko5tas/t212/internal/history"
)

func approx(a, b float64) bool {
	return math.Abs(a-b) < 0.01
}

// GBP stock: buy £100 for 10 shares at £10 each, sell 3 shares for £35,
// dividends £7.30. Still hold 7 shares, currentValueGBP = £84 (12*7).
func TestComputeReturns_GBP_BuysAndSells(t *testing.T) {
	orders := []api.HistoricalOrder{
		makeOrder("BUY", 100.0, 10),  // £100 for 10 shares
		makeOrder("SELL", 35.0, 3),   // sold 3 shares for £35
	}
	divs := []api.DividendItem{{Amount: 7.30}}

	// currentValueGBP = 84 (from T212 walletImpact.currentValue)
	ri := history.ComputeReturns(orders, divs, 84.0)

	if !approx(ri.TotalBought, 100.0) {
		t.Errorf("TotalBought: got %v, want 100.0", ri.TotalBought)
	}
	if !approx(ri.TotalSold, 35.0) {
		t.Errorf("TotalSold: got %v, want 35.0", ri.TotalSold)
	}
	if !approx(ri.TotalDividends, 7.30) {
		t.Errorf("TotalDividends: got %v, want 7.30", ri.TotalDividends)
	}
	// Return = 84 + 35 + 7.30 - 100 = 26.30
	if !approx(ri.Return, 26.30) {
		t.Errorf("Return: got %v, want 26.30", ri.Return)
	}
	// ReturnPct = 26.30 / 100 * 100 = 26.30
	if !approx(ri.ReturnPct, 26.30) {
		t.Errorf("ReturnPct: got %v, want 26.30", ri.ReturnPct)
	}
}

// USD stock: buy 3.39 shares for £100. T212 says current GBP value = £47.53.
// Matches the user's USAR example exactly.
func TestComputeReturns_USD_UnrealisedLoss(t *testing.T) {
	orders := []api.HistoricalOrder{
		makeOrder("BUY", 100.0, 3.39421173),
	}

	// currentValueGBP = 47.53 (from T212 walletImpact, includes FX + fees)
	ri := history.ComputeReturns(orders, nil, 47.53)

	// Return = 47.53 + 0 + 0 - 100 = -52.47
	if !approx(ri.Return, -52.47) {
		t.Errorf("Return: got %v, want -52.47", ri.Return)
	}
	// ReturnPct = -52.47 / 100 * 100 = -52.47
	if !approx(ri.ReturnPct, -52.47) {
		t.Errorf("ReturnPct: got %v, want -52.47%%", ri.ReturnPct)
	}
}

// All shares sold — purely realised.
func TestComputeReturns_AllSold(t *testing.T) {
	orders := []api.HistoricalOrder{
		makeOrder("BUY", 100.0, 2),
		makeOrder("SELL", 120.0, 2),
	}

	// currentValueGBP = 0 (no shares held)
	ri := history.ComputeReturns(orders, nil, 0)

	// Return = 0 + 120 + 0 - 100 = 20
	if !approx(ri.Return, 20.0) {
		t.Errorf("Return: got %v, want 20.0", ri.Return)
	}
	if !approx(ri.ReturnPct, 20.0) {
		t.Errorf("ReturnPct: got %v, want 20.0", ri.ReturnPct)
	}
}

func TestComputeReturns_NoBuys(t *testing.T) {
	ri := history.ComputeReturns(nil, nil, 0)
	if ri.ReturnPct != 0 {
		t.Errorf("ReturnPct should be 0 with no buys, got %v", ri.ReturnPct)
	}
}

func TestComputeReturns_OnlyDividends(t *testing.T) {
	orders := []api.HistoricalOrder{
		makeOrder("BUY", 200.0, 2),
	}
	divs := []api.DividendItem{{Amount: 10.0}}

	// currentValueGBP = 210 (from T212 walletImpact)
	ri := history.ComputeReturns(orders, divs, 210.0)

	// Return = 210 + 0 + 10 - 200 = 20
	if !approx(ri.Return, 20.0) {
		t.Errorf("Return: got %v, want 20.0", ri.Return)
	}
}

func TestComputeReturns_SkipsZeroQuantityFills(t *testing.T) {
	orders := []api.HistoricalOrder{
		makeOrder("BUY", 100.0, 2),
		makeOrder("SELL", 50.0, 0), // zero fill qty — not a real fill
	}
	ri := history.ComputeReturns(orders, nil, 100.0)
	if ri.TotalSold != 0 {
		t.Errorf("zero-quantity fill should be ignored, got TotalSold=%v", ri.TotalSold)
	}
}

func makeOrder(side string, netValue, fillQty float64) api.HistoricalOrder {
	var o api.HistoricalOrder
	o.Order.Side = side
	o.Fill.Impact.NetValue = netValue
	o.Fill.Quantity = fillQty
	return o
}
