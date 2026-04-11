package btsemm

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// MidPrice tracks the live mid-price for a symbol via WebSocket.
type MidPrice struct {
	mu      sync.RWMutex
	mid     float64
	bid     float64
	ask     float64
	symbol  string
	ws      *WSConn
	ready   chan struct{}
	once    sync.Once
}

// WatchMidPrice starts a background goroutine that maintains the live mid-price
// for the given symbol. Call Price() at any time to get the current value.
// Call Stop() when done.
func (c *Client) WatchMidPrice(symbol string) (*MidPrice, error) {
	mp := &MidPrice{
		symbol: symbol,
		ready:  make(chan struct{}),
	}

	ws, err := c.ConnectOrderbookWS(
		WithWSHandler(func(msg WSMessage) {
			if !strings.HasPrefix(msg.Topic, "snapshotL1:") {
				return
			}
			snap, err := msg.ParseSnapshotL1()
			if err != nil {
				return
			}

			var b, a float64
			if len(snap.Bids) > 0 && len(snap.Bids[0]) >= 2 {
				b, _ = strconv.ParseFloat(snap.Bids[0][0], 64)
			}
			if len(snap.Asks) > 0 && len(snap.Asks[0]) >= 2 {
				a, _ = strconv.ParseFloat(snap.Asks[0][0], 64)
			}

			if b > 0 && a > 0 {
				mp.mu.Lock()
				mp.bid = b
				mp.ask = a
				mp.mid = (b + a) / 2
				mp.mu.Unlock()
				mp.once.Do(func() { close(mp.ready) })
			}
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("btse: midprice connect: %w", err)
	}
	mp.ws = ws

	if err := ws.Subscribe("snapshotL1:" + symbol); err != nil {
		ws.Close()
		return nil, fmt.Errorf("btse: midprice subscribe: %w", err)
	}

	return mp, nil
}

// Ready returns a channel that is closed once the first price is received.
func (mp *MidPrice) Ready() <-chan struct{} {
	return mp.ready
}

// Price returns the current mid-price.
func (mp *MidPrice) Price() float64 {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return mp.mid
}

// Bid returns the current best bid.
func (mp *MidPrice) Bid() float64 {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return mp.bid
}

// Ask returns the current best ask.
func (mp *MidPrice) Ask() float64 {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return mp.ask
}

// Spread returns the current bid-ask spread.
func (mp *MidPrice) Spread() float64 {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return mp.ask - mp.bid
}

// Snapshot returns all values at once.
func (mp *MidPrice) Snapshot() (mid, bid, ask, spread float64) {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return mp.mid, mp.bid, mp.ask, mp.ask - mp.bid
}

// Stop shuts down the background WebSocket.
func (mp *MidPrice) Stop() error {
	return mp.ws.Close()
}
