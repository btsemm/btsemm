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

	client := btsemm.NewFromEnv()

	mp, err := client.WatchMidPrice(symbol)
	if err != nil {
		fmt.Printf("failed: %v\n", err)
		os.Exit(1)
	}
	defer mp.Stop()

	fmt.Printf("Waiting for %s price...\n", symbol)
	<-mp.Ready()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var prev float64
	for {
		select {
		case <-sig:
			fmt.Println("\nStopped.")
			return
		case <-ticker.C:
			mid, bid, ask, spread := mp.Snapshot()
			arrow := " "
			if prev > 0 {
				if mid > prev {
					arrow = "▲"
				} else if mid < prev {
					arrow = "▼"
				}
			}
			prev = mid
			fmt.Printf("\r\033[K[%s] %s %s %.2f  bid=%.2f  ask=%.2f  spread=%.2f",
				time.Now().Format("15:04:05"), symbol, arrow, mid, bid, ask, spread)
		}
	}
}
