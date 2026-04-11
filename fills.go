package btsemm

import (
	"encoding/json"
	"log"
	"strings"
)

// WSOrderNotification represents an order status update from the notificationApiV2 topic.
type WSOrderNotification struct {
	OrderID       string      `json:"orderID"`
	ClOrderID     string      `json:"clOrderID"`
	Symbol        string      `json:"symbol"`
	Side          string      `json:"side"`
	Price         float64     `json:"price"`
	Size          float64     `json:"size"`
	FillSize      float64     `json:"fillSize"`
	Status        OrderStatus `json:"status"`
	Timestamp     int64       `json:"timestamp"`
	AvgFillPrice  float64     `json:"avgFillPrice"`
	RemainingSize float64     `json:"remainingSize"`
	OriginalSize  float64     `json:"originalSize"`
	TimeInForce   string      `json:"time_in_force"`
	Maker         bool        `json:"maker"`
	PostOnly      bool        `json:"postOnly"`
}

// NormalizedSide returns "BUY" or "SELL" from BTSE's "MODE_BUY"/"MODE_SELL" format.
func (n *WSOrderNotification) NormalizedSide() string {
	s := strings.TrimPrefix(n.Side, "MODE_")
	return s
}

// WSFill represents a fill derived from a notification with fillSize > 0.
type WSFill struct {
	OrderID     string  `json:"orderID"`
	ClOrderID   string  `json:"clOrderID"`
	Symbol      string  `json:"symbol"`
	Side        string  `json:"side"`
	Price       float64 `json:"price"`
	Size        float64 `json:"size"`
	Fee         float64 `json:"fee"`
	FeeCurrency string  `json:"feeCurrency"`
	Timestamp   int64   `json:"timestamp"`
}

// ParseOrderNotification parses the data from a notificationApiV2 message.
// BTSE sends a single notification object, not an array.
func (m WSMessage) ParseOrderNotification() (*WSOrderNotification, error) {
	var out WSOrderNotification
	return &out, json.Unmarshal(m.Data, &out)
}

// FillHandler dispatches fill and order notification events to registered callbacks.
type FillHandler struct {
	symbol                string
	orderPrefix           string
	fillCallbacks         []func(WSFill)
	notificationCallbacks []func(WSOrderNotification)
}

// NewFillHandler creates a new FillHandler that only processes events
// matching the given symbol and order prefix.
func NewFillHandler(symbol, orderPrefix string) *FillHandler {
	return &FillHandler{symbol: symbol, orderPrefix: orderPrefix}
}

// OnFill registers a callback that fires for each fill.
func (fh *FillHandler) OnFill(cb func(WSFill)) {
	fh.fillCallbacks = append(fh.fillCallbacks, cb)
}

// OnNotification registers a callback that fires for each order notification.
func (fh *FillHandler) OnNotification(cb func(WSOrderNotification)) {
	fh.notificationCallbacks = append(fh.notificationCallbacks, cb)
}

// Handle routes a WebSocket message to registered callbacks.
func (fh *FillHandler) Handle(msg WSMessage) {
	if msg.Topic != "notificationApiV2" {
		return
	}

	notif, err := msg.ParseOrderNotification()
	if err != nil {
		log.Printf("fills: parse error: %v", err)
		return
	}

	// Normalize side from MODE_BUY/MODE_SELL to BUY/SELL
	notif.Side = notif.NormalizedSide()

	// Ignore notifications for other symbols
	if fh.symbol != "" && notif.Symbol != "" && notif.Symbol != fh.symbol {
		return
	}

	// Ignore notifications for orders not placed by this bot instance
	if fh.orderPrefix != "" && notif.ClOrderID != "" && !strings.HasPrefix(notif.ClOrderID, fh.orderPrefix) {
		return
	}

	// Fire notification callbacks
	for _, cb := range fh.notificationCallbacks {
		cb(*notif)
	}

	// If there was a fill, synthesize a WSFill and fire fill callbacks
	if notif.FillSize > 0 {
		fill := WSFill{
			OrderID:   notif.OrderID,
			ClOrderID: notif.ClOrderID,
			Symbol:    notif.Symbol,
			Side:      notif.Side,
			Price:     notif.AvgFillPrice,
			Size:      notif.FillSize,
			Timestamp: notif.Timestamp,
		}
		if fill.Price == 0 {
			fill.Price = notif.Price
		}
		for _, cb := range fh.fillCallbacks {
			cb(fill)
		}
	}
}
