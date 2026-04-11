package btsemm

import "fmt"

// WalletBalance represents the balance of a single currency in the account.
type WalletBalance struct {
	Currency  string  `json:"currency"`
	Total     float64 `json:"total"`
	Available float64 `json:"available"`
}

// MarketSummary represents a single market's summary data.
type MarketSummary struct {
	Symbol            string  `json:"symbol"`
	Last              float64 `json:"last"`
	LowestAsk         float64 `json:"lowestAsk"`
	HighestBid        float64 `json:"highestBid"`
	PercentageChange  float64 `json:"percentageChange"`
	Volume            float64 `json:"volume"`
	High24Hr          float64 `json:"high24hr"`
	Low24Hr           float64 `json:"low24hr"`
	Base              string  `json:"base"`
	Quote             string  `json:"quote"`
	Active            bool    `json:"active"`
	Size              float64 `json:"size"`
	MinValidPrice     float64 `json:"minValidPrice"`
	MinPriceIncrement float64 `json:"minPriceIncrement"`
	MinOrderSize      float64 `json:"minOrderSize"`
	MinSizeIncrement  float64 `json:"minSizeIncrement"`
	MaxOrderSize      float64 `json:"maxOrderSize"`
}

// OHLCV represents a single candlestick bar.
type OHLCV struct {
	Timestamp int64   `json:"ts"`
	Open      float64 `json:"o"`
	High      float64 `json:"h"`
	Low       float64 `json:"l"`
	Close     float64 `json:"c"`
	Volume    float64 `json:"v"`
}

// Price represents a market's current price.
type Price struct {
	Symbol    string  `json:"symbol"`
	IndexPrice float64 `json:"indexPrice"`
	LastPrice  float64 `json:"lastPrice"`
	MarkPrice  float64 `json:"markPrice"`
}

// OrderbookEntry represents a single price level in the orderbook.
type OrderbookEntry struct {
	Price    string `json:"price"`
	Size     string `json:"size"`
}

// Orderbook represents the L2 orderbook.
type Orderbook struct {
	BuyQuote  []OrderbookEntry `json:"buyQuote"`
	SellQuote []OrderbookEntry `json:"sellQuote"`
	Symbol    string           `json:"symbol"`
	Timestamp int64            `json:"timestamp"`
}

// Trade represents a public trade fill.
type Trade struct {
	SerialID  int64   `json:"serialId"`
	Symbol    string  `json:"symbol"`
	Timestamp int64   `json:"timestamp"`
	Price     float64 `json:"price"`
	Amount    float64 `json:"amount"`
	Side      string  `json:"side"`
	Type      string  `json:"type"`
}

// ServerTime represents the server time response.
type ServerTime struct {
	ISO   string `json:"iso"`
	Epoch int64  `json:"epoch"`
}

// OrderSide represents order side.
type OrderSide string

const (
	SideBuy  OrderSide = "BUY"
	SideSell OrderSide = "SELL"
)

// OrderType represents order type.
type OrderType string

const (
	OrderTypeMarket OrderType = "MARKET"
	OrderTypeLimit  OrderType = "LIMIT"
)

// TimeInForce represents time-in-force for orders.
type TimeInForce string

const (
	TifGTC TimeInForce = "GTC"
	TifIOC TimeInForce = "IOC"
	TifFOK TimeInForce = "FOK"
)

// CreateOrderRequest is the payload for creating an order.
type CreateOrderRequest struct {
	Symbol    string      `json:"symbol"`
	Side      OrderSide   `json:"side"`
	Type      OrderType   `json:"type"`
	Size      float64     `json:"size"`
	Price     float64     `json:"price,omitempty"`
	TimeInForce TimeInForce `json:"time_in_force,omitempty"`
	PostOnly  bool        `json:"postOnly,omitempty"`
	ClOrderID string      `json:"clOrderID,omitempty"`
	TxType    string      `json:"txType,omitempty"`
}

// AmendType specifies what fields are being amended.
type AmendType string

const (
	AmendPrice AmendType = "PRICE"
	AmendSize  AmendType = "SIZE"
	AmendAll   AmendType = "ALL"
)

// AmendOrderRequest is the payload for amending an order.
type AmendOrderRequest struct {
	Symbol    string    `json:"symbol"`
	OrderID   string    `json:"orderID,omitempty"`
	ClOrderID string    `json:"clOrderID,omitempty"`
	Type      AmendType `json:"type"`
	Price     float64   `json:"price,omitempty"`
	Size      float64   `json:"size,omitempty"`
}

