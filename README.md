# btsemm

Go package for the BTSE Spot Exchange API v3.3.

## Setup

```bash
git clone https://codeberg.org/btsemm/btsemm
```

Create a `.env` file (make sure it's in `.gitignore`):

```bash
export BTSE_API_KEY="your-api-key"
export BTSE_API_SECRET="your-api-secret"
```

## Client

```go
// From environment variables
client := btsemm.NewFromEnv()

// Manual
client := btsemm.New("api-key", "api-secret")

// Testnet
client := btsemm.New("api-key", "api-secret", btsemm.WithTestnet())

// Public only (no auth needed for market data)
client := btsemm.New("", "")
```

## Test Authentication

```go
if err := client.TestAuth(); err != nil {
    log.Fatal("bad keys:", err)
}
```

## Key Capabilities

```go
caps, err := client.GetKeyCapabilities()
// caps.Valid  - key is accepted
// caps.Read   - can query balances, fees, history
// caps.Trade  - can place/cancel orders
```

## Market Data (public, no auth)

```go
// Market summary (all markets or specific symbol)
summaries, err := client.GetMarketSummary("")
summaries, err := client.GetMarketSummary("BTC-USDT")

// Current price
prices, err := client.GetPrice("BTC-USDT")

// Candlestick data (resolution in minutes: "1", "5", "15", "60", "1440")
candles, err := client.GetOHLCV("BTC-USDT", "60", startEpoch, endEpoch)

// L2 orderbook
ob, err := client.GetOrderbook("BTC-USDT", 0)

// Recent trades
trades, err := client.GetTrades("BTC-USDT", 10, 0, 0)

// Server time
t, err := client.GetServerTime()
```

## Wallet

```go
// All balances
balances, err := client.GetWalletBalance()
for _, b := range balances {
    fmt.Printf("%s: total=%.8f available=%.8f\n", b.Currency, b.Total, b.Available)
}

// Single currency
bal, err := client.GetCurrencyBalance("USDT")
// bal.Total, bal.Available, locked = bal.Total - bal.Available
```

## Trading

### Place an order

```go
orders, err := client.CreateOrder(&btsemm.CreateOrderRequest{
    Symbol: "BTC-USDT",
    Side:   btsemm.SideBuy,
    Type:   btsemm.OrderTypeLimit,
    Price:  50000,
    Size:   0.0004,
})
// orders[0].OrderID, orders[0].Status
```

### Query an order

```go
order, err := client.GetOrder("BTC-USDT", "order-id", "")
```

### Amend an order

```go
order, err := client.AmendOrder(&btsemm.AmendOrderRequest{
    Symbol:  "BTC-USDT",
    OrderID: "order-id",
    Type:    btsemm.OrderTypeLimit,
    Price:   51000,
    Size:    0.0005,
})
```

### Cancel an order

```go
cancelled, err := client.CancelOrder(&btsemm.CancelOrderRequest{
    Symbol:  "BTC-USDT",
    OrderID: "order-id",
})
```

### Dead man's switch

Cancels all orders if no new request within timeout (milliseconds). Set to -1 to deactivate.

```go
err := client.CancelAllAfter(60000) // cancel all in 60s if no heartbeat
err := client.CancelAllAfter(-1)    // deactivate
```

### Open orders & history

```go
open, err := client.GetOpenOrders("")          // all symbols
open, err := client.GetOpenOrders("BTC-USDT")  // specific symbol

history, err := client.GetTradeHistory("BTC-USDT", 0, 0, 50)

fees, err := client.GetFees("BTC-USDT")
// fees[0].MakerFee, fees[0].TakerFee
```

## Order Status Codes

| Code | Constant | Meaning |
|------|----------|---------|
| 2 | `OrderStatusInserted` | Order accepted |
| 4 | `OrderStatusFullyTransacted` | Fully filled |
| 5 | `OrderStatusPartiallyFilled` | Partially filled |
| 6 | `OrderStatusCancelled` | Cancelled |
| 7 | `OrderStatusRefunded` | Refunded |
| 8 | `OrderStatusInsufficientBal` | Insufficient balance |
| 9 | `OrderStatusTriggerInserted` | Trigger order placed |
| 10 | `OrderStatusTriggerActivated` | Trigger activated |
| 15 | `OrderStatusRejected` | Rejected |
| 16 | `OrderStatusNotFound` | Not found |
| 17 | `OrderStatusRequestFailed` | Request failed |

## Live Mid-Price

The simplest way to track a symbol's price in real-time. Starts a background WebSocket
that maintains the current mid-price (average of best bid and ask from the L1 orderbook).

```go
mp, err := client.WatchMidPrice("BTC-USDT")
if err != nil {
    log.Fatal(err)
}
defer mp.Stop()

// Wait for first price to arrive
<-mp.Ready()

// Call from anywhere, anytime — thread-safe
mp.Price()    // 74254.20  (mid-price)
mp.Bid()      // 74254.00
mp.Ask()      // 74254.40
mp.Spread()   // 0.40

// Or get everything at once
mid, bid, ask, spread := mp.Snapshot()
```

## WebSocket

### Watch trades

```go
ws, err := client.ConnectPublicWS(
    btsemm.WithWSHandler(func(msg btsemm.WSMessage) {
        trades, _ := msg.ParseTrades()
        for _, t := range trades {
            fmt.Printf("%s %.2f x %.6f\n", t.Side, t.Price, t.Size)
        }
    }),
    btsemm.WithWSErrorHandler(func(err error) {
        log.Println("ws error:", err)
    }),
)
defer ws.Close()

ws.Subscribe("tradeHistoryApi:BTC-USDT")
<-ws.Done()
```

### L1 orderbook snapshots (best bid/offer)

**Note:** L1 snapshots are served from the OSS WebSocket (`ConnectOrderbookWS`), not the regular one.

```go
ws, err := client.ConnectOrderbookWS(btsemm.WithWSHandler(func(msg btsemm.WSMessage) {
    snap, _ := msg.ParseSnapshotL1()
    fmt.Printf("bid=%s ask=%s\n", snap.Bids[0][0], snap.Asks[0][0])
}))
defer ws.Close()

ws.Subscribe("snapshotL1:BTC-USDT")
<-ws.Done()
```

### Orderbook stream (L2 deltas)

```go
ws, err := client.ConnectOrderbookWS(btsemm.WithWSHandler(func(msg btsemm.WSMessage) {
    update, _ := msg.ParseOrderbookUpdate()
    fmt.Printf("type=%s seq=%d bids=%d asks=%d\n",
        update.Type, update.SeqNum, len(update.Bids), len(update.Asks))
}))
defer ws.Close()

ws.Subscribe("update:BTC-USDT_0")
<-ws.Done()
```

### Private stream (order notifications & fills)

```go
ws, err := client.ConnectPrivateWS(btsemm.WithWSHandler(func(msg btsemm.WSMessage) {
    fmt.Printf("topic=%s data=%s\n", msg.Topic, msg.Data)
}))
defer ws.Close()

ws.Subscribe("notificationApiV3", "fills")
<-ws.Done()
```

## CLI Tools

### Smoke test

Tests auth, balance, order placement, and cancellation:

```bash
go run ./cmd/smoketest/
```

### Watch trades

```bash
go run ./cmd/watch/              # BTC-USDT default
go run ./cmd/watch/ ETH-USDT
```

### Watch price (detailed)

Live mid-price, bid, ask, spread, and last trade using dual WebSocket streams:

```bash
go run ./cmd/price/              # BTC-USDT default
go run ./cmd/price/ SOL-USDT
```

### Watch price (simple)

Clean single-line mid-price using `WatchMidPrice`:

```bash
go run ./cmd/price2/             # BTC-USDT default
go run ./cmd/price2/ ETH-USDT
```

## API Endpoints

| Environment | REST | WebSocket | WebSocket (Orderbook) |
|---|---|---|---|
| Production | `https://api.btse.com/spot` | `wss://ws.btse.com/ws/spot` | `wss://ws.btse.com/ws/oss/spot` |
| Testnet | `https://testapi.btse.io/spot` | `wss://testws.btse.io/ws/spot` | `wss://testws.btse.io/ws/oss/spot` |

## Rate Limits

- Queries: 15 req/s per API key, 30 req/s per user
- Orders: 75 req/s per API key and per user
- Violations result in 1s, 5min, or 15min blocks with `Retry-After` header
