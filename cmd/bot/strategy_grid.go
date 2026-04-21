package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"strings"
	"sync"

	"codeberg.org/btsemm/btsemm"
)

var (
	gridGrids      = flag.Int("grid.grids", 10, "grid: number of grid levels")
	gridLow        = flag.Float64("grid.low", 0, "grid: lower price bound")
	gridHigh       = flag.Float64("grid.high", 0, "grid: upper price bound")
	gridInvestment = flag.Float64("grid.investment", 100, "grid: total investment in USDT")
)

const defaultMakerFeePct = 0.2 // fallback if API fails

func init() {
	registerStrategy("grid", newGrid)
}

// gridState tracks whether a grid gap is waiting to buy or sell.
type gridState int

const (
	gridBuy  gridState = iota // waiting to buy at lower level
	gridSell                  // holding, waiting to sell at upper level
)

type gridStrategy struct {
	mu          sync.Mutex
	grids       int
	low         float64
	high        float64
	investment  float64
	levels      []float64   // grids+1 price levels
	state       []gridState // grids entries: buy or sell per gap
	ratio       float64
	sizePerGrid float64 // USDT per grid
	makerFeePct float64 // actual maker fee from exchange
	lastMid     float64 // last mid observed by OnTick; used by DumpState

	// Profit tracking
	totalProfitUSDT float64 // accumulated net profit from completed grid cycles
	completedCycles int     // number of completed buy→sell round trips
}

func newGrid() btsemm.Strategy {
	return &gridStrategy{
		grids:      *gridGrids,
		low:        *gridLow,
		high:       *gridHigh,
		investment: *gridInvestment,
	}
}

