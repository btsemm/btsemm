package main

import (
	"fmt"
	"os"
	"strings"

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
	c := btsemm.NewFromEnv()
	ok := true

	// Step 1: Test auth
	fmt.Print("[1] Testing API keys... ")
	if err := c.TestAuth(); err != nil {
		fmt.Printf("FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("OK")

	// Step 2: Check key capabilities
	fmt.Print("[2] Checking key capabilities... ")
	caps, err := c.GetKeyCapabilities()
	if err != nil {
		fmt.Printf("FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("OK (read=%v trade=%v)\n", caps.Read, caps.Trade)
	if !caps.Trade {
		fmt.Println("    Key does not have trade permission, cannot continue.")
		os.Exit(1)
	}

	// Step 3: Check USDT balance
	fmt.Print("[3] Checking USDT balance... ")
	bal, err := c.GetCurrencyBalance("USDT")
	if err != nil {
		fmt.Printf("FAIL: %v\n", err)
		os.Exit(1)
	}
	if bal == nil || bal.Available < 1 {
		fmt.Printf("FAIL: insufficient USDT (available=%.2f)\n", 0.0)
		if bal != nil {
			fmt.Printf("    available=%.2f\n", bal.Available)
		}
		os.Exit(1)
	}
	fmt.Printf("OK (available=%.2f)\n", bal.Available)

	// Step 4: Get current BTC price for reference
	fmt.Print("[4] Fetching BTC-USDT price... ")
	prices, err := c.GetPrice("BTC-USDT")
	if err != nil {
		fmt.Printf("FAIL: %v\n", err)
		os.Exit(1)
	}
	if len(prices) == 0 {
		fmt.Println("FAIL: no price data")
		os.Exit(1)
	}
	fmt.Printf("OK (last=%.2f)\n", prices[0].LastPrice)

	// Step 5: Place a buy limit order at $45,000 (well below market)
	fmt.Print("[5] Placing BUY LIMIT 0.0004 BTC @ 45000... ")
	orders, err := c.CreateOrder(&btsemm.CreateOrderRequest{
		Symbol: "BTC-USDT",
		Side:   btsemm.SideBuy,
		Type:   btsemm.OrderTypeLimit,
		Price:  45000,
		Size:   0.0004,
	})
	if err != nil {
		fmt.Printf("FAIL: %v\n", err)
		os.Exit(1)
	}
	if len(orders) == 0 || orders[0].Status != btsemm.OrderStatusInserted {
		fmt.Printf("FAIL: unexpected response\n")
		os.Exit(1)
	}
	ourOrderID := orders[0].OrderID
	fmt.Printf("OK (id=%s status=%s)\n", ourOrderID, orders[0].Status)

	// Step 6: List open orders and verify ours is there
	fmt.Print("[6] Listing open orders... ")
	openOrders, err := c.GetOpenOrders("")
	if err != nil {
		fmt.Printf("FAIL: %v\n", err)
		ok = false
	} else {
		found := false
		for _, o := range openOrders {
			tag := "   "
			if o.OrderID == ourOrderID {
				tag = ">> "
				found = true
			}
			fmt.Printf("\n    %s%-36s %s %-4s price=%.2f size=%.4f",
				tag, o.OrderID, o.Symbol, o.Side, o.Price, o.OriginalOrderBaseSize)
		}
		if !found {
			fmt.Printf("\n    WARNING: our order not found in open orders")
			ok = false
		}
		fmt.Printf("\n    OK (%d total, ours %s)\n", len(openOrders), map[bool]string{true: "found", false: "NOT found"}[found])
	}

	// Step 7: Cancel only our order
	fmt.Printf("[7] Cancelling our order %s... ", ourOrderID)
	cancelled, err := c.CancelOrder(&btsemm.CancelOrderRequest{
		Symbol:  "BTC-USDT",
		OrderID: ourOrderID,
	})
	if err != nil {
		fmt.Printf("FAIL: %v\n", err)
		ok = false
	} else if len(cancelled) > 0 && cancelled[0].Status == btsemm.OrderStatusCancelled {
		fmt.Printf("OK (status=%s)\n", cancelled[0].Status)
	} else {
		fmt.Println("FAIL: unexpected cancel response")
		ok = false
	}

	// Step 8: Verify it's gone but other orders remain
	fmt.Print("[8] Verifying order list after cancel... ")
	remaining, err := c.GetOpenOrders("")
	if err != nil {
		fmt.Printf("FAIL: %v\n", err)
		ok = false
	} else {
		ourGone := true
		for _, o := range remaining {
			if o.OrderID == ourOrderID {
				ourGone = false
			}
		}
		if !ourGone {
			fmt.Println("FAIL: our order still present")
			ok = false
		} else {
			fmt.Printf("OK (%d orders remaining, ours removed)\n", len(remaining))
		}
	}

	// Summary
	fmt.Println()
	if ok {
		fmt.Println("All checks passed.")
	} else {
		fmt.Println("Some checks failed.")
		os.Exit(1)
	}
}
