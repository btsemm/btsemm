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
	FeeAmount     float64     `json:"feeAmount"`
	FeeCurrency   string      `json:"feeCurrency"`
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

	// Verbose enables per-notification debug logging in Handle. Set by the
	// engine from EngineConfig.Verbose.
	Verbose bool

	// DefaultFeeRate is the fallback fee rate (as a fraction, e.g. 0.002 for
	// 0.2%) used to estimate fees when the exchange notification doesn't
	// include them. Set to 0 to disable estimation.
	DefaultFeeRate float64

	// seenFills tracks the cumulative fillSize already dispatched per
	// clOrderID. BTSE's notificationApiV2 reports fillSize as a running
	// total, not an incremental amount. Without deduplication, a partial
	// fill followed by a full-fill notification double-dispatches the
	// entire order size.
	seenFills map[string]float64
}

// NewFillHandler creates a new FillHandler that only processes events
// matching the given symbol and order prefix.
func NewFillHandler(symbol, orderPrefix string) *FillHandler {
	return &FillHandler{
		symbol:      symbol,
		orderPrefix: orderPrefix,
		seenFills:   make(map[string]float64),
	}
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

	if fh.Verbose {
		log.Printf("fills: raw notif JSON: %s", string(msg.Data))
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

	if fh.Verbose {
		log.Printf("fills: notif side=%s status=%d size=%.6f fillSize=%.6f remaining=%.6f price=%.6f avgFillPrice=%.6f fee=%.8f feeCcy=%s maker=%v clOID=%s",
			notif.Side, notif.Status, notif.Size, notif.FillSize, notif.RemainingSize,
			notif.Price, notif.AvgFillPrice, notif.FeeAmount, notif.FeeCurrency, notif.Maker, notif.ClOrderID)
	}

	// Fire notification callbacks
	for _, cb := range fh.notificationCallbacks {
		cb(*notif)
	}

	// If there was a fill, compute the INCREMENTAL fill size. BTSE's
	// notificationApiV2 reports fillSize as a running cumulative total, not
	// the amount filled in this specific event. Without this delta logic, a
	// partial fill (status=5) followed by a full-fill (status=4) would
	// dispatch the entire order size twice — double-counting the position
	// and matching the wrong grid.
	if notif.FillSize > 0 {
		prev := fh.seenFills[notif.ClOrderID]
		incremental := notif.FillSize - prev
		if incremental < 1e-12 {
			// Already dispatched this fill amount — skip. This happens when
			// BTSE sends a duplicate notification or a status update with the
			// same cumulative fillSize.
			if fh.Verbose {
				log.Printf("fills: skipping duplicate fill for %s (cumulative=%.6f, already dispatched=%.6f)",
					notif.ClOrderID, notif.FillSize, prev)
			}
		} else {
			fh.seenFills[notif.ClOrderID] = notif.FillSize

			fill := WSFill{
				OrderID:     notif.OrderID,
				ClOrderID:   notif.ClOrderID,
				Symbol:      notif.Symbol,
				Side:        notif.Side,
				Price:       notif.AvgFillPrice,
				Size:        incremental, // delta, not cumulative
				Fee:         notif.FeeAmount,
				FeeCurrency: notif.FeeCurrency,
				Timestamp:   notif.Timestamp,
			}
			if fill.Price == 0 {
				fill.Price = notif.Price
			}
			// If the exchange didn't report a fee, estimate from the default rate
			// using the incremental size.
			if fill.Fee == 0 && fh.DefaultFeeRate > 0 {
				fill.Fee = incremental * fill.Price * fh.DefaultFeeRate
			}
			if fh.Verbose {
				log.Printf("fills: dispatching fill %s %.6f @ %.6f (incremental, cumulative=%.6f) to %d callbacks",
					fill.Side, fill.Size, fill.Price, notif.FillSize, len(fh.fillCallbacks))
			}
			for _, cb := range fh.fillCallbacks {
				cb(fill)
			}
		}
	}
}
