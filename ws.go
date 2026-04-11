package btsemm

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSMessage is a raw message received from the WebSocket.
type WSMessage struct {
	Topic string          `json:"topic"`
	Data  json.RawMessage `json:"data"`
}

// WSTrade represents a trade from the tradeHistoryApi stream.
type WSTrade struct {
	Symbol    string  `json:"symbol"`
	Side      string  `json:"side"`
	Size      float64 `json:"size"`
	Price     float64 `json:"price"`
	TradeID   int64   `json:"tradeId"`
	Timestamp int64   `json:"timestamp"`
}

// WSSnapshotL1 represents an L1 orderbook snapshot.
type WSSnapshotL1 struct {
	Bids      [][]string `json:"bids"`
	Asks      [][]string `json:"asks"`
	Symbol    string     `json:"symbol"`
	Timestamp int64      `json:"timestamp"`
}

// WSOrderbookUpdate represents an orderbook delta/snapshot from the OSS stream.
type WSOrderbookUpdate struct {
	Bids       [][]string `json:"bids"`
	Asks       [][]string `json:"asks"`
	SeqNum     int64      `json:"seqNum"`
	PrevSeqNum int64      `json:"prevSeqNum"`
	Type       string     `json:"type"`
	Symbol     string     `json:"symbol"`
	Timestamp  int64      `json:"timestamp"`
}

// WSConn is a managed WebSocket connection to BTSE.
type WSConn struct {
	conn    *websocket.Conn
	mu      sync.Mutex
	done    chan struct{}
	handler func(WSMessage)
	onError func(error)
}

// wsOp is the subscribe/unsubscribe message format.
type wsOp struct {
	Op   string   `json:"op"`
	Args []string `json:"args"`
}

// WSOption configures the WebSocket connection.
type WSOption func(*wsConfig)

type wsConfig struct {
	url     string
	handler func(WSMessage)
	onError func(error)
}

// WithWSHandler sets the message handler.
func WithWSHandler(h func(WSMessage)) WSOption {
	return func(c *wsConfig) { c.handler = h }
}

// WithWSErrorHandler sets the error handler.
func WithWSErrorHandler(h func(error)) WSOption {
	return func(c *wsConfig) { c.onError = h }
}

// ConnectPublicWS connects to the public WebSocket endpoint.
func (c *Client) ConnectPublicWS(opts ...WSOption) (*WSConn, error) {
	cfg := &wsConfig{url: ProdWSURL}
	if c.baseURL == TestnetBaseURL {
		cfg.url = TestnetWSURL
	}
	for _, o := range opts {
		o(cfg)
	}
	return dialWS(cfg)
}

// ConnectOrderbookWS connects to the orderbook (OSS) WebSocket endpoint.
func (c *Client) ConnectOrderbookWS(opts ...WSOption) (*WSConn, error) {
	cfg := &wsConfig{url: ProdOSSWSURL}
	if c.baseURL == TestnetBaseURL {
		cfg.url = TestnetOSSWSURL
	}
	for _, o := range opts {
		o(cfg)
	}
	return dialWS(cfg)
}

// ConnectPrivateWS connects and authenticates to the private WebSocket endpoint.
func (c *Client) ConnectPrivateWS(opts ...WSOption) (*WSConn, error) {
	cfg := &wsConfig{url: ProdWSURL}
	if c.baseURL == TestnetBaseURL {
		cfg.url = TestnetWSURL
	}
	for _, o := range opts {
		o(cfg)
	}

	ws, err := dialWS(cfg)
	if err != nil {
		return nil, err
	}

	// Authenticate
	nonce := strconv.FormatInt(time.Now().UnixMilli(), 10)
	payload := "/ws/spot" + nonce
	mac := hmac.New(sha512.New384, []byte(c.apiSecret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	authMsg := wsOp{
		Op:   "authKeyExpires",
		Args: []string{c.apiKey, nonce, sig},
	}

	ws.mu.Lock()
	err = ws.conn.WriteJSON(authMsg)
	ws.mu.Unlock()
	if err != nil {
		ws.Close()
		return nil, fmt.Errorf("btse: ws auth failed: %w", err)
	}

	return ws, nil
}

func dialWS(cfg *wsConfig) (*WSConn, error) {
	conn, _, err := websocket.DefaultDialer.Dial(cfg.url, nil)
	if err != nil {
		return nil, fmt.Errorf("btse: ws connect failed: %w", err)
	}

	ws := &WSConn{
		conn:    conn,
		done:    make(chan struct{}),
		handler: cfg.handler,
		onError: cfg.onError,
	}

	// Start read loop
	go ws.readLoop()

	// Start ping keepalive
	go ws.pingLoop()

	return ws, nil
}

// Subscribe subscribes to one or more topics.
func (ws *WSConn) Subscribe(topics ...string) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.conn.WriteJSON(wsOp{Op: "subscribe", Args: topics})
}

// Unsubscribe unsubscribes from one or more topics.
func (ws *WSConn) Unsubscribe(topics ...string) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.conn.WriteJSON(wsOp{Op: "unsubscribe", Args: topics})
}

// Close shuts down the WebSocket connection.
func (ws *WSConn) Close() error {
	select {
	case <-ws.done:
		return nil
	default:
		close(ws.done)
	}
	return ws.conn.Close()
}

// Done returns a channel that is closed when the connection is closed.
func (ws *WSConn) Done() <-chan struct{} {
	return ws.done
}

func (ws *WSConn) readLoop() {
	defer func() {
		select {
		case <-ws.done:
		default:
			close(ws.done)
		}
	}()

	for {
		_, data, err := ws.conn.ReadMessage()
		if err != nil {
			if ws.onError != nil {
				ws.onError(err)
			}
			return
		}

		// Ignore pong responses
		if string(data) == "pong" {
			continue
		}

		if ws.handler == nil {
			continue
		}

		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			if ws.onError != nil {
				ws.onError(fmt.Errorf("btse: ws decode: %w", err))
			}
			continue
		}

		if msg.Topic != "" {
			ws.handler(msg)
		}
	}
}

func (ws *WSConn) pingLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ws.done:
			return
		case <-ticker.C:
			ws.mu.Lock()
			err := ws.conn.WriteMessage(websocket.TextMessage, []byte("ping"))
			ws.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}
