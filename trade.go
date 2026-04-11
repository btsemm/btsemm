package btsemm

import "net/url"

// CreateOrder places a new order.
func (c *Client) CreateOrder(req *CreateOrderRequest) ([]Order, error) {
	var out []Order
	return out, c.doAuth("POST", "/order", nil, req, &out)
}

// GetOrder queries an order by orderID or clOrderID.
func (c *Client) GetOrder(symbol, orderID, clOrderID string) (*Order, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	if orderID != "" {
		params.Set("orderID", orderID)
	}
	if clOrderID != "" {
		params.Set("clOrderID", clOrderID)
	}
	var out Order
	return &out, c.doAuth("GET", "/order", params, nil, &out)
}

// AmendOrder modifies an existing order.
func (c *Client) AmendOrder(req *AmendOrderRequest) (*Order, error) {
	var out Order
	return &out, c.doAuth("PUT", "/order", nil, req, &out)
}

// CancelOrder cancels an existing order.
func (c *Client) CancelOrder(req *CancelOrderRequest) ([]Order, error) {
	params := url.Values{}
	params.Set("symbol", req.Symbol)
	if req.OrderID != "" {
		params.Set("orderID", req.OrderID)
	}
	if req.ClOrderID != "" {
		params.Set("clOrderID", req.ClOrderID)
	}
	var out []Order
	return out, c.doAuth("DELETE", "/order", params, nil, &out)
}

// CancelAllAfter activates the dead man's switch — cancels all orders
// if no new request is received within timeout milliseconds.
// Set timeout to -1 to deactivate.
func (c *Client) CancelAllAfter(timeout int64) error {
	req := &CancelAllAfterRequest{Timeout: timeout}
	return c.doAuth("POST", "/order/cancelAllAfter", nil, req, nil)
}

// GetOpenOrders returns all open orders, optionally filtered by symbol.
func (c *Client) GetOpenOrders(symbol string) ([]Order, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	var out []Order
	return out, c.doAuth("GET", "/user/open_orders", params, nil, &out)
}

// GetTradeHistory returns the user's trade history.
func (c *Client) GetTradeHistory(symbol string, start, end int64, count int) ([]UserTrade, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	if start > 0 {
		params.Set("start", formatInt64(start))
	}
	if end > 0 {
		params.Set("end", formatInt64(end))
	}
	if count > 0 {
		params.Set("count", formatInt64(int64(count)))
	}
	var out []UserTrade
	return out, c.doAuth("GET", "/user/trade_history", params, nil, &out)
}

// GetFees returns the account's trading fee rates.
func (c *Client) GetFees(symbol string) ([]AccountFees, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	var out []AccountFees
	return out, c.doAuth("GET", "/user/fees", params, nil, &out)
}
