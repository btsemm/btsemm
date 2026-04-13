# Geometric Grid Strategy

A grid trading strategy that places buy and sell orders at fixed price levels with equal percentage spacing between them.

## How It Works

### Setup

Given a price range (`low` to `high`) and a number of grids, the strategy calculates price levels using a geometric sequence:

```
ratio = (high / low) ^ (1 / grids)

level[0] = low
level[1] = low × ratio
level[2] = low × ratio²
...
level[n] = high
```

Each gap between adjacent levels has the same percentage difference. For example, with 10 grids from $60,000 to $80,000:

```
ratio = 1.029186 (2.92% per level)

60000.00 → 61751.16 → 63553.43 → 65408.30 → 67317.31 →
69282.03 → 71304.10 → 73385.18 → 75527.00 → 77731.33 → 80000.00
```

### Initial State

At startup, the current price determines which levels are buys and which are sells:

- **Below current price** → BUY orders (waiting to buy the dip)
- **Above current price** → SELL orders (holding inventory, waiting to sell the rip)

```
BUY  @ 60000    ┐
BUY  @ 61751    │ buy orders — need USDT
BUY  @ 63553    │
BUY  @ 65408    ┘
     ◄ price 68450
SELL @ 71304    ┐
SELL @ 73385    │ sell orders — need BTC
SELL @ 75527    │
SELL @ 77731    │
SELL @ 80000    ┘
```

### Fill Logic

When an order fills, its grid level flips:

- **Buy fills** at level N → flip to SELL at level N+1 (the upper boundary)
- **Sell fills** at level N → flip to BUY at level N (the lower boundary)

This is how the grid captures profit: buy low, sell high, repeat.

```
Price drops to 65408, buy fills:
  BUY @ 65408 → now SELL @ 67317

Price rises to 67317, sell fills:
  SELL @ 67317 → now BUY @ 65408
  Net profit: 2.92% - 0.4% fees = 2.52%
```

### Order Prices Are Fixed

The grid levels never change after startup. The engine's amend threshold ensures that each order matches exactly to its level — no shifting, no cascading replacements. Orders sit on the book until filled.

## Profit Per Grid

Each completed buy→sell cycle captures the geometric ratio minus round-trip fees:

```
Gross profit = (ratio - 1) × 100%
Fees         = maker_fee × 2  (buy + sell)
Net profit   = gross - fees
```

With 0.2% maker fee:

| Grids | Range | Spacing | Gross | Net |
|-------|-------|---------|-------|-----|
| 5 | 60k–80k | 5.92% | 5.92% | 5.52% |
| 10 | 60k–80k | 2.92% | 2.92% | 2.52% |
| 20 | 60k–80k | 1.44% | 1.44% | 1.04% |
| 50 | 60k–80k | 0.57% | 0.57% | 0.17% |
| 70 | 60k–80k | 0.41% | 0.41% | 0.01% |
| 71+ | 60k–80k | <0.40% | <0.40% | **negative** |

More grids = more fills but less profit per fill. Too many grids and fees eat all the profit.

## Parameters

| Flag | Default | Description |
|------|---------|-------------|
| `-grid.grids` | `10` | Number of grid levels |
| `-grid.low` | (required) | Lower price bound |
| `-grid.high` | (required) | Upper price bound |
| `-grid.investment` | `100` | Total investment in USDT (split evenly across grids) |

## Init Checks

At startup the strategy validates:

1. Current price is within the grid range
2. Net profit per grid is positive (warns if not)
3. Sufficient quote balance (USDT) for all buy orders
4. Sufficient base balance (BTC) for all sell orders
5. Smallest order size is above exchange minimum

## Risk Settings

The engine has global risk limits that apply to all strategies. For the grid strategy, you **must** set `-risk.max-pos` to at least your `-grid.investment` amount, otherwise the risk manager will block buy orders before the grid is fully deployed.

The grid strategy has its own risk boundaries (the price range and investment amount), so the engine's position limit should give it room to operate.

| Flag | Recommended | Why |
|------|-------------|-----|
| `-risk.max-pos` | `>= grid.investment` | Grid needs to buy all levels when price drops |
| `-risk.max-orders` | `>= grid.grids + 5` | One order per grid level plus headroom |
| `-risk.max-loss` | your comfort level | Unrealized loss limit before kill switch |

**What happens if risk.max-pos is too low:** Price drops, the grid buys a few levels, hits the position limit, and all remaining buy orders get silently blocked. The grid sits with sells only and can't buy any more dips. When price recovers it sells what it has, but it missed the lower buy opportunities.

## Examples

```bash
# 10 grids on BTC, $60k-$80k range, $500 total
./bot -strategy grid -symbol BTC-USDT \
  -risk.max-pos 600 -risk.max-orders 20 \
  -grid.grids 10 -grid.low 60000 -grid.high 80000 -grid.investment 500

# 63 grids, $1000 investment
./bot -strategy grid -symbol BTC-USDT \
  -risk.max-pos 1100 -risk.max-orders 200 \
  -grid.grids 63 -grid.low 60000 -grid.high 80000 -grid.investment 1000

# Tight grid on a cheap token
./bot -strategy grid -symbol WOO-USDT \
  -risk.max-pos 250 -risk.max-orders 30 \
  -grid.grids 20 -grid.low 0.015 -grid.high 0.025 -grid.investment 200

# Wide grid, fewer levels, higher profit per fill
./bot -strategy grid -symbol BTC-USDT \
  -grid.grids 5 -grid.low 55000 -grid.high 85000 -grid.investment 1000
```

