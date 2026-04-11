// orders lists all open orders on the exchange, sorted by price.
//
// Usage:
//
//	go run ./cmd/orders
//	go run ./cmd/orders -testnet
//	go run ./cmd/orders -symbol WOO-USDT
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"codeberg.org/btsemm/btsemm"
)

func init() {
	data, _ := os.ReadFile(".env")
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		if k, v, ok := strings.Cut(line, "="); ok {
			os.Setenv(k, strings.Trim(v, `"`))
		}
	}
}

func main() {
	testnet := flag.Bool("testnet", false, "use BTSE testnet")
	symbol := flag.String("symbol", "BTC-USDT", "trading pair")
	flag.Parse()

	var opts []btsemm.Option
	if *testnet {
		opts = append(opts, btsemm.WithTestnet())
	}
	c := btsemm.NewFromEnv(opts...)

	orders, err := c.GetOpenOrders(*symbol)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		os.Exit(1)
	}

	sort.Slice(orders, func(i, j int) bool { return orders[i].Price < orders[j].Price })

	buys := 0
	sells := 0
	for _, o := range orders {
		if o.Side == "BUY" {
			buys++
		} else {
			sells++
		}
		fmt.Printf("  %s  %10.1f  size=%.6f  clOrderID=%s\n", o.Side, o.Price, o.RemainingOrderBaseSize, o.ClOrderID)
	}
	fmt.Printf("\nTotal: %d (%d buys, %d sells)\n", len(orders), buys, sells)

	prices, _ := c.GetPrice(*symbol)
	if len(prices) > 0 {
		fmt.Printf("Current price: %.2f\n", prices[0].LastPrice)
	}
}