func (s *gridStrategy) Init(ctx btsemm.StrategyContext) error {
	if s.low <= 0 || s.high <= 0 {
		return fmt.Errorf("grid: -grid.low and -grid.high are required")
	}
	if s.low >= s.high {
		return fmt.Errorf("grid: low (%.6f) must be less than high (%.6f)", s.low, s.high)
	}
	if s.grids < 2 {
		return fmt.Errorf("grid: need at least 2 grids")
	}

	// Geometric ratio
	s.ratio = math.Pow(s.high/s.low, 1.0/float64(s.grids))

	// Build levels
	s.levels = make([]float64, s.grids+1)
	for i := 0; i <= s.grids; i++ {
		s.levels[i] = s.low * math.Pow(s.ratio, float64(i))
	}

	// USDT per grid
	s.sizePerGrid = s.investment / float64(s.grids)

	// Fetch actual maker fee from exchange
	s.makerFeePct = defaultMakerFeePct
	fees, err := ctx.Client.GetFees(ctx.Symbol)
	if err != nil {
		log.Printf("grid: WARNING: could not fetch fees, using default %.2f%%", defaultMakerFeePct)
	} else if len(fees) > 0 {
		s.makerFeePct = fees[0].MakerFee * 100 // API returns 0.002 for 0.2%
	}
	log.Printf("grid: maker fee      %.4f%%", s.makerFeePct)

	// Profit calculation
	grossProfitPct := (s.ratio - 1) * 100
	feesPct := s.makerFeePct * 2
	netProfitPct := grossProfitPct - feesPct

	// Check current price is within range
	if ctx.LastPrice < s.low || ctx.LastPrice > s.high {
		return fmt.Errorf("grid: current price %.6f is outside range [%.6f, %.6f]",
			ctx.LastPrice, s.low, s.high)
	}

	// Initialize grid states: below current price = buy, above = sell.
	// The gap containing the current price is a sell (place sell at upper boundary).
	// This keeps the gap between last buy and first sell equal to one grid spacing.
	s.state = make([]gridState, s.grids)
	buyCount := 0
	sellCount := 0
	for i := 0; i < s.grids; i++ {
		if s.levels[i+1] <= ctx.LastPrice {
			// Gap is entirely below current price — buy at lower level
			s.state[i] = gridBuy
			buyCount++
		} else {
			// Gap contains or is above current price — sell at upper level
			s.state[i] = gridSell
			sellCount++
		}
	}

	// Balance requirements
	quoteNeeded := float64(buyCount) * s.sizePerGrid
	baseNeeded := 0.0
	for i := 0; i < s.grids; i++ {
		if s.state[i] == gridSell {
			baseNeeded += s.sizePerGrid / s.levels[i]
		}
	}

	// Parse base/quote from symbol
	parts := strings.SplitN(ctx.Symbol, "-", 2)
	if len(parts) != 2 {
		return fmt.Errorf("grid: cannot parse symbol %q", ctx.Symbol)
	}
	base, quote := parts[0], parts[1]

	// Check balances
	baseBal, _ := ctx.Client.GetCurrencyBalance(base)
	quoteBal, _ := ctx.Client.GetCurrencyBalance(quote)
	baseAvail := 0.0
	if baseBal != nil {
		baseAvail = baseBal.Available
	}
	quoteAvail := 0.0
	if quoteBal != nil {
		quoteAvail = quoteBal.Available
	}

	// Print grid summary
	log.Printf("grid: ═══════════════════════════════════════════════")
	log.Printf("grid: GEOMETRIC GRID — %s", ctx.Symbol)
	log.Printf("grid: ═══════════════════════════════════════════════")
	log.Printf("grid: range          %.6f — %.6f", s.low, s.high)
	log.Printf("grid: grids          %d", s.grids)
	log.Printf("grid: ratio          %.6f (%.4f%% per level)", s.ratio, (s.ratio-1)*100)
	log.Printf("grid: investment     %.2f %s", s.investment, quote)
	log.Printf("grid: per grid       %.2f %s", s.sizePerGrid, quote)
	log.Printf("grid: current price  %.6f", ctx.LastPrice)
	log.Printf("grid: buy orders     %d (need %.2f %s)", buyCount, quoteNeeded, quote)
	log.Printf("grid: sell orders    %d (need %.6f %s)", sellCount, baseNeeded, base)
	log.Printf("grid: ───────────────────────────────────────────────")
	log.Printf("grid: gross profit   %.4f%% per grid", grossProfitPct)
	log.Printf("grid: fees           %.4f%% (maker %.4f%% × 2)", feesPct, s.makerFeePct)
	log.Printf("grid: NET PROFIT     %.4f%% per grid", netProfitPct)
	if netProfitPct <= 0 {
		log.Printf("grid: WARNING: net profit is negative! Increase range or decrease grids.")
	}
	log.Printf("grid: ───────────────────────────────────────────────")

	// Print each level with profit
	for i := 0; i < s.grids; i++ {
		side := "BUY "
		if s.state[i] == gridSell {
			side = "SELL"
		}
		size := s.sizePerGrid / s.levels[i]
		gridGrossPct := (s.levels[i+1]/s.levels[i] - 1) * 100
		gridNetPct := gridGrossPct - s.makerFeePct*2
		gridNetUSDT := s.sizePerGrid * gridNetPct / 100
		priceMark := ""
		if s.levels[i] <= ctx.LastPrice && s.levels[i+1] > ctx.LastPrice {
			priceMark = fmt.Sprintf("  ◄ price %.6f", ctx.LastPrice)
		}
		profitMark := ""
		if gridNetPct <= 0 {
			profitMark = " !!!"
		}
		log.Printf("grid:   [%2d] %s  %8.1f → %8.1f  size=%.6f  profit=%.4f%% ($%.4f)%s%s",
			i, side, s.levels[i], s.levels[i+1], size, gridNetPct, gridNetUSDT, profitMark, priceMark)
	}

	log.Printf("grid: ═══════════════════════════════════════════════")

	// Validate balances
	if quoteAvail < quoteNeeded {
		return fmt.Errorf("grid: insufficient %s: have %.4f, need %.4f for %d buy orders",
			quote, quoteAvail, quoteNeeded, buyCount)
	}
	if baseAvail < baseNeeded {
		return fmt.Errorf("grid: insufficient %s: have %.6f, need %.6f for %d sell orders",
			base, baseAvail, baseNeeded, sellCount)
	}

	// Validate order sizes against exchange minimums
	smallestSize := s.sizePerGrid / s.high
	if smallestSize < ctx.Market.MinOrderSize {
		return fmt.Errorf("grid: smallest order %.6f < exchange min %.6f — increase -grid.investment or decrease -grid.grids",
			smallestSize, ctx.Market.MinOrderSize)
	}

	log.Printf("grid: all checks passed, ready to trade")
	return nil
}

func (s *gridStrategy) OnTick(state btsemm.MarketState) []btsemm.DesiredOrder {
	if state.Mid == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastMid = state.Mid

	var orders []btsemm.DesiredOrder

	for i := 0; i < s.grids; i++ {
		size := s.sizePerGrid / s.levels[i]

		if s.state[i] == gridBuy {
			orders = append(orders, btsemm.DesiredOrder{
				Side:     btsemm.SideBuy,
				Price:    s.levels[i],
				Size:     size,
				PostOnly: true,
			})
		} else {
			orders = append(orders, btsemm.DesiredOrder{
				Side:     btsemm.SideSell,
				Price:    s.levels[i+1],
				Size:     size,
				PostOnly: true,
			})
		}
	}

	return orders
}

