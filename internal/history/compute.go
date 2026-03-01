package history

import "github.com/ko5tas/t212/internal/api"

// ComputeReturns aggregates order fills and dividends into a ReturnInfo.
// unrealisedPPS is the current ProfitPerShare and qty is the current quantity held.
//
// Return = sold + dividends - bought  (net realised cash flow; negative means
// investment not yet recovered).
// ReturnPct = Return / bought * 100.
// NetROIPct = (Return + unrealised) / bought * 100  (total P&L as % of investment).
func ComputeReturns(orders []api.HistoricalOrder, divs []api.DividendItem, unrealisedPPS, qty float64) api.ReturnInfo {
	var bought, sold float64
	for _, o := range orders {
		if o.Order.Status != "FILLED" {
			continue
		}
		v := o.Fill.Impact.NetValue
		if v < 0 {
			v = -v
		}
		switch o.Order.Side {
		case "BUY":
			bought += v
		case "SELL":
			sold += v
		}
	}

	var dividends float64
	for _, d := range divs {
		dividends += d.Amount
	}

	ret := sold + dividends - bought
	var retPct, netROI float64
	if bought > 0 {
		retPct = ret / bought * 100
		unrealised := unrealisedPPS * qty
		netROI = (ret + unrealised) / bought * 100
	}

	return api.ReturnInfo{
		TotalBought:    bought,
		TotalSold:      sold,
		TotalDividends: dividends,
		Return:         ret,
		ReturnPct:      retPct,
		NetROIPct:      netROI,
	}
}
