package btsemm_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"codeberg.org/btsemm/btsemm"
)

func init() {
	// Load .env file if BTSE_API_KEY is not already set.
	if os.Getenv("BTSE_API_KEY") == "" {
		data, err := os.ReadFile(".env")
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "export ")
			if k, v, ok := strings.Cut(line, "="); ok {
				v = strings.Trim(v, `"`)
				os.Setenv(k, v)
			}
		}
	}
}

func newClient() *btsemm.Client {
	return btsemm.NewFromEnv()
}

func TestGetServerTime(t *testing.T) {
	c := newClient()
	st, err := c.GetServerTime()
	if err != nil {
		t.Fatalf("GetServerTime: %v", err)
	}
	if st.Epoch == 0 {
		t.Fatal("expected non-zero epoch")
	}
	t.Logf("server time: %s (epoch %d)", st.ISO, st.Epoch)
}

func TestGetMarketSummary(t *testing.T) {
	c := newClient()
	summaries, err := c.GetMarketSummary("")
	if err != nil {
		t.Fatalf("GetMarketSummary: %v", err)
	}
	if len(summaries) == 0 {
		t.Fatal("expected at least one market")
	}
	t.Logf("got %d markets, first: %s last=%.2f", len(summaries), summaries[0].Symbol, summaries[0].Last)
}

func TestGetMarketSummaryBTC(t *testing.T) {
	c := newClient()
	summaries, err := c.GetMarketSummary("BTC-USDT")
	if err != nil {
		t.Fatalf("GetMarketSummary BTC-USDT: %v", err)
	}
	if len(summaries) == 0 {
		t.Fatal("expected BTC-USDT market")
	}
	t.Logf("BTC-USDT: last=%.2f high=%.2f low=%.2f vol=%.2f",
		summaries[0].Last, summaries[0].High24Hr, summaries[0].Low24Hr, summaries[0].Volume)
}

func TestGetPrice(t *testing.T) {
	c := newClient()
	prices, err := c.GetPrice("BTC-USDT")
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}
	if len(prices) == 0 {
		t.Fatal("expected price data")
	}
	t.Logf("BTC-USDT price: last=%.2f index=%.2f", prices[0].LastPrice, prices[0].IndexPrice)
}

func TestGetOrderbook(t *testing.T) {
	c := newClient()
	ob, err := c.GetOrderbook("BTC-USDT", 0)
	if err != nil {
		t.Fatalf("GetOrderbook: %v", err)
	}
	if len(ob.BuyQuote) == 0 || len(ob.SellQuote) == 0 {
		t.Fatal("expected bids and asks")
	}
	t.Logf("BTC-USDT orderbook: %d bids, %d asks, best bid=%s, best ask=%s",
		len(ob.BuyQuote), len(ob.SellQuote), ob.BuyQuote[0].Price, ob.SellQuote[0].Price)
}

func TestGetTrades(t *testing.T) {
	c := newClient()
	trades, err := c.GetTrades("BTC-USDT", 5, 0, 0)
	if err != nil {
		t.Fatalf("GetTrades: %v", err)
	}
	if len(trades) == 0 {
		t.Fatal("expected trades")
	}
	t.Logf("got %d trades, latest: price=%.2f size=%.4f side=%s",
		len(trades), trades[0].Price, trades[0].Amount, trades[0].Side)
}

func TestGetFees(t *testing.T) {
	c := newClient()
	fees, err := c.GetFees("")
	if err != nil {
		t.Fatalf("GetFees: %v", err)
	}
	t.Logf("fees: %+v", fees)
}

func TestGetOpenOrders(t *testing.T) {
	c := newClient()
	orders, err := c.GetOpenOrders("")
	if err != nil {
		t.Fatalf("GetOpenOrders: %v", err)
	}
	t.Logf("open orders: %d", len(orders))
	for _, o := range orders {
		t.Logf("  id=%s symbol=%s side=%s price=%.2f size=%.4f remaining=%.4f status=%s",
			o.OrderID, o.Symbol, o.Side, o.Price, o.OriginalOrderBaseSize, o.RemainingOrderBaseSize, o.Status)
	}
}

