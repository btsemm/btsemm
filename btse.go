// Package btsemm provides a Go client for the BTSE Spot Exchange API v3.3.
package btsemm

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

const (
	ProdBaseURL    = "https://api.btse.com/spot"
	TestnetBaseURL = "https://testapi.btse.io/spot"

	ProdWSURL    = "wss://ws.btse.com/ws/spot"
	TestnetWSURL = "wss://testws.btse.io/ws/spot"

	ProdOSSWSURL    = "wss://ws.btse.com/ws/oss/spot"
	TestnetOSSWSURL = "wss://testws.btse.io/ws/oss/spot"

	apiPrefix = "/api/v3.3"
)

// Client is the BTSE spot API client.
type Client struct {
	baseURL    string
	apiKey     string
	apiSecret  string
	httpClient *http.Client
}

// Option configures the Client.
type Option func(*Client)

// WithHTTPClient sets a custom http.Client.
func WithHTTPClient(c *http.Client) Option {
	return func(cl *Client) { cl.httpClient = c }
}

// WithTestnet points the client at the BTSE testnet.
func WithTestnet() Option {
	return func(cl *Client) { cl.baseURL = TestnetBaseURL }
}

// NewFromEnv creates a client using BTSE_API_KEY and BTSE_API_SECRET
// environment variables.
func NewFromEnv(opts ...Option) *Client {
	return New(os.Getenv("BTSE_API_KEY"), os.Getenv("BTSE_API_SECRET"), opts...)
}

// New creates a new BTSE spot client. apiKey and apiSecret can be empty for
// public-only endpoints.
func New(apiKey, apiSecret string, opts ...Option) *Client {
	c := &Client{
		baseURL:    ProdBaseURL,
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		httpClient: newHTTPClient(),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// TestAuth verifies that the API key and secret are valid by making a
// lightweight authenticated request. Returns nil if credentials are working.
func (c *Client) TestAuth() error {
	_, err := c.GetWalletBalance()
	return err
}

// KeyCapabilities describes what permissions an API key has.
type KeyCapabilities struct {
	Valid bool // key and secret are accepted
	Read  bool // can query balances, fees, trade history
	Trade bool // can place/cancel orders
}

// GetKeyCapabilities probes the API to determine what permissions the key has.
// BTSE doesn't expose a permissions endpoint, so this makes lightweight calls
// and checks which succeed.
func (c *Client) GetKeyCapabilities() (*KeyCapabilities, error) {
	caps := &KeyCapabilities{}

	// Test Read permission via wallet balance
	_, err := c.GetWalletBalance()
	if err != nil {
		if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == 401 {
			return caps, nil // invalid key
		}
		if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == 403 {
			caps.Valid = true // key works but no read permission
		} else {
			return nil, err // network or other error
		}
	} else {
		caps.Valid = true
		caps.Read = true
	}

	// Test Trade permission via open orders query
	_, err = c.GetOpenOrders("")
	if err != nil {
		if apiErr, ok := err.(*APIError); ok && (apiErr.StatusCode == 403 || apiErr.StatusCode == 401) {
			// no trade permission
		} else if _, ok := err.(*APIError); !ok {
			return nil, err // network error
		}
	} else {
		caps.Trade = true
	}

	return caps, nil
}

// sign produces the HMAC-SHA384 signature for an authenticated request.
func (c *Client) sign(path string, nonce string, body []byte) string {
	payload := path + nonce + string(body)
	mac := hmac.New(sha512.New384, []byte(c.apiSecret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// doPublic performs an unauthenticated GET request.
func (c *Client) doPublic(endpoint string, params url.Values, out interface{}) error {
	u := c.baseURL + apiPrefix + endpoint
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("btse: build request: %w", err)
	}

	return c.execute(req, out)
}

// doAuth performs an authenticated request using the default v3.3 API prefix.
func (c *Client) doAuth(method, endpoint string, params url.Values, body interface{}, out interface{}) error {
	return c.doAuthRaw(method, apiPrefix+endpoint, params, body, out)
}

// doAuthRaw performs an authenticated request with a full path (including version prefix).
func (c *Client) doAuthRaw(method, path string, params url.Values, body interface{}, out interface{}) error {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("btse: marshal body: %w", err)
		}
	}

	req, err := http.NewRequest(method, u, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("btse: build request: %w", err)
	}

	nonce := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sig := c.sign(path, nonce, bodyBytes)

	req.Header.Set("request-api", c.apiKey)
	req.Header.Set("request-nonce", nonce)
	req.Header.Set("request-sign", sig)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.execute(req, out)
}

// execute sends the request and decodes the response.
func (c *Client) execute(req *http.Request, out interface{}) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("btse: request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("btse: read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := resp.Header.Get("Retry-After")
		return &RateLimitError{RetryAfter: retryAfter}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(data),
		}
	}

	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("btse: decode response: %w", err)
		}
	}
	return nil
}

// newHTTPClient creates an HTTP client hardened for long-running processes.
// Forces HTTP/1.1 to avoid stale HTTP/2 connection issues where Go silently
// writes into a dead TCP socket after Cloudflare/NAT kills the connection.
// TCP keepalives detect dead connections at the OS level before reuse.
func newHTTPClient() *http.Client {
	transport := &http.Transport{
		// Force HTTP/1.1 — disable HTTP/2 ALPN negotiation
		TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),

		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 15 * time.Second,
			}).DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			tc := conn.(*net.TCPConn)
			tc.SetKeepAlive(true)
			tc.SetKeepAlivePeriod(15 * time.Second)
			return conn, nil
		},

		IdleConnTimeout:       20 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		MaxIdleConnsPerHost:   1,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}
}

func (c *Client) IsEmptyAuthentication() bool {
	if c.apiKey == "" || c.apiSecret == "" {
		return true
	}
	return false
}
