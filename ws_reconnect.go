package btsemm

import (
	"log"
	"sync"
	"time"
)

// ReconnectingWSConn wraps a WSConn with automatic reconnection logic.
// It monitors the connection and re-dials on disconnect, re-subscribing
// to all previously recorded topics.
type ReconnectingWSConn struct {
	mu          sync.Mutex
	ws          *WSConn
	factory     func() (*WSConn, error)
	topics      []string
	done        chan struct{}
	handler     func(WSMessage)
	onError     func(error)
	onReconnect func()
}

// ReconnectOption configures the ReconnectingWSConn.
type ReconnectOption func(*ReconnectingWSConn)

// WithOnReconnect sets a callback that fires after each successful reconnection.
func WithOnReconnect(fn func()) ReconnectOption {
	return func(r *ReconnectingWSConn) { r.onReconnect = fn }
}

// newReconnecting creates a ReconnectingWSConn wrapping an initial connection.
func newReconnecting(ws *WSConn, factory func() (*WSConn, error), opts ...ReconnectOption) *ReconnectingWSConn {
	r := &ReconnectingWSConn{
		ws:      ws,
		factory: factory,
		done:    make(chan struct{}),
		handler: ws.handler,
		onError: ws.onError,
	}
	for _, o := range opts {
		o(r)
	}
	go r.monitor()
	return r
}

// Subscribe subscribes to topics and records them for re-subscription on reconnect.
func (r *ReconnectingWSConn) Subscribe(topics ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ws.Subscribe(topics...); err != nil {
		return err
	}
	r.topics = appendUnique(r.topics, topics...)
	return nil
}

// Unsubscribe unsubscribes from topics and removes them from the recorded list.
func (r *ReconnectingWSConn) Unsubscribe(topics ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ws.Unsubscribe(topics...); err != nil {
		return err
	}
	r.topics = removeTopics(r.topics, topics...)
	return nil
}

// Close permanently shuts down the reconnecting connection.
func (r *ReconnectingWSConn) Close() error {
	select {
	case <-r.done:
		return nil
	default:
		close(r.done)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ws.Close()
}

// Done returns a channel that closes when the connection is permanently closed.
func (r *ReconnectingWSConn) Done() <-chan struct{} {
	return r.done
}

// Conn returns the current underlying WSConn.
func (r *ReconnectingWSConn) Conn() *WSConn {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ws
}

func (r *ReconnectingWSConn) monitor() {
	for {
		r.mu.Lock()
		ws := r.ws
		r.mu.Unlock()

		select {
		case <-r.done:
			return
		case <-ws.Done():
			// Connection died, attempt reconnect
		}

		r.reconnectLoop()
	}
}

func (r *ReconnectingWSConn) reconnectLoop() {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-r.done:
			return
		default:
		}

		log.Printf("btse: ws reconnecting in %v", backoff)
		select {
		case <-r.done:
			return
		case <-time.After(backoff):
		}

		ws, err := r.factory()
		if err != nil {
			log.Printf("btse: ws reconnect failed: %v", err)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		// Re-subscribe to recorded topics
		r.mu.Lock()
		topics := make([]string, len(r.topics))
		copy(topics, r.topics)
		r.mu.Unlock()

		if len(topics) > 0 {
			if err := ws.Subscribe(topics...); err != nil {
				log.Printf("btse: ws resubscribe failed: %v", err)
				ws.Close()
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}
		}

		r.mu.Lock()
		r.ws = ws
		r.mu.Unlock()

		log.Printf("btse: ws reconnected, resubscribed to %d topics", len(topics))

		if r.onReconnect != nil {
			r.onReconnect()
		}
		return
	}
}

// ConnectPublicWSReconnect connects to the public WS with auto-reconnect.
func (c *Client) ConnectPublicWSReconnect(opts ...WSOption) (*ReconnectingWSConn, error) {
	factory := func() (*WSConn, error) {
		return c.ConnectPublicWS(opts...)
	}
	ws, err := factory()
	if err != nil {
		return nil, err
	}
	return newReconnecting(ws, factory), nil
}

// ConnectOrderbookWSReconnect connects to the orderbook WS with auto-reconnect.
func (c *Client) ConnectOrderbookWSReconnect(opts ...WSOption) (*ReconnectingWSConn, error) {
	factory := func() (*WSConn, error) {
		return c.ConnectOrderbookWS(opts...)
	}
	ws, err := factory()
	if err != nil {
		return nil, err
	}
	return newReconnecting(ws, factory), nil
}

// ConnectPrivateWSReconnect connects to the private WS with auto-reconnect.
func (c *Client) ConnectPrivateWSReconnect(ropts []ReconnectOption, opts ...WSOption) (*ReconnectingWSConn, error) {
	factory := func() (*WSConn, error) {
		return c.ConnectPrivateWS(opts...)
	}
	ws, err := factory()
	if err != nil {
		return nil, err
	}
	return newReconnecting(ws, factory, ropts...), nil
}

func appendUnique(slice []string, items ...string) []string {
	set := make(map[string]bool, len(slice))
	for _, s := range slice {
		set[s] = true
	}
	for _, item := range items {
		if !set[item] {
			slice = append(slice, item)
			set[item] = true
		}
	}
	return slice
}

func removeTopics(slice []string, items ...string) []string {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[item] = true
	}
	result := slice[:0]
	for _, s := range slice {
		if !set[s] {
			result = append(result, s)
		}
	}
	return result
}
