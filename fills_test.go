package btsemm

import (
	"encoding/json"
	"testing"
)

func TestParseOrderNotification(t *testing.T) {
	raw := `{
		"symbol": "BTC-USDT",
		"orderID": "ord1",
		"clOrderID": "mm-5678",
		"side": "MODE_SELL",
		"type": 76,
		"price": 51000.0,
		"size": 0.002,
		"fillSize": 0.001,
		"status": 5,
		"timestamp": 1700000000000,
		"avgFillPrice": 51000.0,
		"time_in_force": "GTC",
		"remainingSize": 0.001,
		"originalSize": 0.002,
		"maker": true
	}`

	msg := WSMessage{Topic: "notificationApiV2", Data: json.RawMessage(raw)}
	notif, err := msg.ParseOrderNotification()
	if err != nil {
		t.Fatalf("ParseOrderNotification: %v", err)
	}
	if notif.OrderID != "ord1" {
		t.Errorf("OrderID = %q, want %q", notif.OrderID, "ord1")
	}
	if notif.Status != OrderStatusPartiallyFilled {
		t.Errorf("Status = %v, want PARTIALLY_FILLED", notif.Status)
	}
	if notif.RemainingSize != 0.001 {
		t.Errorf("RemainingSize = %f, want 0.001", notif.RemainingSize)
	}
	if notif.NormalizedSide() != "SELL" {
		t.Errorf("NormalizedSide = %q, want SELL", notif.NormalizedSide())
	}
	if notif.FillSize != 0.001 {
		t.Errorf("FillSize = %f, want 0.001", notif.FillSize)
	}
}

func TestFillHandler_Notification(t *testing.T) {
	fh := NewFillHandler("", "")

	var gotNotif WSOrderNotification
	var gotFill WSFill

	fh.OnNotification(func(n WSOrderNotification) { gotNotif = n })
	fh.OnFill(func(f WSFill) { gotFill = f })

	// Notification with a fill
	notifData := `{
		"orderID": "o1",
		"clOrderID": "mm-1",
		"symbol": "BTC-USDT",
		"side": "MODE_BUY",
		"status": 4,
		"price": 70000,
		"fillSize": 0.001,
		"avgFillPrice": 70000,
		"timestamp": 1700000000000
	}`
	fh.Handle(WSMessage{Topic: "notificationApiV2", Data: json.RawMessage(notifData)})

	if gotNotif.Status != OrderStatusFullyTransacted {
		t.Errorf("notification status = %v, want FULLY_TRANSACTED", gotNotif.Status)
	}
	if gotNotif.Side != "BUY" {
		t.Errorf("notification side = %q, want BUY", gotNotif.Side)
	}
	if gotFill.Size != 0.001 {
		t.Errorf("fill size = %f, want 0.001", gotFill.Size)
	}
	if gotFill.Price != 70000 {
		t.Errorf("fill price = %f, want 70000", gotFill.Price)
	}
}

func TestFillHandler_NoFill(t *testing.T) {
	fh := NewFillHandler("", "")

	var gotFill bool
	fh.OnFill(func(f WSFill) { gotFill = true })

	// Notification without a fill (e.g. order inserted)
	notifData := `{
		"orderID": "o1",
		"clOrderID": "mm-1",
		"side": "MODE_BUY",
		"status": 2,
		"fillSize": 0,
		"timestamp": 1700000000000
	}`
	fh.Handle(WSMessage{Topic: "notificationApiV2", Data: json.RawMessage(notifData)})

	if gotFill {
		t.Error("fill callback should not fire when fillSize=0")
	}
}

func TestFillHandler_IgnoresOtherTopics(t *testing.T) {
	fh := NewFillHandler("", "")

	var called bool
	fh.OnNotification(func(n WSOrderNotification) { called = true })

	fh.Handle(WSMessage{Topic: "tradeHistoryApi", Data: json.RawMessage(`{}`)})
	if called {
		t.Error("should not process non-notificationApiV2 topics")
	}
}
