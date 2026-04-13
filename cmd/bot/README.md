# Market Making Bot

A tick-based market making bot built on the `btsemm` engine. Strategies are pluggable — the bot handles connectivity, order management, risk, and reconciliation.

## Build

```
go build -o bot ./cmd/bot
```

## Configuration

Before running the bot, create a `.env` file in the directory you'll run it from with your BTSE API credentials:

```
BTSE_API_KEY="your-api-key"
BTSE_API_SECRET="your-api-secret"
```

The bot loads this file automatically at startup. If the keys are missing or empty, the bot exits with `Please set your API keys in the .env file`.

## Usage

```
./bot [flags]
```

### Flags

**Engine:**

| Flag | Default | Description |
|------|---------|-------------|
| `-symbol` | `BTC-USDT` | Trading pair |
| `-strategy` | `simple` | Strategy to run |
| `-tick` | `1s` | Tick interval |
| `-reconcile` | `30s` | Full reconcile interval (polls exchange) |
| `-deadman` | `30000` | Dead man's switch in ms (exchange cancels all if bot dies) |
| `-max-errors` | `5` | Kill after N consecutive order errors |
| `-amend-bps` | `1` | Amend threshold in BPS of order price (see below) |

**Risk (global, all strategies):**

| Flag | Default | Description |
|------|---------|-------------|
| `-risk.max-pos` | `100` | Max position in USDT (auto-converts to base) |
| `-risk.max-loss` | `10` | Max unrealized loss in USDT before kill |
| `-risk.max-orders` | `10` | Max concurrent open orders |
| `-risk.data-ttl` | `30s` | Kill if no market data for this long |

**Simple strategy:**

| Flag | Default | Description |
|------|---------|-------------|
| `-simple.size` | `20` | Order size per side in USDT |
| `-simple.spread` | `10` | Half-spread in basis points |

**Grid strategy:**

| Flag | Default | Description |
|------|---------|-------------|
| `-grid.grids` | `10` | Number of grid levels |
| `-grid.low` | (required) | Lower price bound |
| `-grid.high` | (required) | Upper price bound |
| `-grid.investment` | `100` | Total investment in USDT |

### Examples

```bash
# BTC with defaults
./bot

# WOO-USDT, tighter spread, bigger size
./bot -symbol WOO-USDT -simple.size 50 -simple.spread 5

# BTC with higher risk tolerance
./bot -symbol BTC-USDT -risk.max-pos 500 -risk.max-loss 50

# Faster ticks, wider spread
./bot -symbol BTC-USDT -tick 500ms -simple.spread 20
```

## How It Works

### Startup

Before the first tick, the engine runs a sequence of checks:

1. **Preflight** — fetches the exchange's market parameters (price tick, size tick, min/max order size) and validates that the account has enough balance on at least one side. Risk limits expressed in USDT are converted to base currency using the current price.
2. **Strategy Init** — calls `strategy.Init()` so the strategy can validate its own requirements (e.g. order size above exchange minimum, sufficient balance for its number of levels).
3. **Mid-price feed** — connects a WebSocket to the orderbook and waits for the first price to arrive before proceeding.
4. **Private WebSocket** — connects and authenticates a second WebSocket for fill notifications and order status updates. Subscribes to `fills` and `notificationApiV3` topics.
5. **Initial reconcile** — polls `GetOpenOrders` from the exchange to sync local state with any existing orders.

### The Tick Loop

Every tick (default 1 second), the engine:

1. **Reads market data** — gets mid-price, bid, ask, spread from the WebSocket feed
2. **Checks risk** — position size, P&L, open order count, data liveness. Kills the bot if any limit is breached
3. **Refreshes dead man's switch** — sends `CancelAllAfter` to the exchange, signaling "I'm alive, don't cancel everything." If the bot dies without deactivating this, the exchange auto-cancels all orders after the timeout
4. **Builds a snapshot** — packages market data + current position + open orders into `MarketState` and passes it to the strategy
5. **Calls the strategy** — `strategy.OnTick(state)` returns `[]DesiredOrder`, a declarative list of what the strategy wants resting on the book
6. **Rounds prices and sizes** — adjusts to exchange tick increments, filters out orders below minimum size
7. **Reconciles orders** — compares desired orders to what's actually resting on the exchange (see below)

### Event-Driven Data (Between Ticks)

Independent of the tick loop, the private WebSocket streams two topics in real-time:

- **`fills`** — updates the `PositionTracker` (base qty, quote spent, fees) and calls `strategy.OnFill()` for immediate reaction
- **`notificationApiV3`** — updates the `OrderManager` local state (order status changes, fill progress)

These arrive between ticks, so the next tick always sees up-to-date position and order state without polling.

### Order Reconciliation

This is the core of the engine. Each tick, the strategy declares *what it wants on the book*. The engine figures out *the minimum work* to get there.

For each desired order, the engine finds the best matching resting order (same side, closest price). Then:

- **Match found, price and size within threshold** → do nothing (zero API calls)
- **Match found, price or size differ beyond threshold** → cancel the old order, place a new one
- **No match found** → place a new order
- **Resting order has no matching desired order** → cancel it

This means if the strategy returns the exact same orders two ticks in a row, the engine makes zero API calls. If the market moves slightly, it still makes zero calls as long as the drift is within the amend threshold.

Every 30 seconds (configurable via `-reconcile`), the engine also polls `GetOpenOrders` from the exchange to catch any drift between local state and reality (e.g. orders that were filled or cancelled externally).

### Amend Threshold (`-amend-bps`)

The amend threshold determines when "close enough" becomes "too far" for a resting order. This is an engine-level setting, independent of any strategy.

