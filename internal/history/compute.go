package history

import (
	"log/slog"
	"strings"

	"github.com/ko5tas/t212/internal/api"
)

// ComputeReturns aggregates order fills and dividends into a ReturnInfo.
//
// currentValueGBP is the current market value of the remaining holding in GBP,
// sourced from the T212 positions API walletImpact.currentValue. This includes
// live FX conversion and platform fees, so no manual FX calculation is needed.
//
// Return = currentValueGBP + sold + dividends − bought.
// ReturnPct = Return / bought × 100.
func ComputeReturns(orders []api.HistoricalOrder, divs []api.DividendItem, currentValueGBP float64) api.ReturnInfo {
	var bought, sold float64

	for _, o := range orders {
		// Historical orders from T212 are all completed; filter on fill
		// quantity to skip any non-trade entries.
		if o.Fill.Quantity == 0 {
			continue
		}
		v := o.Fill.Impact.NetValue
		if v < 0 {
			v = -v
		}
		side := strings.ToUpper(o.Order.Side)
		switch side {
		case "BUY":
			bought += v
		case "SELL":
			sold += v
		default:
			slog.Warn("unknown order side", "side", o.Order.Side, "ticker", o.Order.Ticker, "quantity", o.Fill.Quantity, "netValue", o.Fill.Impact.NetValue)
		}
	}

	var dividends float64
	for _, d := range divs {
		dividends += d.Amount
	}

	ret := currentValueGBP + sold + dividends - bought
	var retPct float64
	if bought > 0 {
		retPct = ret / bought * 100
	} else if currentValueGBP > 0 {
		slog.Warn("position has holdings but no buy orders found", "currentValueGBP", currentValueGBP, "orders", len(orders), "divs", len(divs))
	}

	return api.ReturnInfo{
		TotalBought:    bought,
		TotalSold:      sold,
		TotalDividends: dividends,
		Return:         ret,
		ReturnPct:      retPct,
	}
}
