package btsemm

import "net/url"

// GetMarketSummary returns market summary for all or a specific symbol.
func (c *Client) GetMarketSummary(symbol string) ([]MarketSummary, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	var out []MarketSummary
	return out, c.doPublic("/market_summary", params, &out)
}

// GetOHLCV returns candlestick data.
// resolution: e.g. "1", "5", "15", "30", "60", "360", "1440" (minutes)
func (c *Client) GetOHLCV(symbol string, resolution string, start, end int64) ([]OHLCV, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	if resolution != "" {
		params.Set("resolution", resolution)
	}
	if start > 0 {
		params.Set("start", formatInt64(start))
	}
	if end > 0 {
		params.Set("end", formatInt64(end))
	}
	var out []OHLCV
	return out, c.doPublic("/ohlcv", params, &out)
}

// GetPrice returns current prices for all or a specific symbol.
func (c *Client) GetPrice(symbol string) ([]Price, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	var out []Price
	return out, c.doPublic("/price", params, &out)
}

// GetOrderbook returns the L2 orderbook with optional grouping.
func (c *Client) GetOrderbook(symbol string, group int) (*Orderbook, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	if group > 0 {
		params.Set("group", formatInt64(int64(group)))
	}
	var out Orderbook
	return &out, c.doPublic("/orderbook/L2", params, &out)
}

// GetTrades returns recent public trades for a symbol.
func (c *Client) GetTrades(symbol string, count int, beforeSerialID, afterSerialID int64) ([]Trade, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	if count > 0 {
		params.Set("count", formatInt64(int64(count)))
	}
	if beforeSerialID > 0 {
		params.Set("beforeSerialId", formatInt64(beforeSerialID))
	}
	if afterSerialID > 0 {
		params.Set("afterSerialId", formatInt64(afterSerialID))
	}
	var out []Trade
	return out, c.doPublic("/trades", params, &out)
}

// GetServerTime returns the current server time.
func (c *Client) GetServerTime() (*ServerTime, error) {
	var out ServerTime
	return &out, c.doPublic("/time", nil, &out)
}
