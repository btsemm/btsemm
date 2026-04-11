package btsemm

import (
	"math"
	"sync"
	"time"
)

// PositionSnapshot is a point-in-time view of the position.
type PositionSnapshot struct {
	BaseQty    float64   // Net base currency held (positive = long, negative = short)
	QuoteSpent float64   // Net quote currency spent (buys add, sells subtract)
	AvgCost    float64   // Average entry cost in quote per base
	TotalFees  float64   // Total fees paid (in fee currency)
	FillCount  int       // Number of fills processed
	UpdatedAt  time.Time // Time of last update
}

// PositionTracker tracks net position for a single symbol in real-time.
type PositionTracker struct {
	mu         sync.RWMutex
	symbol     string
	baseQty    float64
	quoteSpent float64
	totalFees  float64
	fillCount  int
	updatedAt  time.Time
}

// NewPositionTracker creates a tracker for the given symbol.
func NewPositionTracker(symbol string) *PositionTracker {
	return &PositionTracker{
		symbol:    symbol,
		updatedAt: time.Now(),
	}
}

// Seed sets the initial position from an exchange balance query.
func (pt *PositionTracker) Seed(baseQty, avgCost float64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.baseQty = baseQty
	pt.quoteSpent = baseQty * avgCost
	pt.updatedAt = time.Now()
}

// ProcessFill updates the position based on a fill event.
func (pt *PositionTracker) ProcessFill(fill WSFill) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	switch OrderSide(fill.Side) {
	case SideBuy:
		pt.baseQty += fill.Size
		pt.quoteSpent += fill.Size * fill.Price
	case SideSell:
		pt.baseQty -= fill.Size
		pt.quoteSpent -= fill.Size * fill.Price
	}

	pt.totalFees += fill.Fee
	pt.fillCount++
	pt.updatedAt = time.Now()
}

// Position returns the current net base quantity.
func (pt *PositionTracker) Position() float64 {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return pt.baseQty
}

// AvgCost returns the average cost per unit of the position.
func (pt *PositionTracker) AvgCost() float64 {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	if math.Abs(pt.baseQty) < 1e-12 {
		return 0
	}
	return pt.quoteSpent / pt.baseQty
}

// Snapshot returns a point-in-time copy of the position state.
func (pt *PositionTracker) Snapshot() PositionSnapshot {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	avg := 0.0
	if math.Abs(pt.baseQty) > 1e-12 {
		avg = pt.quoteSpent / pt.baseQty
	}
	return PositionSnapshot{
		BaseQty:    pt.baseQty,
		QuoteSpent: pt.quoteSpent,
		AvgCost:    avg,
		TotalFees:  pt.totalFees,
		FillCount:  pt.fillCount,
		UpdatedAt:  pt.updatedAt,
	}
}
