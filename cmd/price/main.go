package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
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

	var mu sync.Mutex
	var lastMid, prevMid float64
	var lastTrade float64
	var bid, ask string

	render := func(ts string) {
		arrow := " "
		if prevMid > 0 {
			if lastMid > prevMid {
				arrow = "▲"
			} else if lastMid < prevMid {
				arrow = "▼"
			}
		}
		fmt.Printf("\r\033[K[%s] %s %s mid=%.2f  bid=%s  ask=%s  spread=%.1f  last=%.2f",
			ts, symbol, arrow, lastMid, bid, ask, lastMid*2-lastMid, lastTrade)
	}

	// OSS websocket for L1 orderbook (mid-price)
	ossWS, err := c.ConnectOrderbookWS(
		btsemm.WithWSHandler(func(msg btsemm.WSMessage) {
			if !strings.HasPrefix(msg.Topic, "snapshotL1:") {
				return
			}
			snap, err := msg.ParseSnapshotL1()
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if len(snap.Bids) > 0 && len(snap.Bids[0]) >= 2 {
				bid = snap.Bids[0][0]
			}
			if len(snap.Asks) > 0 && len(snap.Asks[0]) >= 2 {
				ask = snap.Asks[0][0]
			}
			b, _ := strconv.ParseFloat(bid, 64)
			a, _ := strconv.ParseFloat(ask, 64)
			if b > 0 && a > 0 {
				prevMid = lastMid
				lastMid = (b + a) / 2
			}
			ts := time.UnixMilli(snap.Timestamp).Format("15:04:05")
			spread := a - b
			fmt.Printf("\r\033[K[%s] %s  mid=%.2f  bid=%s  ask=%s  spread=%.2f  last=%.2f",
				ts, symbol, lastMid, bid, ask, spread, lastTrade)
		}),
		btsemm.WithWSErrorHandler(func(err error) {
			fmt.Printf("\noss error: %v\n", err)
		}),
	)
	if err != nil {
		fmt.Printf("oss connect failed: %v\n", err)
		os.Exit(1)
	}
	defer ossWS.Close()

	// Regular websocket for trades (last traded price)
	tradeWS, err := c.ConnectPublicWS(
		btsemm.WithWSHandler(func(msg btsemm.WSMessage) {
			if !strings.HasPrefix(msg.Topic, "tradeHistoryApi:") {
				return
			}
			trades, err := msg.ParseTrades()
			if err != nil || len(trades) == 0 {
				return
			}
			mu.Lock()
			lastTrade = trades[0].Price
			mu.Unlock()
			_ = render
		}),
		btsemm.WithWSErrorHandler(func(err error) {
			fmt.Printf("\nws error: %v\n", err)
		}),
	)
	if err != nil {
		fmt.Printf("ws connect failed: %v\n", err)
		os.Exit(1)
	}
	defer tradeWS.Close()

	if err := ossWS.Subscribe("snapshotL1:" + symbol); err != nil {
		fmt.Printf("subscribe failed: %v\n", err)
		os.Exit(1)
	}
	if err := tradeWS.Subscribe("tradeHistoryApi:" + symbol); err != nil {
		fmt.Printf("subscribe failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Watching %s (Ctrl+C to stop)\n", symbol)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	select {
	case <-sig:
		fmt.Println("\nStopped.")
	case <-ossWS.Done():
		fmt.Println("\nOSS connection closed.")
	case <-tradeWS.Done():
		fmt.Println("\nTrade connection closed.")
	}
}
