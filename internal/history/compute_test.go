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

func TestComputeReturns_BuysAndSells(t *testing.T) {
	orders := []api.HistoricalOrder{
		makeOrder("BUY", 100.0),
		makeOrder("SELL", 35.0),
	}
	divs := []api.DividendItem{{Amount: 7.30}}

	ri := history.ComputeReturns(orders, divs, 2.50, 3.0)

	if !approx(ri.TotalBought, 100.0) {
		t.Errorf("TotalBought: got %v, want 100.0", ri.TotalBought)
	}
	if !approx(ri.TotalSold, 35.0) {
		t.Errorf("TotalSold: got %v, want 35.0", ri.TotalSold)
	}
	if !approx(ri.TotalDividends, 7.30) {
		t.Errorf("TotalDividends: got %v, want 7.30", ri.TotalDividends)
	}
	// Return = sold + dividends - bought = 35 + 7.30 - 100 = -57.70
	if !approx(ri.Return, -57.70) {
		t.Errorf("Return: got %v, want -57.70", ri.Return)
	}
	// ReturnPct = -57.70 / 100 * 100 = -57.70
	if !approx(ri.ReturnPct, -57.70) {
		t.Errorf("ReturnPct: got %v, want -57.70", ri.ReturnPct)
	}
	// NetROIPct = (-57.70 + 7.50) / 100 * 100 = -50.20
	if !approx(ri.NetROIPct, -50.20) {
		t.Errorf("NetROIPct: got %v, want ~-50.20", ri.NetROIPct)
	}
}

func TestComputeReturns_NoBuys(t *testing.T) {
	ri := history.ComputeReturns(nil, nil, 0, 0)
	if ri.ReturnPct != 0 {
		t.Errorf("ReturnPct should be 0 with no buys, got %v", ri.ReturnPct)
	}
}

func TestComputeReturns_OnlyDividends(t *testing.T) {
	orders := []api.HistoricalOrder{makeOrder("BUY", 200.0)}
	divs := []api.DividendItem{{Amount: 10.0}}
	ri := history.ComputeReturns(orders, divs, 5.0, 2.0)
	// Return = 0 (sold) + 10 (divs) - 200 (bought) = -190
	if !approx(ri.Return, -190.0) {
		t.Errorf("Return: got %v, want -190.0", ri.Return)
	}
}

func TestComputeReturns_SkipsNonFilled(t *testing.T) {
	orders := []api.HistoricalOrder{
		makeOrder("BUY", 100.0),
		makeOrderWithStatus("SELL", 50.0, "CANCELLED"),
	}
	ri := history.ComputeReturns(orders, nil, 0, 0)
	if ri.TotalSold != 0 {
		t.Errorf("cancelled sell should be ignored, got TotalSold=%v", ri.TotalSold)
	}
}

func makeOrder(side string, netValue float64) api.HistoricalOrder {
	return makeOrderWithStatus(side, netValue, "FILLED")
}

func makeOrderWithStatus(side string, netValue float64, status string) api.HistoricalOrder {
	var o api.HistoricalOrder
	o.Order.Side = side
	o.Order.Status = status
	o.Fill.Impact.NetValue = netValue
	return o
}
