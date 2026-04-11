package main

import (
	"flag"
	"fmt"
	"log"

	"codeberg.org/btsemm/btsemm"
)

var (
	simpleSize   = flag.Float64("simple.size", 20, "simple: order size in USDT")
	simpleSpread = flag.Float64("simple.spread", 10, "simple: half-spread in basis points")
)

func init() {
	registerStrategy("simple", newSimpleMM)
}

type simpleMM struct {
	orderSizeUSDT float64
	spreadBPS     float64
}

func newSimpleMM() btsemm.Strategy {
	return &simpleMM{
		orderSizeUSDT: *simpleSize,
		spreadBPS:     *simpleSpread,
	}
}

func (s *simpleMM) Init(ctx btsemm.StrategyContext) error {
	size := s.orderSizeUSDT / ctx.LastPrice
	log.Printf("strategy init: order_size=%.6f %s (%.2f USDT @ %.6f)",
		size, ctx.Symbol, s.orderSizeUSDT, ctx.LastPrice)

	if size < ctx.Market.MinOrderSize {
		return fmt.Errorf("order size %.6f < exchange min %.6f — increase -simple.size",
			size, ctx.Market.MinOrderSize)
	}

	return nil
}

func (s *simpleMM) OnTick(state btsemm.MarketState) []btsemm.DesiredOrder {
	if state.Mid == 0 {
		return nil
	}

	halfSpread := state.Mid * (s.spreadBPS / 10000.0)
	bidPrice := state.Mid - halfSpread
	askPrice := state.Mid + halfSpread
	size := s.orderSizeUSDT / state.Mid

	log.Printf("strategy: mid=%.6f bid=%.6f ask=%.6f size=%.6f pos=%.6f",
		state.Mid, bidPrice, askPrice, size, state.Position.BaseQty)

	return []btsemm.DesiredOrder{
		{Side: btsemm.SideBuy, Price: bidPrice, Size: size, PostOnly: true},
		{Side: btsemm.SideSell, Price: askPrice, Size: size, PostOnly: true},
	}
}

func (s *simpleMM) OnFill(fill btsemm.WSFill) {
	log.Printf("strategy: FILL %s %.6f @ %.6f (fee=%.6f %s)",
		fill.Side, fill.Size, fill.Price, fill.Fee, fill.FeeCurrency)
}