func TestAuth(t *testing.T) {
	c := newClient()
	if err := c.TestAuth(); err != nil {
		t.Fatalf("TestAuth: keys are invalid: %v", err)
	}
	t.Log("API keys are valid")
}

func TestGetKeyCapabilities(t *testing.T) {
	c := newClient()
	caps, err := c.GetKeyCapabilities()
	if err != nil {
		t.Fatalf("GetKeyCapabilities: %v", err)
	}
	t.Logf("Key capabilities: valid=%v read=%v trade=%v", caps.Valid, caps.Read, caps.Trade)
}

func TestAuthBadKeys(t *testing.T) {
	c := btsemm.New("bad-key", "bad-secret")
	if err := c.TestAuth(); err == nil {
		t.Fatal("expected error with bad keys")
	} else {
		t.Logf("correctly rejected bad keys: %v", err)
	}
}

func TestBuyLimitOrderBTC(t *testing.T) {
	c := newClient()

	// Place a buy limit order: 20 USDT worth of BTC at 50000
	// size = 20 / 50000 = 0.0004 BTC
	orders, err := c.CreateOrder(&btsemm.CreateOrderRequest{
		Symbol: "BTC-USDT",
		Side:   btsemm.SideBuy,
		Type:   btsemm.OrderTypeLimit,
		Price:  50000,
		Size:   0.0004,
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if len(orders) == 0 {
		t.Fatal("expected order response")
	}
	o := orders[0]
	t.Logf("order placed: id=%s symbol=%s side=%s price=%.2f size=%.4f status=%s",
		o.OrderID, o.Symbol, o.Side, o.Price, o.OriginalOrderBaseSize, o.Status)

	// Clean up: cancel the order
	cancelled, err := c.CancelOrder(&btsemm.CancelOrderRequest{
		Symbol:  "BTC-USDT",
		OrderID: o.OrderID,
	})
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	if len(cancelled) > 0 {
		t.Logf("order cancelled: id=%s status=%s", cancelled[0].OrderID, cancelled[0].Status)
	}
}

func TestGetWalletBalance(t *testing.T) {
	c := newClient()
	balances, err := c.GetWalletBalance()
	if err != nil {
		t.Fatalf("GetWalletBalance: %v", err)
	}
	t.Logf("wallet has %d currencies", len(balances))
	for _, b := range balances {
		if b.Total > 0 {
			t.Logf("  %s: total=%.8f available=%.8f locked=%.8f",
				b.Currency, b.Total, b.Available, b.Total-b.Available)
		}
	}
}

func TestGetCurrencyBalance(t *testing.T) {
	c := newClient()
	b, err := c.GetCurrencyBalance("USDT")
	if err != nil {
		t.Fatalf("GetCurrencyBalance: %v", err)
	}
	if b == nil {
		t.Log("USDT: no balance found")
		return
	}
	t.Logf("USDT: total=%.8f available=%.8f locked=%.8f",
		b.Total, b.Available, b.Total-b.Available)
}

func TestMidPrice(t *testing.T) {
	c := newClient()
	mp, err := c.WatchMidPrice("BTC-USDT")
	if err != nil {
		t.Fatalf("WatchMidPrice: %v", err)
	}
	defer mp.Stop()

	// Wait for first price
	<-mp.Ready()

	// Sample a few times
	for i := 0; i < 5; i++ {
		mid, bid, ask, spread := mp.Snapshot()
		t.Logf("mid=%.2f  bid=%.2f  ask=%.2f  spread=%.2f", mid, bid, ask, spread)
		if mid == 0 {
			t.Fatal("expected non-zero mid price")
		}
	}

	// Also test individual accessors
	fmt.Printf("  Price()=%.2f  Bid()=%.2f  Ask()=%.2f  Spread()=%.2f\n",
		mp.Price(), mp.Bid(), mp.Ask(), mp.Spread())
}

// Verify .env is in .gitignore
func TestEnvNotTracked(t *testing.T) {
	out, err := exec.Command("git", "check-ignore", ".env").Output()
	if err != nil {
		t.Skip("not a git repo or git not available")
	}
	if !strings.Contains(string(out), ".env") {
		t.Fatal(".env is not in .gitignore — your keys could be committed!")
	}
}