// CancelOrderRequest is the payload for cancelling an order.
type CancelOrderRequest struct {
	Symbol    string `json:"symbol"`
	OrderID   string `json:"orderID,omitempty"`
	ClOrderID string `json:"clOrderID,omitempty"`
}

// OrderStatus represents the numeric status of an order.
type OrderStatus int

const (
	OrderStatusInserted          OrderStatus = 2
	OrderStatusFullyTransacted   OrderStatus = 4
	OrderStatusPartiallyFilled   OrderStatus = 5
	OrderStatusCancelled         OrderStatus = 6
	OrderStatusRefunded          OrderStatus = 7
	OrderStatusInsufficientBal   OrderStatus = 8
	OrderStatusTriggerInserted   OrderStatus = 9
	OrderStatusTriggerActivated  OrderStatus = 10
	OrderStatusRejected          OrderStatus = 15
	OrderStatusNotFound          OrderStatus = 16
	OrderStatusRequestFailed     OrderStatus = 17
)

func (s OrderStatus) String() string {
	switch s {
	case OrderStatusInserted:
		return "INSERTED"
	case OrderStatusFullyTransacted:
		return "FULLY_TRANSACTED"
	case OrderStatusPartiallyFilled:
		return "PARTIALLY_FILLED"
	case OrderStatusCancelled:
		return "CANCELLED"
	case OrderStatusRefunded:
		return "REFUNDED"
	case OrderStatusInsufficientBal:
		return "INSUFFICIENT_BALANCE"
	case OrderStatusTriggerInserted:
		return "TRIGGER_INSERTED"
	case OrderStatusTriggerActivated:
		return "TRIGGER_ACTIVATED"
	case OrderStatusRejected:
		return "REJECTED"
	case OrderStatusNotFound:
		return "NOT_FOUND"
	case OrderStatusRequestFailed:
		return "REQUEST_FAILED"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", s)
	}
}

// Order represents an order response from the API.
type Order struct {
	OrderID                string      `json:"orderID"`
	ClOrderID              string      `json:"clOrderID"`
	Symbol                 string      `json:"symbol"`
	Side                   string      `json:"side"`
	OrderType              int         `json:"orderType"`
	Price                  float64     `json:"price"`
	Status                 OrderStatus `json:"status"`
	Timestamp              int64       `json:"timestamp"`
	TimeInForce            string      `json:"time_in_force"`
	TriggerPrice           float64     `json:"triggerPrice"`
	StopPrice              float64     `json:"stopPrice"`
	Trigger                bool        `json:"trigger"`
	Message                string      `json:"message"`
	PostOnly               bool        `json:"postOnly"`
	OriginalOrderBaseSize  float64     `json:"originalOrderBaseSize"`
	OriginalOrderQuoteSize float64     `json:"originalOrderQuoteSize"`
	CurrentOrderBaseSize   float64     `json:"currentOrderBaseSize"`
	CurrentOrderQuoteSize  float64     `json:"currentOrderQuoteSize"`
	RemainingOrderBaseSize float64     `json:"remainingOrderBaseSize"`
	RemainingOrderQuoteSize float64    `json:"remainingOrderQuoteSize"`
	FilledBaseSize         float64     `json:"filledBaseSize"`
	TotalFilledBaseSize    float64     `json:"totalFilledBaseSize"`
	AvgFilledPrice         float64     `json:"avgFilledPrice"`
	Stealth                float64     `json:"stealth"`
	Deviation              float64     `json:"deviation"`
}

// UserTrade represents a user's trade fill.
type UserTrade struct {
	TradeID   string  `json:"tradeId"`
	OrderID   string  `json:"orderId"`
	Symbol    string  `json:"symbol"`
	Side      string  `json:"side"`
	Price     float64 `json:"price"`
	Size      float64 `json:"size"`
	Fee       float64 `json:"fee"`
	FeeGross  float64 `json:"feeGross"`
	FeeCurrency string `json:"feeCurrency"`
	Timestamp int64   `json:"timestamp"`
}

// AccountFees represents the user's fee tier.
type AccountFees struct {
	MakerFee float64 `json:"makerFee"`
	TakerFee float64 `json:"takerFee"`
	Symbol   string  `json:"symbol"`
}

// CancelAllAfterRequest is the payload for the dead man's switch.
type CancelAllAfterRequest struct {
	Timeout int64 `json:"timeout"`
}
