package main

import (
	"fmt"
	"os"
	"strings"

	"codeberg.org/btsemm/btsemm"
)

func init() {
	data, _ := os.ReadFile("../../.env")
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
	summaries, _ := c.GetMarketSummary("")
	for _, s := range summaries {
		if strings.HasPrefix(s.Symbol, "BTC-") && s.Active {
			fmt.Printf("%s last=%.2f vol=%.2f\n", s.Symbol, s.Last, s.Volume)
		}
	}
}
