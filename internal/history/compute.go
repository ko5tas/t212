package history

import "github.com/ko5tas/t212/internal/api"

// ComputeReturns aggregates order fills and dividends into a ReturnInfo.
//
// currentPrice is the live price per share (in stock currency — USD, EUR, or
// GBP after GBX normalisation). qty is shares currently held. currency is the
// position's currency ("GBP", "USD", "EUR", …).
//
// For foreign-currency stocks, an implied FX rate is derived from buy fills
// (fill.price * fill.qty / walletImpact) and used to estimate the current
// GBP value of the remaining holding.
//
// Return = estimatedCurrentValueGBP + sold + dividends − bought.
// ReturnPct = Return / bought × 100.
// NetROIPct mirrors ReturnPct (total P&L as % of investment).
func ComputeReturns(orders []api.HistoricalOrder, divs []api.DividendItem, currentPrice, qty float64, currency string) api.ReturnInfo {
	var bought, sold float64
	var buyFillValueLocal float64 // sum(fill.price * fill.quantity) for buys, in stock currency

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
			buyFillValueLocal += o.Fill.Price * o.Fill.Quantity
		case "SELL":
			sold += v
		}
	}

	var dividends float64
	for _, d := range divs {
		dividends += d.Amount
	}

	// Estimate current value of remaining shares in GBP.
	var currentValueGBP float64
	if qty > 0 && currentPrice > 0 {
		if currency == "GBP" {
			// GBP stock — currentPrice is already normalised to GBP.
			currentValueGBP = currentPrice * qty
		} else if buyFillValueLocal > 0 && bought > 0 {
			// Foreign stock — derive implied FX rate from buy history.
			// impliedRate = stock_currency per GBP (includes FX fee spread).
			impliedRate := buyFillValueLocal / bought
			currentValueGBP = (currentPrice * qty) / impliedRate
		}
	}

	ret := currentValueGBP + sold + dividends - bought
	var retPct, netROI float64
	if bought > 0 {
		retPct = ret / bought * 100
		netROI = retPct
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
