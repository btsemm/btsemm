# Commands

## Bot

| Command | Description |
|---------|-------------|
| `bot` | Market making / grid trading bot with pluggable strategies. See [bot/README.md](bot/README.md) |

## Tools

| Command | Description |
|---------|-------------|
| `orders` | List all open orders on the exchange, sorted by price |
| `cancelall` | Cancel all open orders for a symbol |
| `wslisten` | Connect to private WebSocket and print all notifications (debug tool) |
| `smoketest` | Run integration tests against the exchange API |

## Market Data

| Command | Description |
|---------|-------------|
| `price` | Watch real-time prices using two WebSocket connections (OSS + trades) |
| `price2` | Watch real-time prices using the `WatchMidPrice` helper |
| `watch` | Watch trade feed and best bid/offer |
| `checkpairs` | List available BTC trading pairs |

## Usage

All commands read API keys from `.env` in the working directory. Most support `-testnet` for BTSE testnet.

```bash
# Build and run
go run ./cmd/orders
go run ./cmd/orders -testnet -symbol WOO-USDT

# Or build a binary
go build -o orders ./cmd/orders
./orders -symbol BTC-USDT
```