**What it compares:** The price of an existing resting order vs the price the strategy now wants for that order.

**How it's calculated:** The threshold equals the resting order's price multiplied by `amend-bps / 10000`. This is computed per-order, so it automatically scales with the asset price.

**Example with `-amend-bps 1` (default, 0.01%):**

```
Tick 1: Strategy wants BUY @ 74,000. No order exists. → Engine places it.

Tick 2: Strategy wants BUY @ 74,002. Existing order at 74,000.
         Difference: $2.00
         Threshold:  74,000 × 1/10000 = $7.40
         $2.00 < $7.40 → DO NOTHING

Tick 3: Strategy wants BUY @ 74,005. Existing order at 74,000.
         Difference: $5.00
         Threshold:  $7.40
         $5.00 < $7.40 → DO NOTHING

Tick 8: Strategy wants BUY @ 74,012. Existing order at 74,000.
         Difference: $12.00
         Threshold:  $7.40
         $12.00 > $7.40 → CANCEL old, PLACE new at 74,012
```

**Same setting on a cheap asset (WOO at $0.02):**

```
Resting order at 0.019900
Strategy wants  0.019903
Difference: 0.000003
Threshold:  0.019900 × 1/10000 = 0.00000199
0.000003 > 0.00000199 → REPLACE
```

The same `-amend-bps 1` value works for both BTC and WOO because it's relative to the order's own price. No need to set different values per pair.

**Trade-offs:**
- **Lower value** (e.g. 0.5) — orders track the strategy more closely, but more cancel+replace calls, more API usage, brief moments with no order on the book during replacement
- **Higher value** (e.g. 5) — orders sit longer, fewer API calls, but quotes get stale relative to where the strategy wants them
- **Default (1)** — reasonable balance for most pairs

```bash
# More responsive
./bot -amend-bps 0.5

# More stable
./bot -amend-bps 5
```

### WebSocket Reconnection

The private WebSocket uses `ReconnectingWSConn`, which monitors the connection and re-dials with exponential backoff (1s → 2s → 4s → ... capped at 30s). On reconnect it re-subscribes to recorded topics and triggers a full order reconcile to catch any fills or state changes missed during the disconnect.

Position and order state live in memory, not in the WebSocket connection, so they survive reconnections without data loss.

### Shutdown

Ctrl+C triggers graceful shutdown:

1. Cancels all open orders
2. Deactivates the dead man's switch (timeout = -1)
3. Closes both WebSocket connections (private + mid-price feed)

If the bot crashes without shutdown, the exchange's dead man's switch cancels all orders after the configured timeout (default 30 seconds).

### Error Handling

The engine tracks consecutive order errors. If placing or replacing orders fails N times in a row (configurable via `-max-errors`, default 5), the kill switch fires: all orders are cancelled and the bot shuts down. This prevents runaway API calls when something is fundamentally wrong (e.g. insufficient balance, exchange maintenance).

## Strategies

| Name | Description | Docs |
|------|-------------|------|
| `simple` | One bid + one ask at fixed spread around mid | [strategy_simple.md](strategy_simple.md) |
| `grid` | Geometric grid with fixed levels, flips on fill | [strategy_grid.md](strategy_grid.md) |

### Strategy Interface

Every strategy implements three methods:

```go
type Strategy interface {
    Init(ctx StrategyContext) error       // validate requirements at startup
    OnTick(state MarketState) []DesiredOrder  // return desired orders each tick
    OnFill(fill WSFill)                  // react to fills in real-time
}
```

- **Init** — called once after preflight. Receives the client, symbol, market info (tick sizes, min/max order size), and last price. Return an error to abort startup.
- **OnTick** — called every tick with a market snapshot. Returns a declarative list of desired orders. The engine handles all placement, cancellation, and reconciliation.
- **OnFill** — called immediately when a fill arrives via WebSocket, between ticks. Use for logging or immediate reactions.

### Adding a Strategy

Create a single file `cmd/bot/strategy_yourname.go`. It self-registers via `init()` — no other files need editing.

Use a prefix for your flags (`yourname.`) to avoid collisions with other strategies. Flags are declared as package-level vars so they're registered before `main()` runs and show up in `./bot -help`.

```go
package main

import (
    "flag"
    "codeberg.org/btsemm/btsemm"
)

// Strategy-specific flags — prefixed with "yourname."
var (
    ynLevels  = flag.Int("yourname.levels", 3, "yourname: number of levels per side")
    ynSpacing = flag.Float64("yourname.spacing", 5, "yourname: spacing between levels in BPS")
)

func init() {
    registerStrategy("yourname", newYourName)
}

func newYourName() btsemm.Strategy {
    return &yourName{
        levels:  *ynLevels,
        spacing: *ynSpacing,
    }
}

type yourName struct {
    levels  int
    spacing float64
}

func (s *yourName) Init(ctx btsemm.StrategyContext) error                  { return nil }
func (s *yourName) OnTick(state btsemm.MarketState) []btsemm.DesiredOrder  { return nil }
func (s *yourName) OnFill(fill btsemm.WSFill)                             {}
```

Run with `./bot -strategy yourname -yourname.levels 5 -yourname.spacing 10`

## Logging

All output goes to both stdout and `bot.log` in the working directory. The log file appends across runs.

## Files

```
cmd/bot/
├── main.go               # Entry point, CLI flags
├── strategies.go         # Strategy registry
├── strategy_simple.go    # Simple MM strategy
├── strategy_simple.md    # Simple strategy docs
├── strategy_grid.go      # Geometric grid strategy
├── strategy_grid.md      # Grid strategy docs
└── README.md             # This file
```
