# Simple Market Making Strategy

A minimal strategy that places one bid and one ask at a fixed spread around the mid-price.

## How It Works

Each tick (default 1s), the strategy receives a `MarketState` snapshot containing the current mid-price, bid, ask, spread, position, and open orders.

It returns two orders:

```
        ask = mid + (mid × spreadBPS / 10000)
  ---- mid ----
        bid = mid - (mid × spreadBPS / 10000)
```

The order size is calculated from the USDT target:

```
size = orderSizeUSDT / mid
```

Both orders are **post-only**, meaning they will be rejected rather than cross the spread and take liquidity. This ensures you always pay maker fees (which are lower, sometimes negative).

## What the Engine Does

The strategy just declares what it wants. The engine handles the rest:

- **Rounding** prices and sizes to exchange tick increments
- **Diffing** desired orders against what's already on the book
- **Skipping** updates when the price hasn't moved enough (amend threshold = 5% of total spread)
- **Cancel+replace** when the price moves beyond the threshold
- **Risk checks** before every order placement

## Parameters

| Flag | Default | Description |
|------|---------|-------------|
| `-simple.size` | 20 | Order size in USDT per side |
| `-simple.spread` | 10 | Half-spread in basis points (10 = 0.1% each side, 0.2% total) |

## Examples

BTC-USDT at $74,000 with defaults (size=20, spread=10):
- Half-spread: $74,000 × 10/10000 = **$74**
- Bid at ~$73,926, ask at ~$74,074
- Size: 20 / 74000 = **0.00027 BTC** per side
- Total spread: 0.2% ($148)

WOO-USDT at $0.02 with defaults:
- Half-spread: $0.02 × 10/10000 = **$0.000020**
- Bid at ~$0.01998, ask at ~$0.02002
- Size: 20 / 0.02 = **1000 WOO** per side

Tighter spread for more fills:
```
./bot -symbol BTC-USDT -simple.spread 3 -simple.size 20
```
Places orders 0.03% each side (~$22 from mid on BTC). More likely to get filled, but more inventory risk.

## Limitations

- **No inventory management** — doesn't skew quotes when holding a position
- **No volatility awareness** — same spread whether the market is calm or moving fast
- **Single level** — one bid and one ask, not a ladder of orders
- **Fixed size** — doesn't adjust order size based on conditions

These are intentional. This strategy exists to test the engine infrastructure. Smarter strategies can be built on top of the same engine by implementing the `Strategy` interface.
