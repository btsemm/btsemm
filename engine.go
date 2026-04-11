package btsemm

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"time"
)

// Strategy is the interface that market making strategies must implement.
type Strategy interface {
	// Init is called once at startup after preflight. The strategy can validate
	// its own requirements (balances, spread vs fees, etc). Return an error to
	// abort startup. Returning nil from Init or not needing extra checks is fine.
	Init(ctx StrategyContext) error

	// OnTick receives a market snapshot and returns the desired resting orders.
	// The engine diffs desired vs actual and issues minimum API calls.
	OnTick(state MarketState) []DesiredOrder

	// OnFill is called immediately when a fill is received.
	OnFill(fill WSFill)
}

// StrategyContext is passed to Strategy.Init with everything the strategy
// needs to validate its requirements.
type StrategyContext struct {
	Client    *Client
	Symbol    string
	Market    MarketInfo
	LastPrice float64
}

// MarketState is the snapshot passed to the strategy each tick.
type MarketState struct {
	Mid        float64
	Bid        float64
	Ask        float64
	Spread     float64
	Position   PositionSnapshot
	OpenOrders []*TrackedOrder
	Timestamp  time.Time
}

// DesiredOrder represents an order the strategy wants on the book.
type DesiredOrder struct {
	Side     OrderSide
	Price    float64
	Size     float64
	PostOnly bool
}

// EngineConfig configures the trading engine.
type EngineConfig struct {
	Symbol            string
	TickInterval      time.Duration
	ReconcileInterval time.Duration
	DeadManTimeout    int64   // milliseconds for CancelAllAfter; 0 to disable
	MinPriceIncrement float64 // overridden by preflight if 0
	MinSizeIncrement  float64 // overridden by preflight if 0
	AmendBPS          float64 // min price move in BPS of order price to trigger replace (default 1)
	Risk              RiskConfig
	MaxConsecErrors   int // kill after this many consecutive order errors; 0 = 10
}

// MarketInfo holds validated market parameters from the exchange.
type MarketInfo struct {
	MinPriceIncrement float64
	MinSizeIncrement  float64
	MinOrderSize      float64
	MaxOrderSize      float64
}

// Engine is the main market making engine that ties everything together.
type Engine struct {
	config   EngineConfig
	client   *Client
	strategy Strategy

	midPrice *MidPrice
	position *PositionTracker
	orders   *OrderManager
	risk     *RiskManager
	fills    *FillHandler
	market   MarketInfo

	privateWS        *ReconnectingWSConn
	consecErrors     int
	lastRiskRejected int
	lastPrice        float64
}

// NewEngine creates a new engine with the given configuration and strategy.
func NewEngine(client *Client, config EngineConfig, strategy Strategy) *Engine {
	position := NewPositionTracker(config.Symbol)
	orders := NewOrderManager(client, config.Symbol)
	risk := NewRiskManager(config.Risk, position, orders)
	fills := NewFillHandler(config.Symbol, orders.Prefix())

	// Wire up fill processing
	fills.OnFill(position.ProcessFill)
	fills.OnFill(strategy.OnFill)
	fills.OnNotification(orders.ProcessNotification)

	if config.MaxConsecErrors == 0 {
		config.MaxConsecErrors = 10
	}

	return &Engine{
		config:   config,
		client:   client,
		strategy: strategy,
		position: position,
		orders:   orders,
		risk:     risk,
		fills:    fills,
	}
}