// DumpState implements the btsemm.Dumper interface. It prints every grid level
// with its current side (gridBuy/gridSell), the price range it covers, and a
// marker on the gap that brackets the most recently observed mid price.
//
// The mid is taken from the strategy's own observation, since the dumper has
// no access to the live MidPrice. Callers that have a current mid can pass it
// via DumpStateAt; DumpState falls back to the last value seen by OnTick.
func (s *gridStrategy) DumpState(w io.Writer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	buyCount, sellCount := 0, 0
	fmt.Fprintf(w, "grid: === GRID STATE (%d grids, ratio=%.6f, sizePerGrid=%.4f) ===\n",
		s.grids, s.ratio, s.sizePerGrid)
	for i := 0; i < s.grids; i++ {
		state := "BUY "
		if s.state[i] == gridSell {
			state = "SELL"
			sellCount++
		} else {
			buyCount++
		}
		marker := ""
		if s.lastMid > 0 && s.levels[i] <= s.lastMid && s.levels[i+1] > s.lastMid {
			marker = fmt.Sprintf("  ◄ contains mid %.6f", s.lastMid)
		}
		size := s.sizePerGrid / s.levels[i]
		fmt.Fprintf(w, "grid:   [%2d] %s  %10.6f → %10.6f  size=%.6f  state=%s%s\n",
			i, state, s.levels[i], s.levels[i+1], size, stateName(s.state[i]), marker)
	}
	fmt.Fprintf(w, "grid: summary: %d gridBuy, %d gridSell\n", buyCount, sellCount)
	fmt.Fprintf(w, "grid: === END GRID STATE ===\n")
}

// GridLevelStatus is the JSON representation of a single grid level.
type GridLevelStatus struct {
	Index int     `json:"index"`
	Lower float64 `json:"lower"`
	Upper float64 `json:"upper"`
	Side  string  `json:"side"`
	Size  float64 `json:"size"`
}

// GridStatus is the JSON representation of the full grid strategy state.
type GridStatus struct {
	Grids           int               `json:"grids"`
	Low             float64           `json:"low"`
	High            float64           `json:"high"`
	Ratio           float64           `json:"ratio"`
	SizePerGrid     float64           `json:"sizePerGrid"`
	MakerFeePct     float64           `json:"makerFeePct"`
	LastMid         float64           `json:"lastMid"`
	BuyCount        int               `json:"buyCount"`
	SellCount       int               `json:"sellCount"`
	TotalProfitUSDT float64           `json:"totalProfitUSDT"`
	CompletedCycles int               `json:"completedCycles"`
	ProfitPerCycle  float64           `json:"profitPerCycle"`
	Levels          []GridLevelStatus `json:"levels"`
}

// Status implements btsemm.StatusProvider. Returns a GridStatus struct.
func (s *gridStrategy) Status() interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	profitPerCycle := 0.0
	if s.completedCycles > 0 {
		profitPerCycle = s.totalProfitUSDT / float64(s.completedCycles)
	}
	gs := GridStatus{
		Grids:           s.grids,
		Low:             s.low,
		High:            s.high,
		Ratio:           s.ratio,
		SizePerGrid:     s.sizePerGrid,
		MakerFeePct:     s.makerFeePct,
		LastMid:         s.lastMid,
		TotalProfitUSDT: s.totalProfitUSDT,
		CompletedCycles: s.completedCycles,
		ProfitPerCycle:  profitPerCycle,
		Levels:          make([]GridLevelStatus, s.grids),
	}
	for i := 0; i < s.grids; i++ {
		side := "BUY"
		if s.state[i] == gridSell {
			side = "SELL"
			gs.SellCount++
		} else {
			gs.BuyCount++
		}
		gs.Levels[i] = GridLevelStatus{
			Index: i,
			Lower: s.levels[i],
			Upper: s.levels[i+1],
			Side:  side,
			Size:  s.sizePerGrid / s.levels[i],
		}
	}
	return gs
}

func stateName(s gridState) string {
	if s == gridBuy {
		return "gridBuy"
	}
	return "gridSell"
}

func (s *gridStrategy) OnFill(fill btsemm.WSFill) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Match fill to grid level by price.
	// Buy orders are placed at levels[i], sell orders at levels[i+1].
	for i := 0; i < s.grids; i++ {
		if fill.Side == "BUY" && s.state[i] == gridBuy {
			// Buy order is at levels[i] — match if fill price is close
			if math.Abs(fill.Price-s.levels[i])/s.levels[i] < 0.001 {
				s.state[i] = gridSell
				log.Printf("grid: [%2d] FILLED BUY @ %.6f → now SELL @ %.6f", i, fill.Price, s.levels[i+1])
				return
			}
		}
		if fill.Side == "SELL" && s.state[i] == gridSell {
			// Sell order is at levels[i+1] — match if fill price is close
			if math.Abs(fill.Price-s.levels[i+1])/s.levels[i+1] < 0.001 {
				s.state[i] = gridBuy
				profit := (s.levels[i+1]/s.levels[i] - 1) * 100
				netProfit := profit - s.makerFeePct*2
				netProfitUSDT := s.sizePerGrid * netProfit / 100
				s.totalProfitUSDT += netProfitUSDT
				s.completedCycles++
				log.Printf("grid: [%2d] FILLED SELL @ %.6f → now BUY @ %.6f (net profit %.4f%% / $%.4f, total $%.4f in %d cycles)",
					i, fill.Price, s.levels[i], netProfit, netProfitUSDT, s.totalProfitUSDT, s.completedCycles)
				return
			}
		}
	}

	log.Printf("grid: FILL %s %.6f @ %.6f (unmatched to grid)", fill.Side, fill.Size, fill.Price)
}