## Detailed / Advanced Example

A walkthrough of a wide-range grid on a cheap token, explaining how to size each risk parameter against the strategy's economics.

```bash
./bot -strategy grid -symbol JUNO-USDT \
  -grid.low 0.20 -grid.high 1.00 -grid.grids 80 -grid.investment 500 \
  -risk.max-pos 1000 -risk.max-orders 150 -risk.max-loss 500
```

### Grid economics

| Quantity | Value |
|----------|-------|
| Price range | 0.20 → 1.00 (5× span) |
| Grids | 80 |
| Ratio per level | `(1.00 / 0.20)^(1/80) ≈ 1.0203` → ~2.03% spacing |
| Investment / grid | `500 / 80 = 6.25 USDT` per order |
| Gross profit per cycle | ~2.03% |
| Round-trip fees (0.2% maker × 2) | 0.40% |
| **Net profit per cycle** | **~1.63%** (positive → init check passes) |

The smallest order size is `6.25 / 1.00 = 6.25 JUNO` at the top of the range — the init check (`cmd/bot/strategy_grid.go:203`) will reject startup if this is below BTSE's minimum for `JUNO-USDT`. If it does, either raise `-grid.investment` or lower `-grid.grids`.

### Why `-risk.max-pos 1000` (2× investment)

`risk.max-pos` is declared in USDT but converted to base units **once, at startup price** (`engine.go:204-205`):

```
MaxPositionSize = MaxPositionUSDT / lastPrice
```

After that, the risk check compares your live `|baseQty|` against that fixed base-unit limit (`risk.go:68`). The grid buys at levels *below* startup, and USDT at a lower price buys *more* base than the same USDT at startup price — so the base you accumulate can exceed `investment / startupPrice`.

Worked example assuming startup price ≈ 0.45 (roughly the geometric mid of the range):

| Setting | Base-unit cap | Notes |
|---------|---------------|-------|
| `max-pos 500` | ~1,111 JUNO | Minimum per docs (`>= investment`); risks silently blocking buys if price dumps |
| `max-pos 600` | ~1,333 JUNO | 1.2× — matches the simple examples above |
| `max-pos 1000` | ~2,222 JUNO | Current choice; generous headroom, grid can fully deploy even at the bottom |

`1000` is safe and deliberately loose. The tightest "by the book" value is `~600`.

### Why `-risk.max-orders 150`

Needs to be at least `grids + 5 = 85` (per `strategy_grid.md` risk table). `150` leaves comfortable headroom for the brief moments during reconciliation when an old order is being cancelled and its replacement is still being placed.

### Why `-risk.max-loss 500` is effectively "no stop"

This is **not a stop loss** — on trip, the engine cancels open orders and exits; it does **not** market-sell your inventory (`risk.go:123-134`). You're left holding whatever base you'd accumulated.

Unrealized PnL is marked against mid-price (`risk.go:76`):

```
unrealizedPnL = baseQty × midPrice − quoteSpent − totalFees
```

For this grid, if every buy level fills on the way down, average buy price ≈ geometric mean `√(0.20 × 1.00) = 0.447` and total JUNO held ≈ `500 / 0.447 ≈ 1,119`.

| Mid price | Position value (USDT) | Unrealized loss | Trips `max-loss 500`? |
|-----------|----------------------|-----------------|-----------------------|
| 0.447 (startup) | 500 | 0 | no |
| 0.30 | 336 | ~164 | no |
| 0.20 (bottom of range) | 224 | ~276 | no |
| 0.10 (below range) | 112 | ~388 | no |
| 0.00 (total collapse) | 0 | 500 | barely |

`max-loss 500` only fires near total wipeout. Pick based on intent:

| Intent | Suggested `max-loss` | Roughly trips when |
|--------|---------------------|---------------------|
| Ride out the full range, only kill on extreme breakdown | `300` | Price drops below ~`0.20` |
| Bail early if underwater | `100`–`150` | Price around `0.30` |
| Never kill on PnL (current) | `500` | Only on ~total collapse |

Remember: grids are *designed* to accumulate inventory on the way down. An aggressive stop defeats the thesis; a loose one accepts holding the bag if the range breaks. This is a deliberate trade-off, not a number to optimize.

### Pre-flight checklist

1. `.env` with `BTSE_API_KEY` / `BTSE_API_SECRET` in the run directory
2. Quote balance ≥ USDT needed for buy side (~half of `investment` if starting near the geometric mid)
3. Base balance ≥ JUNO needed for sell side (the init log prints the exact figure)
4. Confirm `JUNO-USDT` is a live BTSE pair and the per-grid order size clears exchange minimums
5. Consider a dry run with `-testnet` first

## Limitations

- **No rebalancing** — if price moves outside the range, all orders are on one side and the grid stops producing profit
- **Inventory risk** — if price drops through all buy levels, you're holding max position at a loss
- **Fixed levels** — doesn't adapt to changing volatility or market conditions
- **Single order per level** — doesn't scale order size based on distance from current price