// Preflight fetches market info and validates balances before starting.
func (e *Engine) Preflight() error {
	symbol := e.config.Symbol

	// Fetch market summary for tick sizes
	summaries, err := e.client.GetMarketSummary(symbol)
	if err != nil {
		return fmt.Errorf("preflight: fetch market summary: %w", err)
	}
	if len(summaries) == 0 {
		return fmt.Errorf("preflight: no market summary for %s", symbol)
	}
	ms := summaries[0]

	e.market = MarketInfo{
		MinPriceIncrement: ms.MinPriceIncrement,
		MinSizeIncrement:  ms.MinSizeIncrement,
		MinOrderSize:      ms.MinOrderSize,
		MaxOrderSize:      ms.MaxOrderSize,
	}

	// Override config with exchange values
	if e.config.MinPriceIncrement == 0 {
		e.config.MinPriceIncrement = ms.MinPriceIncrement
	}
	if e.config.MinSizeIncrement == 0 {
		e.config.MinSizeIncrement = ms.MinSizeIncrement
	}

	log.Printf("preflight: %s price_tick=%.10g size_tick=%.10g min_size=%.10g max_size=%.10g",
		symbol, ms.MinPriceIncrement, ms.MinSizeIncrement, ms.MinOrderSize, ms.MaxOrderSize)

	// Parse base/quote currencies from symbol (e.g. "BTC-USDT")
	parts := strings.SplitN(symbol, "-", 2)
	if len(parts) != 2 {
		return fmt.Errorf("preflight: cannot parse symbol %q (expected BASE-QUOTE)", symbol)
	}
	base, quote := parts[0], parts[1]

	// Check balances
	baseBal, err := e.client.GetCurrencyBalance(base)
	if err != nil {
		return fmt.Errorf("preflight: fetch %s balance: %w", base, err)
	}
	quoteBal, err := e.client.GetCurrencyBalance(quote)
	if err != nil {
		return fmt.Errorf("preflight: fetch %s balance: %w", quote, err)
	}

	baseAvail := 0.0
	if baseBal != nil {
		baseAvail = baseBal.Available
	}
	quoteAvail := 0.0
	if quoteBal != nil {
		quoteAvail = quoteBal.Available
	}

	log.Printf("preflight: balances %s=%.8f %s=%.4f", base, baseAvail, quote, quoteAvail)

	// Estimate required amounts using last price
	prices, err := e.client.GetPrice(symbol)
	if err != nil {
		return fmt.Errorf("preflight: fetch price: %w", err)
	}
	if len(prices) == 0 {
		return fmt.Errorf("preflight: no price for %s", symbol)
	}
	lastPrice := prices[0].LastPrice
	if lastPrice == 0 {
		lastPrice = prices[0].IndexPrice
	}

	// Minimum base needed to place a sell at min order size
	minBase := ms.MinOrderSize
	// Minimum quote needed to place a buy at min order size
	minQuote := ms.MinOrderSize * lastPrice

	log.Printf("preflight: last_price=%.2f min_base_needed=%.8f min_quote_needed=%.4f",
		lastPrice, minBase, minQuote)

	// Convert USDT-denominated risk limits to base currency
	if e.config.Risk.MaxPositionUSDT > 0 && lastPrice > 0 {
		e.config.Risk.MaxPositionSize = e.config.Risk.MaxPositionUSDT / lastPrice
		log.Printf("preflight: max_position=%.4f %s (%.2f USDT @ %.4f)",
			e.config.Risk.MaxPositionSize, base, e.config.Risk.MaxPositionUSDT, lastPrice)
		// Update the risk manager with the new limit
		e.risk.SetMaxPosition(e.config.Risk.MaxPositionSize)
	}

	if baseAvail < minBase {
		log.Printf("preflight: WARNING: %s balance %.8f < min sell size %.8f — sell orders will fail", base, baseAvail, minBase)
	}
	if quoteAvail < minQuote {
		log.Printf("preflight: WARNING: %s balance %.4f < min buy cost %.4f — buy orders will fail", quote, quoteAvail, minQuote)
	}
	if baseAvail < minBase && quoteAvail < minQuote {
		return fmt.Errorf("preflight: insufficient balance for both sides — need at least %.8f %s or %.4f %s",
			minBase, base, minQuote, quote)
	}

	e.lastPrice = lastPrice
	return nil
}

