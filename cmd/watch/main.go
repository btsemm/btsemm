package main

import (
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
		line = strings.TrimPrefix(line, "export ")
		if k, v, ok := strings.Cut(line, "="); ok {
			os.Setenv(k, strings.Trim(v, `"`))
		}
	}
}

func main() {
	symbol := "BTC-USDT"
	if len(os.Args) > 1 {
		symbol = os.Args[1]
	}

	c := btsemm.NewFromEnv()

	fmt.Printf("Watching %s (Ctrl+C to stop)\n\n", symbol)

	ws, err := c.ConnectPublicWS(
		btsemm.WithWSHandler(func(msg btsemm.WSMessage) {
			if strings.HasPrefix(msg.Topic, "tradeHistoryApi:") {
				trades, err := msg.ParseTrades()
				if err != nil {
					fmt.Printf("parse error: %v\n", err)
					return
				}
				for _, t := range trades {
					ts := time.UnixMilli(t.Timestamp).Format("15:04:05.000")
					arrow := "↑"
					if t.Side == "SELL" {
						arrow = "↓"
					}
					fmt.Printf("[%s] %s %s  price=%.2f  size=%.6f\n",
						ts, arrow, t.Side, t.Price, t.Size)
				}
			} else if strings.HasPrefix(msg.Topic, "snapshotL1:") {
				snap, err := msg.ParseSnapshotL1()
				if err != nil {
					fmt.Printf("parse error: %v\n", err)
					return
				}
				bid, ask := "—", "—"
				bidSz, askSz := "—", "—"
				if len(snap.Bids) > 0 && len(snap.Bids[0]) >= 2 {
					bid = snap.Bids[0][0]
					bidSz = snap.Bids[0][1]
				}
				if len(snap.Asks) > 0 && len(snap.Asks[0]) >= 2 {
					ask = snap.Asks[0][0]
					askSz = snap.Asks[0][1]
				}
				ts := time.UnixMilli(snap.Timestamp).Format("15:04:05.000")
				fmt.Printf("[%s] BBO  bid=%s(%s)  ask=%s(%s)\n",
					ts, bid, bidSz, ask, askSz)
			}
		}),
		btsemm.WithWSErrorHandler(func(err error) {
			fmt.Printf("ws error: %v\n", err)
		}),
	)
	if err != nil {
		fmt.Printf("connect failed: %v\n", err)
		os.Exit(1)
	}
	defer ws.Close()

	// Subscribe to trades and best bid/offer
	if err := ws.Subscribe(
		"tradeHistoryApi:"+symbol,
		"snapshotL1:"+symbol,
	); err != nil {
		fmt.Printf("subscribe failed: %v\n", err)
		os.Exit(1)
	}

	// Wait for Ctrl+C
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	select {
	case <-sig:
		fmt.Println("\nStopping...")
	case <-ws.Done():
		fmt.Println("Connection closed")
	}
}
