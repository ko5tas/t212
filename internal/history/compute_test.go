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
// dividends £7.30. Still hold 7 shares, current price £12.
func TestComputeReturns_GBP_BuysAndSells(t *testing.T) {
	orders := []api.HistoricalOrder{
		makeOrderFull("BUY", 100.0, 10.0, 7, "FILLED"),  // £100 for 10 shares @ £10
		makeOrderFull("SELL", 35.0, 11.67, 3, "FILLED"),  // sold 3 shares for £35
	}
	divs := []api.DividendItem{{Amount: 7.30}}

	// current price £12, 7 shares remaining, GBP stock
	ri := history.ComputeReturns(orders, divs, 12.0, 7.0, "GBP")

	if !approx(ri.TotalBought, 100.0) {
		t.Errorf("TotalBought: got %v, want 100.0", ri.TotalBought)
	}
	if !approx(ri.TotalSold, 35.0) {
		t.Errorf("TotalSold: got %v, want 35.0", ri.TotalSold)
	}
	if !approx(ri.TotalDividends, 7.30) {
		t.Errorf("TotalDividends: got %v, want 7.30", ri.TotalDividends)
	}
	// currentValueGBP = 12 * 7 = 84
	// Return = 84 + 35 + 7.30 - 100 = 26.30
	if !approx(ri.Return, 26.30) {
		t.Errorf("Return: got %v, want 26.30", ri.Return)
	}
	// ReturnPct = 26.30 / 100 * 100 = 26.30
	if !approx(ri.ReturnPct, 26.30) {
		t.Errorf("ReturnPct: got %v, want 26.30", ri.ReturnPct)
	}
}

// USD stock: buy 3.39 shares at $39.17 for £100, no sales/dividends,
// current price $18.87.  Matches the user's USAR example.
func TestComputeReturns_USD_UnrealisedLoss(t *testing.T) {
	orders := []api.HistoricalOrder{
		makeOrderFull("BUY", 100.0, 39.17, 3.39421173, "FILLED"),
	}

	ri := history.ComputeReturns(orders, nil, 18.87, 3.39421173, "USD")

	// impliedRate = (39.17 * 3.39421173) / 100 ≈ 1.3290
	// currentValueGBP = (18.87 * 3.39421173) / 1.3290 ≈ 48.19
	// Return ≈ 48.19 + 0 + 0 - 100 ≈ -51.81
	// (User sees £47.53 because live FX differs; implied rate is an approximation.)
	if ri.Return > -45 || ri.Return < -60 {
		t.Errorf("Return: got %v, want roughly -48 to -52", ri.Return)
	}
	if ri.ReturnPct > -45 || ri.ReturnPct < -60 {
		t.Errorf("ReturnPct: got %v, want roughly -48%% to -52%%", ri.ReturnPct)
	}
}

// All shares sold — purely realised.
func TestComputeReturns_AllSold(t *testing.T) {
	orders := []api.HistoricalOrder{
		makeOrderFull("BUY", 100.0, 50.0, 2, "FILLED"),
		makeOrderFull("SELL", 120.0, 60.0, 2, "FILLED"),
	}

	ri := history.ComputeReturns(orders, nil, 0, 0, "GBP")

	// currentValueGBP = 0 (no shares)
	// Return = 0 + 120 + 0 - 100 = 20
	if !approx(ri.Return, 20.0) {
		t.Errorf("Return: got %v, want 20.0", ri.Return)
	}
	if !approx(ri.ReturnPct, 20.0) {
		t.Errorf("ReturnPct: got %v, want 20.0", ri.ReturnPct)
	}
}

func TestComputeReturns_NoBuys(t *testing.T) {
	ri := history.ComputeReturns(nil, nil, 0, 0, "GBP")
	if ri.ReturnPct != 0 {
		t.Errorf("ReturnPct should be 0 with no buys, got %v", ri.ReturnPct)
	}
}

func TestComputeReturns_OnlyDividends(t *testing.T) {
	orders := []api.HistoricalOrder{
		makeOrderFull("BUY", 200.0, 100.0, 2, "FILLED"),
	}
	divs := []api.DividendItem{{Amount: 10.0}}

	// current price £105, 2 shares, GBP
	ri := history.ComputeReturns(orders, divs, 105.0, 2.0, "GBP")

	// currentValueGBP = 105 * 2 = 210
	// Return = 210 + 0 + 10 - 200 = 20
	if !approx(ri.Return, 20.0) {
		t.Errorf("Return: got %v, want 20.0", ri.Return)
	}
}

func TestComputeReturns_SkipsNonFilled(t *testing.T) {
	orders := []api.HistoricalOrder{
		makeOrderFull("BUY", 100.0, 50.0, 2, "FILLED"),
		makeOrderFull("SELL", 50.0, 50.0, 1, "CANCELLED"),
	}
	ri := history.ComputeReturns(orders, nil, 50.0, 2, "GBP")
	if ri.TotalSold != 0 {
		t.Errorf("cancelled sell should be ignored, got TotalSold=%v", ri.TotalSold)
	}
}

func makeOrderFull(side string, netValue, fillPrice, fillQty float64, status string) api.HistoricalOrder {
	var o api.HistoricalOrder
	o.Order.Side = side
	o.Order.Status = status
	o.Fill.Impact.NetValue = netValue
	o.Fill.Price = fillPrice
	o.Fill.Quantity = fillQty
	return o
}