// RoundPrice rounds a price to the exchange's min price increment.
func (e *Engine) RoundPrice(price float64, side OrderSide) float64 {
	tick := e.market.MinPriceIncrement
	if tick == 0 {
		tick = e.config.MinPriceIncrement
	}
	if tick == 0 {
		return price
	}
	// Use integer math to avoid floating point drift
	inv := math.Round(1.0 / tick)
	if side == SideBuy {
		return math.Floor(price*inv) / inv
	}
	return math.Ceil(price*inv) / inv
}

// RoundSize rounds a size down to the exchange's min size increment.
func (e *Engine) RoundSize(size float64) float64 {
	tick := e.market.MinSizeIncrement
	if tick == 0 {
		tick = e.config.MinSizeIncrement
	}
	if tick == 0 {
		return size
	}
	inv := math.Round(1.0 / tick)
	return math.Floor(size*inv) / inv
}

// Run starts the engine and blocks until the context is cancelled.
func (e *Engine) Run(ctx context.Context) error {
	// Preflight checks
	if err := e.Preflight(); err != nil {
		return err
	}

	// Let the strategy validate its own requirements
	if err := e.strategy.Init(StrategyContext{
		Client:    e.client,
		Symbol:    e.config.Symbol,
		Market:    e.market,
		LastPrice: e.lastPrice,
	}); err != nil {
		return fmt.Errorf("strategy init: %w", err)
	}

	// Start mid-price feed
	mp, err := e.client.WatchMidPrice(e.config.Symbol)
	if err != nil {
		return fmt.Errorf("btse: engine start midprice: %w", err)
	}
	e.midPrice = mp

	// Wait for first price
	log.Printf("engine: waiting for mid-price...")
	select {
	case <-ctx.Done():
		mp.Stop()
		return ctx.Err()
	case <-mp.Ready():
		log.Printf("engine: mid-price ready: %.2f", mp.Price())
	}

	// Connect private WS for fills and notifications
	ws, err := e.client.ConnectPrivateWSReconnect(
		[]ReconnectOption{WithOnReconnect(func() {
			log.Printf("engine: private WS reconnected, reconciling...")
			if err := e.orders.Reconcile(); err != nil {
				log.Printf("engine: reconcile after reconnect: %v", err)
			}
		})},
		WithWSHandler(e.fills.Handle),
	)
	if err != nil {
		mp.Stop()
		return fmt.Errorf("btse: engine connect private ws: %w", err)
	}
	e.privateWS = ws

	// Subscribe to fill and notification topics
	if err := ws.Subscribe("notificationApiV2"); err != nil {
		mp.Stop()
		ws.Close()
		return fmt.Errorf("btse: engine subscribe: %w", err)
	}

	amendBPS := e.config.AmendBPS
	if amendBPS == 0 {
		amendBPS = 1
	}
	log.Printf("engine: started for %s (tick=%v, reconcile=%v, prefix=%s)",
		e.config.Symbol, e.config.TickInterval, e.config.ReconcileInterval, e.orders.Prefix())
	log.Printf("engine: amend_bps=%.1f (at current price %.6f = threshold %.6f)",
		amendBPS, e.lastPrice, e.lastPrice*(amendBPS/10000.0))

	// Initial reconcile
	if err := e.orders.Reconcile(); err != nil {
		log.Printf("engine: initial reconcile: %v", err)
	}

	// Main loop
	tickInterval := e.config.TickInterval
	if tickInterval == 0 {
		tickInterval = time.Second
	}
	reconcileInterval := e.config.ReconcileInterval
	if reconcileInterval == 0 {
		reconcileInterval = 30 * time.Second
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	reconcileTicker := time.NewTicker(reconcileInterval)
	defer reconcileTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("engine: shutting down...")
			e.Stop()
			return nil

		case <-ticker.C:
			e.tick()
			if e.risk.IsKilled() {
				log.Printf("engine: risk killed, stopping: %s", e.risk.KillReason())
				e.Stop()
				return fmt.Errorf("engine killed: %s", e.risk.KillReason())
			}

		case <-reconcileTicker.C:
			if err := e.orders.Reconcile(); err != nil {
				log.Printf("engine: periodic reconcile: %v", err)
			}
			e.orders.Cleanup(5 * time.Minute)
		}
	}
}

