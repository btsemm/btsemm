package btsemm

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// RiskConfig defines the risk limits for the bot.
type RiskConfig struct {
	MaxPositionSize  float64       // Max absolute base position (set by preflight if MaxPositionUSDT > 0)
	MaxPositionUSDT  float64       // Max position in USDT terms; preflight converts to base
	MaxLossUSDT      float64       // Max unrealized + realized loss before kill
	MaxOpenOrders    int           // Max concurrent open orders
	KillOnDisconnect time.Duration // Kill if no data received for this long
}

// RiskManager monitors risk limits and provides a kill switch.
type RiskManager struct {
	mu       sync.RWMutex
	config   RiskConfig
	position *PositionTracker
	orders   *OrderManager
	killed   bool
	reason   string
	lastData time.Time
}

// NewRiskManager creates a risk manager with the given configuration.
func NewRiskManager(config RiskConfig, position *PositionTracker, orders *OrderManager) *RiskManager {
	return &RiskManager{
		config:   config,
		position: position,
		orders:   orders,
		lastData: time.Now(),
	}
}

// TouchData records that live market data was received.
func (rm *RiskManager) TouchData() {
	rm.mu.Lock()
	rm.lastData = time.Now()
	rm.mu.Unlock()
}

// Check evaluates all risk limits. Returns an error describing the breach, or nil.
// If a limit is breached, it automatically fires the kill switch.
func (rm *RiskManager) Check(midPrice float64) error {
	rm.mu.RLock()
	if rm.killed {
		reason := rm.reason
		rm.mu.RUnlock()
		return fmt.Errorf("btse: risk killed: %s", reason)
	}
	lastData := rm.lastData
	rm.mu.RUnlock()

	// Check data liveness
	if rm.config.KillOnDisconnect > 0 && time.Since(lastData) > rm.config.KillOnDisconnect {
		rm.Kill(fmt.Sprintf("no data for %v", time.Since(lastData).Round(time.Second)))
		return fmt.Errorf("btse: risk killed: data timeout")
	}

	snap := rm.position.Snapshot()

	// Check position size
	if rm.config.MaxPositionSize > 0 && math.Abs(snap.BaseQty) > rm.config.MaxPositionSize {
		reason := fmt.Sprintf("position %.8f exceeds max %.8f", math.Abs(snap.BaseQty), rm.config.MaxPositionSize)
		rm.Kill(reason)
		return fmt.Errorf("btse: risk killed: %s", reason)
	}

	// Check P&L (unrealized)
	if rm.config.MaxLossUSDT > 0 && midPrice > 0 && math.Abs(snap.BaseQty) > 1e-12 {
		unrealizedPnL := snap.BaseQty*midPrice - snap.QuoteSpent - snap.TotalFees
		if unrealizedPnL < -rm.config.MaxLossUSDT {
			reason := fmt.Sprintf("loss %.2f USDT exceeds max %.2f", -unrealizedPnL, rm.config.MaxLossUSDT)
			rm.Kill(reason)
			return fmt.Errorf("btse: risk killed: %s", reason)
		}
	}

	// Check open order count
	if rm.config.MaxOpenOrders > 0 {
		openCount := len(rm.orders.OpenOrders())
		if openCount > rm.config.MaxOpenOrders {
			reason := fmt.Sprintf("%d open orders exceeds max %d", openCount, rm.config.MaxOpenOrders)
			rm.Kill(reason)
			return fmt.Errorf("btse: risk killed: %s", reason)
		}
	}

	return nil
}

// AllowOrder performs a pre-trade check to determine if a new order is allowed.
func (rm *RiskManager) AllowOrder(side OrderSide, size float64) bool {
	rm.mu.RLock()
	if rm.killed {
		rm.mu.RUnlock()
		return false
	}
	rm.mu.RUnlock()

	if rm.config.MaxPositionSize <= 0 {
		return true
	}

	pos := rm.position.Position()
	var newPos float64
	switch side {
	case SideBuy:
		newPos = pos + size
	case SideSell:
		newPos = pos - size
	}

	return math.Abs(newPos) <= rm.config.MaxPositionSize
}

// Kill cancels all orders and latches the killed state.
func (rm *RiskManager) Kill(reason string) {
	rm.mu.Lock()
	if rm.killed {
		rm.mu.Unlock()
		return
	}
	rm.killed = true
	rm.reason = reason
	rm.mu.Unlock()

	rm.orders.CancelAll()
}

// IsKilled returns true if the kill switch has been triggered.
func (rm *RiskManager) IsKilled() bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.killed
}

// KillReason returns the reason the kill switch was triggered.
func (rm *RiskManager) KillReason() string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.reason
}

// SetMaxPosition updates the max position size limit.
func (rm *RiskManager) SetMaxPosition(size float64) {
	rm.mu.Lock()
	rm.config.MaxPositionSize = size
	rm.mu.Unlock()
}

// Config returns a copy of the current risk configuration. Used by the
// watchdog/state dump to surface the active limits.
func (rm *RiskManager) Config() RiskConfig {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.config
}

// Reset clears the kill switch, allowing the bot to resume.
func (rm *RiskManager) Reset() {
	rm.mu.Lock()
	rm.killed = false
	rm.reason = ""
	rm.mu.Unlock()
}
