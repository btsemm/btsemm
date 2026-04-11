package btsemm

import (
	"math"
	"testing"
)

func TestPositionTracker_BuyAndSell(t *testing.T) {
	pt := NewPositionTracker("BTC-USDT")

	// Buy 0.1 BTC at 50000
	pt.ProcessFill(WSFill{Side: "BUY", Price: 50000, Size: 0.1, Fee: 2.5})
	if got := pt.Position(); math.Abs(got-0.1) > 1e-12 {
		t.Errorf("after buy: position = %f, want 0.1", got)
	}
	if got := pt.AvgCost(); math.Abs(got-50000) > 0.01 {
		t.Errorf("after buy: avg cost = %f, want 50000", got)
	}

	// Buy 0.1 more at 51000
	pt.ProcessFill(WSFill{Side: "BUY", Price: 51000, Size: 0.1, Fee: 2.55})
	if got := pt.Position(); math.Abs(got-0.2) > 1e-12 {
		t.Errorf("after 2nd buy: position = %f, want 0.2", got)
	}
	// Avg cost = (50000*0.1 + 51000*0.1) / 0.2 = 50500
	if got := pt.AvgCost(); math.Abs(got-50500) > 0.01 {
		t.Errorf("after 2nd buy: avg cost = %f, want 50500", got)
	}

	// Sell 0.15 at 52000
	pt.ProcessFill(WSFill{Side: "SELL", Price: 52000, Size: 0.15, Fee: 3.9})

	snap := pt.Snapshot()
	if math.Abs(snap.BaseQty-0.05) > 1e-12 {
		t.Errorf("after sell: base = %f, want 0.05", snap.BaseQty)
	}
	if snap.FillCount != 3 {
		t.Errorf("fill count = %d, want 3", snap.FillCount)
	}
	if math.Abs(snap.TotalFees-8.95) > 0.01 {
		t.Errorf("total fees = %f, want 8.95", snap.TotalFees)
	}
}

func TestPositionTracker_Seed(t *testing.T) {
	pt := NewPositionTracker("BTC-USDT")
	pt.Seed(0.5, 48000)

	if got := pt.Position(); math.Abs(got-0.5) > 1e-12 {
		t.Errorf("seeded position = %f, want 0.5", got)
	}
	if got := pt.AvgCost(); math.Abs(got-48000) > 0.01 {
		t.Errorf("seeded avg cost = %f, want 48000", got)
	}
}

func TestPositionTracker_ZeroPosition(t *testing.T) {
	pt := NewPositionTracker("BTC-USDT")
	if got := pt.AvgCost(); got != 0 {
		t.Errorf("zero position avg cost = %f, want 0", got)
	}
}