func (e *Engine) tick() {
	// Touch risk data liveness
	mid, bid, ask, spread := e.midPrice.Snapshot()
	if mid == 0 {
		return // no price yet
	}
	e.risk.TouchData()

	// Check risk
	if err := e.risk.Check(mid); err != nil {
		log.Printf("engine: risk check failed: %v", err)
		return
	}

	// Refresh dead man's switch
	if e.config.DeadManTimeout > 0 {
		if err := e.client.CancelAllAfter(e.config.DeadManTimeout); err != nil {
			log.Printf("engine: dead man refresh: %v", err)
		}
	}

	// Build market state
	state := MarketState{
		Mid:        mid,
		Bid:        bid,
		Ask:        ask,
		Spread:     spread,
		Position:   e.position.Snapshot(),
		OpenOrders: e.orders.OpenOrders(),
		Timestamp:  time.Now(),
	}

	// Get desired orders from strategy
	desired := e.strategy.OnTick(state)

	// Round prices and sizes before reconciliation
	for i := range desired {
		desired[i].Price = e.RoundPrice(desired[i].Price, desired[i].Side)
		desired[i].Size = e.RoundSize(desired[i].Size)
	}

	// Filter out orders below min size
	var valid []DesiredOrder
	for _, d := range desired {
		if e.market.MinOrderSize > 0 && d.Size < e.market.MinOrderSize {
			log.Printf("engine: skipping %s order: size %.8f < min %.8f", d.Side, d.Size, e.market.MinOrderSize)
			continue
		}
		valid = append(valid, d)
	}

	// Reconcile desired vs actual
	e.reconcileOrders(valid, state.OpenOrders, bid, ask)
}

