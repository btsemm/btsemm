package main

import (
	"fmt"
	"os"
	"strings"

	"codeberg.org/btsemm/btsemm"
)

func main() {
	data, _ := os.ReadFile(".env")
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "export ")
		if k, v, ok := strings.Cut(line, "="); ok {
			os.Setenv(k, strings.Trim(v, `"`))
		}
	}

	c := btsemm.NewFromEnv()

	orders, err := c.GetOpenOrders("")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Printf("cancelling %d open orders...\n", len(orders))

	for _, o := range orders {
		cancelled, err := c.CancelOrder(&btsemm.CancelOrderRequest{
			Symbol:  o.Symbol,
			OrderID: o.OrderID,
		})
		if err != nil {
			fmt.Printf("  FAIL %s: %v\n", o.OrderID, err)
		} else if len(cancelled) > 0 {
			fmt.Printf("  OK   %s status=%s\n", cancelled[0].OrderID, cancelled[0].Status)
		}
	}
}
