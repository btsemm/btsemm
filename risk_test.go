package btsemm

import (
	"testing"
	"time"
)

func TestRiskManager_PositionLimit(t *testing.T) {
	pt := NewPositionTracker("BTC-USDT")
	om := NewOrderManager(nil, "BTC-USDT") // nil client OK for test
	rm := NewRiskManager(RiskConfig{MaxPositionSize: 0.1}, pt, om)

	// Within limit
	pt.Seed(0.05, 50000)
	if err := rm.Check(50000); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	// Exceed limit
	pt.Seed(0.2, 50000)
	if err := rm.Check(50000); err == nil {
		t.Error("expected error for position breach")
	}
	if !rm.IsKilled() {
		t.Error("expected killed after position breach")
	}
}

func TestRiskManager_AllowOrder(t *testing.T) {
	pt := NewPositionTracker("BTC-USDT")
	om := NewOrderManager(nil, "BTC-USDT")
	rm := NewRiskManager(RiskConfig{MaxPositionSize: 0.1}, pt, om)

	pt.Seed(0.08, 50000)

	// Buy 0.01 would bring to 0.09 — allowed
	if !rm.AllowOrder(SideBuy, 0.01) {
		t.Error("expected buy 0.01 to be allowed")
	}

	// Buy 0.05 would bring to 0.13 — rejected
	if rm.AllowOrder(SideBuy, 0.05) {
		t.Error("expected buy 0.05 to be rejected")
	}

	// Sell 0.05 would bring to 0.03 — allowed
	if !rm.AllowOrder(SideSell, 0.05) {
		t.Error("expected sell 0.05 to be allowed")
	}
}

func TestRiskManager_KillAndReset(t *testing.T) {
	pt := NewPositionTracker("BTC-USDT")
	om := NewOrderManager(nil, "BTC-USDT")
	rm := NewRiskManager(RiskConfig{}, pt, om)

	rm.Kill("test kill")
	if !rm.IsKilled() {
		t.Error("expected killed")
	}
	if rm.KillReason() != "test kill" {
		t.Errorf("reason = %q, want %q", rm.KillReason(), "test kill")
	}

	// Check should return error when killed
	if err := rm.Check(50000); err == nil {
		t.Error("expected error when killed")
	}

	// AllowOrder should return false when killed
	if rm.AllowOrder(SideBuy, 0.01) {
		t.Error("expected order rejected when killed")
	}

	rm.Reset()
	if rm.IsKilled() {
		t.Error("expected not killed after reset")
	}
}

func TestRiskManager_DataTimeout(t *testing.T) {
	pt := NewPositionTracker("BTC-USDT")
	om := NewOrderManager(nil, "BTC-USDT")
	rm := NewRiskManager(RiskConfig{KillOnDisconnect: 50 * time.Millisecond}, pt, om)

	// Fresh data — should pass
	rm.TouchData()
	if err := rm.Check(50000); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	// Wait for timeout
	time.Sleep(100 * time.Millisecond)
	if err := rm.Check(50000); err == nil {
		t.Error("expected error for data timeout")
	}
	if !rm.IsKilled() {
		t.Error("expected killed after data timeout")
	}
}