// reconcileOrders diffs desired orders against open orders and issues
// minimum API calls (place/amend/cancel) to reach the desired state.
func (e *Engine) reconcileOrders(desired []DesiredOrder, open []*TrackedOrder, bid, ask float64) {
	// Amend threshold: don't cancel+replace for tiny price moves.
	// Expressed in BPS of the order's own price — strategy-agnostic.
	amendBPS := e.config.AmendBPS
	if amendBPS == 0 {
		amendBPS = 1 // default 1 BPS = 0.01%
	}

	// Try to match each desired order to an existing open order
	matched := make(map[string]bool) // clOrderIDs that got matched
	hadError := false
	riskRejected := 0

	for _, d := range desired {
		// Pre-trade risk check
		if !e.risk.AllowOrder(d.Side, d.Size) {
			riskRejected++
			continue
		}

		// Per-order amend threshold: BPS of the desired price, floored to price tick
		amendThreshold := d.Price * (amendBPS / 10000.0)
		if amendThreshold < e.config.MinPriceIncrement {
			amendThreshold = e.config.MinPriceIncrement
		}

		best := e.findBestMatch(d, open, matched, amendThreshold)
		if best != nil {
			matched[best.ClOrderID] = true
			priceDiff := math.Abs(best.Price - d.Price)
			sizeDiff := math.Abs(best.Size - d.Size)
			// Only replace if price or size actually differ (within price tick = identical)
			if priceDiff > e.config.MinPriceIncrement || sizeDiff > 1e-12 {
				log.Printf("engine: amending %s %.6f→%.6f (diff=%.6f threshold=%.6f)",
					d.Side, best.Price, d.Price, priceDiff, amendThreshold)
				if _, err := e.orders.Amend(best.ClOrderID, d.Price, d.Size); err != nil {
					log.Printf("engine: amend failed: %v", err)
					hadError = true
				}
			}
			// Otherwise, order is close enough — no API call needed
		} else {
			// Skip post-only orders that would cross the spread (only for new placements)
			if d.PostOnly && bid > 0 && ask > 0 {
				if d.Side == SideBuy && d.Price >= ask {
					continue
				}
				if d.Side == SideSell && d.Price <= bid {
					continue
				}
			}
			// Place new order
			log.Printf("engine: placing %s %.6f @ %.6f", d.Side, d.Size, d.Price)
			if _, err := e.orders.Place(d.Side, d.Price, d.Size, d.PostOnly); err != nil {
				// Post-only rejections are expected near the spread — don't count as errors
				if strings.Contains(err.Error(), "post_rejected") {
					log.Printf("engine: post-only rejected %s @ %.6f (near spread, skipping)", d.Side, d.Price)
				} else {
					log.Printf("engine: place failed: %v", err)
					hadError = true
				}
			}
		}
	}

	// Cancel unmatched open orders
	for _, o := range open {
		if !matched[o.ClOrderID] {
			log.Printf("engine: cancelling unmatched %s %s @ %.6f", o.Side, o.ClOrderID, o.Price)
			if err := e.orders.Cancel(o.ClOrderID); err != nil {
				log.Printf("engine: cancel failed: %v", err)
			}
		}
	}

	if riskRejected > 0 && riskRejected != e.lastRiskRejected {
		log.Printf("engine: risk rejected %d orders (position limit)", riskRejected)
	}
	e.lastRiskRejected = riskRejected

	// Track consecutive errors
	if hadError {
		e.consecErrors++
		if e.consecErrors >= e.config.MaxConsecErrors {
			reason := fmt.Sprintf("%d consecutive order errors", e.consecErrors)
			log.Printf("engine: killing due to %s", reason)
			e.risk.Kill(reason)
		}
	} else {
		e.consecErrors = 0
	}
}

// findBestMatch finds the best existing order that matches a desired order.
// Only matches if the existing order is within the amend threshold — prevents
// cascading mismatches with multi-level strategies (e.g. grids) where a one-level
// shift would cause every order to match the wrong neighbor.
func (e *Engine) findBestMatch(d DesiredOrder, open []*TrackedOrder, matched map[string]bool, amendThreshold float64) *TrackedOrder {
	var best *TrackedOrder
	bestDist := math.MaxFloat64

	for _, o := range open {
		if matched[o.ClOrderID] {
			continue
		}
		if o.Side != d.Side {
			continue
		}
		dist := math.Abs(o.Price - d.Price)
		if dist < bestDist {
			bestDist = dist
			best = o
		}
	}

	// Only return a match if it's close enough. If the best match is farther
	// than the amend threshold, it's a different order — don't match it.
	// The old order will get cancelled as unmatched, and a new one placed.
	if best != nil && bestDist > amendThreshold {
		return nil
	}

	return best
}

// Stop cancels all orders, deactivates dead man's switch, and shuts down connections.
func (e *Engine) Stop() {
	log.Printf("engine: cancelling all orders...")
	if err := e.orders.CancelAll(); err != nil {
		log.Printf("engine: cancel all: %v", err)
	}

	// Deactivate dead man's switch
	if e.config.DeadManTimeout > 0 {
		if err := e.client.CancelAllAfter(-1); err != nil {
			log.Printf("engine: deactivate dead man: %v", err)
		}
	}

	if e.privateWS != nil {
		e.privateWS.Close()
	}
	if e.midPrice != nil {
		e.midPrice.Stop()
	}
}

// Position returns the position tracker.
func (e *Engine) Position() *PositionTracker {
	return e.position
}

// Orders returns the order manager.
func (e *Engine) Orders() *OrderManager {
	return e.orders
}

// Risk returns the risk manager.
func (e *Engine) Risk() *RiskManager {
	return e.risk
}
