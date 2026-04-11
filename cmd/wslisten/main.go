// wslisten connects to BTSE private WebSocket and prints all notifications.
// Useful for debugging WS topics, fill events, and order status updates.
//
// Usage:
//
//	go run ./cmd/wslisten
//	go run ./cmd/wslisten -testnet
//	go run ./cmd/wslisten -testnet -test  (places + cancels a test order)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

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
	testOrder := flag.Bool("test", false, "place + cancel a test order to trigger notifications")
	symbol := flag.String("symbol", "BTC-USDT", "symbol for test order")
	flag.Parse()

	var opts []btsemm.Option
	if *testnet {
		opts = append(opts, btsemm.WithTestnet())
		fmt.Println("=== TESTNET ===")
	}
	c := btsemm.NewFromEnv(opts...)

	ws, err := c.ConnectPrivateWS(
		btsemm.WithWSHandler(func(msg btsemm.WSMessage) {
			raw, _ := json.MarshalIndent(json.RawMessage(msg.Data), "  ", "  ")
			fmt.Printf("[%s] %s\n", msg.Topic, string(raw))
		}),
		btsemm.WithWSErrorHandler(func(err error) {
			fmt.Printf("WS ERROR: %v\n", err)
		}),
	)
	if err != nil {
		fmt.Printf("connect failed: %v\n", err)
		os.Exit(1)
	}
	defer ws.Close()

	fmt.Println("Subscribing to notificationApiV2...")
	if err := ws.Subscribe("notificationApiV2"); err != nil {
		fmt.Printf("subscribe failed: %v\n", err)
		os.Exit(1)
	}

	if *testOrder {
		fmt.Printf("Placing test order on %s...\n", *symbol)
		orders, err := c.CreateOrder(&btsemm.CreateOrderRequest{
			Symbol:    *symbol,
			Side:      btsemm.SideBuy,
			Type:      btsemm.OrderTypeLimit,
			Price:     50000,
			Size:      0.00001,
			PostOnly:  true,
			ClOrderID: fmt.Sprintf("wstest-%d", time.Now().UnixMilli()),
		})
		if err != nil {
			fmt.Printf("place failed: %v\n", err)
		} else if len(orders) > 0 {
			fmt.Printf("Placed %s, waiting 3s then cancelling...\n", orders[0].OrderID)
			time.Sleep(3 * time.Second)
			c.CancelOrder(&btsemm.CancelOrderRequest{
				Symbol:    *symbol,
				ClOrderID: orders[0].ClOrderID,
			})
			fmt.Println("Cancelled. Watching for events...")
		}
	}

	fmt.Println("Listening... (Ctrl+C to stop)")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	select {
	case <-sig:
		fmt.Println("\nStopped.")
	case <-ws.Done():
		fmt.Println("WS disconnected.")
	}
}
