// web.go: view-only web dashboard for the trading bot.
//
// Serves a Bootstrap-based responsive UI on a configurable address (default
// 127.0.0.1:8082). The page auto-refreshes every 2 seconds via fetch().
// All data is read-only — no orders can be placed/cancelled from the UI.

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"codeberg.org/btsemm/btsemm"
)

// webServer holds references to the engine and strategy name so the HTTP
// handlers can build a full status snapshot.
type webServer struct {
	engine       *btsemm.Engine
	strategyName string
	version      string
}

// statusResponse is the JSON payload returned by GET /api/status.
type statusResponse struct {
	Symbol        string        `json:"symbol"`
	BaseCurrency  string        `json:"baseCurrency"`
	QuoteCurrency string        `json:"quoteCurrency"`
	Strategy      string        `json:"strategy"`
	Version       string        `json:"version"`
	Uptime        string        `json:"uptime"`
	Config        configStatus  `json:"config"`
	Market        marketStatus  `json:"market"`
	Position      posStatus     `json:"position"`
	Risk          riskStatus    `json:"risk"`
	Engine        engineStatus  `json:"engine"`
	Orders        ordersStatus  `json:"orders"`
	Grid          interface{}   `json:"grid,omitempty"`
}

type configStatus struct {
	Symbol            string `json:"symbol"`
	TickInterval      string `json:"tickInterval"`
	ReconcileInterval string `json:"reconcileInterval"`
	DeadManTimeout    int64  `json:"deadManTimeoutMs"`
	AmendBPS          float64 `json:"amendBPS"`
	Verbose           bool   `json:"verbose"`
	WatchdogInterval  string `json:"watchdogInterval"`
	MaxConsecErrors   int    `json:"maxConsecErrors"`
}

type marketStatus struct {
	Mid    float64 `json:"mid"`
	Bid    float64 `json:"bid"`
	Ask    float64 `json:"ask"`
	Spread float64 `json:"spread"`
}

type posStatus struct {
	BaseQty        float64 `json:"baseQty"`
	QuoteSpent     float64 `json:"quoteSpent"`
	AvgCost        float64 `json:"avgCost"`
	TotalFees      float64 `json:"totalFees"`
	FillCount      int     `json:"fillCount"`
	UpdatedAt      string  `json:"updatedAt"`
	InventoryValue float64 `json:"inventoryValue"` // baseQty * mid
	TotalPnL       float64 `json:"totalPnL"`       // baseQty * mid - quoteSpent - totalFees
}

type riskStatus struct {
	MaxPositionSize float64 `json:"maxPositionSize"`
	MaxLossUSDT     float64 `json:"maxLossUSDT"`
	MaxOpenOrders   int     `json:"maxOpenOrders"`
	Killed          bool    `json:"killed"`
	KillReason      string  `json:"killReason,omitempty"`
}

type engineStatus struct {
	TickCount      uint64 `json:"tickCount"`
	ConsecErrors   int    `json:"consecErrors"`
	LastDesiredLen int    `json:"lastDesiredLen"`
	LastFillTime   string `json:"lastFillTime"`
	LastPlaceTime  string `json:"lastPlaceTime"`
	LastFillAgo    string `json:"lastFillAgo"`
	LastPlaceAgo   string `json:"lastPlaceAgo"`
}

type ordersStatus struct {
	Buys  int           `json:"buys"`
	Sells int           `json:"sells"`
	Total int           `json:"total"`
	List  []orderEntry  `json:"list"`
}

type orderEntry struct {
	Side      string  `json:"side"`
	Price     float64 `json:"price"`
	Size      float64 `json:"size"`
	Status    int     `json:"status"`
	Age       string  `json:"age"`
	ClOrderID string  `json:"clOrderID"`
}

func (ws *webServer) buildStatus() statusResponse {
	cfg := ws.engine.EngineConfig()

	// Parse base/quote from symbol (e.g. "JUNO-USDT" → "JUNO", "USDT")
	base, quote := cfg.Symbol, ""
	if parts := strings.SplitN(cfg.Symbol, "-", 2); len(parts) == 2 {
		base, quote = parts[0], parts[1]
	}

	mid, bid, ask, spread := ws.engine.MidPriceSnapshot()
	pos := ws.engine.Position().Snapshot()
	rc := ws.engine.Risk().Config()
	buys, sells, total := ws.engine.Orders().OrderCounts()

	lastFillTime := ws.engine.LastFillTime()
	lastPlaceTime := ws.engine.Orders().LastPlaceTime()

	fmtTime := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format(time.RFC3339)
	}
	fmtAgo := func(t time.Time) string {
		if t.IsZero() {
			return "never"
		}
		return time.Since(t).Truncate(time.Second).String()
	}

	// Open orders sorted by side then price
	open := ws.engine.Orders().OpenOrders()
	sort.Slice(open, func(i, j int) bool {
		if open[i].Side != open[j].Side {
			return open[i].Side == btsemm.SideBuy
		}
		return open[i].Price < open[j].Price
	})
	orderList := make([]orderEntry, len(open))
	for i, o := range open {
		orderList[i] = orderEntry{
			Side:      string(o.Side),
			Price:     o.Price,
			Size:      o.Size,
			Status:    int(o.Status),
			Age:       time.Since(o.CreatedAt).Truncate(time.Second).String(),
			ClOrderID: o.ClOrderID,
		}
	}

	uptime := ""
	if t := ws.engine.StartedAt(); !t.IsZero() {
		uptime = time.Since(t).Truncate(time.Second).String()
	}

	resp := statusResponse{
		Symbol:        cfg.Symbol,
		BaseCurrency:  base,
		QuoteCurrency: quote,
		Strategy:      ws.strategyName,
		Version:       ws.version,
		Uptime:        uptime,
		Config: configStatus{
			Symbol:            cfg.Symbol,
			TickInterval:      cfg.TickInterval.String(),
			ReconcileInterval: cfg.ReconcileInterval.String(),
			DeadManTimeout:    cfg.DeadManTimeout,
			AmendBPS:          cfg.AmendBPS,
			Verbose:           cfg.Verbose,
			WatchdogInterval:  cfg.WatchdogInterval.String(),
			MaxConsecErrors:   cfg.MaxConsecErrors,
		},
		Market: marketStatus{Mid: mid, Bid: bid, Ask: ask, Spread: spread},
		Position: posStatus{
			BaseQty:        pos.BaseQty,
			QuoteSpent:     pos.QuoteSpent,
			AvgCost:        pos.AvgCost,
			TotalFees:      pos.TotalFees,
			FillCount:      pos.FillCount,
			UpdatedAt:      fmtTime(pos.UpdatedAt),
			InventoryValue: pos.BaseQty * mid,
			TotalPnL:       pos.BaseQty*mid - pos.QuoteSpent - pos.TotalFees,
		},
		Risk: riskStatus{
			MaxPositionSize: rc.MaxPositionSize,
			MaxLossUSDT:     rc.MaxLossUSDT,
			MaxOpenOrders:   rc.MaxOpenOrders,
			Killed:          ws.engine.Risk().IsKilled(),
			KillReason:      ws.engine.Risk().KillReason(),
		},
		Engine: engineStatus{
			TickCount:      ws.engine.TickCount(),
			ConsecErrors:   ws.engine.ConsecErrors(),
			LastDesiredLen: ws.engine.LastDesiredLen(),
			LastFillTime:   fmtTime(lastFillTime),
			LastPlaceTime:  fmtTime(lastPlaceTime),
			LastFillAgo:    fmtAgo(lastFillTime),
			LastPlaceAgo:   fmtAgo(lastPlaceTime),
		},
		Orders: ordersStatus{
			Buys:  buys,
			Sells: sells,
			Total: total,
			List:  orderList,
		},
	}

	// Strategy-specific status (grid, etc.)
	if sp, ok := ws.engine.StrategyRaw().(btsemm.StatusProvider); ok {
		resp.Grid = sp.Status()
	}

	return resp
}

func (ws *webServer) handleAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(ws.buildStatus())
}

func (ws *webServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, dashboardHTML)
}

// startWebServer launches the HTTP server in a goroutine. Non-blocking.
func startWebServer(addr string, engine *btsemm.Engine, strategyName, version string) {
	ws := &webServer{
		engine:       engine,
		strategyName: strategyName,
		version:      version,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", ws.handleIndex)
	mux.HandleFunc("/api/status", ws.handleAPI)

	go func() {
		log.Printf("web: dashboard listening on http://%s", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("web: server error: %v", err)
		}
	}()
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Bot Dashboard</title>
<link href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.3/dist/css/bootstrap.min.css" rel="stylesheet">
<style>
  body { background: #1a1a2e; color: #e0e0e0; font-family: 'Segoe UI', system-ui, sans-serif; }
  .card { background: #16213e; border: 1px solid #0f3460; }
  .card-header { background: #0f3460; color: #e94560; font-weight: 600; font-size: 0.85rem; text-transform: uppercase; letter-spacing: 0.5px; }
  .table { color: #e0e0e0; font-size: 0.82rem; margin-bottom: 0; }
  .table th { color: #a0a0c0; font-weight: 500; border-color: #0f3460; }
  .table td { border-color: #0f3460; }
  .table-hover tbody tr:hover { background-color: rgba(233,69,96,0.08); color: #fff; }
  .val { font-family: 'JetBrains Mono', 'Fira Code', monospace; color: #00d2ff; }
  .val-warn { color: #e94560; }
  .val-ok { color: #4ecca3; }
  .buy { color: #4ecca3; }
  .sell { color: #e94560; }
  .badge-buy { background-color: #4ecca3; color: #000; }
  .badge-sell { background-color: #e94560; color: #fff; }
  .status-bar { background: #0f3460; border-bottom: 2px solid #e94560; padding: 10px 0; margin-bottom: 16px; }
  .status-bar .container-fluid { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
  .status-bar .title { font-size: 1.1rem; font-weight: 700; color: #e94560; }
  .status-bar .meta { font-size: 0.78rem; color: #a0a0c0; }
  .status-bar .meta span { margin-right: 12px; }
  .refresh-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #4ecca3; margin-right: 4px; animation: pulse 2s infinite; }
  @keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: 0.3; } }
  .grid-table { max-height: 400px; overflow-y: auto; }
  .orders-table { max-height: 500px; overflow-y: auto; }
  .mid-marker { background: rgba(233,69,96,0.15) !important; }
  h6 { font-size: 0.75rem; color: #a0a0c0; text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 2px; }
  .big-val { font-size: 1.3rem; font-weight: 700; }
  @media (max-width: 576px) {
    .big-val { font-size: 1rem; }
    .card-header { font-size: 0.75rem; }
    .table { font-size: 0.72rem; }
  }
</style>
</head>
<body>

<div class="status-bar">
  <div class="container-fluid">
    <div>
      <span class="title" id="symbol">---</span>
      <span class="meta">
        <span>strategy: <b id="strategy">---</b></span>
        <span>v<span id="version">---</span></span>
      </span>
    </div>
    <div class="meta">
      <span class="refresh-dot"></span>
      <span>uptime: <b id="uptime">---</b></span>
      <span>tick: <b class="val" id="tickCount">0</b></span>
    </div>
  </div>
</div>

<div class="container-fluid">

<!-- Row 0: Profit banner -->
<div class="row g-2 mb-2">
  <div class="col-12">
    <div class="card">
      <div class="card-body py-2">
        <div class="row align-items-center">
          <div class="col-auto">
            <h6>Total PnL</h6>
            <div class="big-val" id="totalPnL" style="font-size:1.5rem">---</div>
          </div>
          <div class="col-auto">
            <h6>Grid Profit (realized)</h6>
            <div class="big-val val-ok" id="gridProfit" style="font-size:1.5rem">---</div>
          </div>
          <div class="col-auto">
            <h6>Completed Cycles</h6>
            <div class="big-val val" id="completedCycles">---</div>
          </div>
          <div class="col-auto">
            <h6>Avg / Cycle</h6>
            <div class="big-val val" id="profitPerCycle">---</div>
          </div>
          <div class="col-auto">
            <h6>Inventory Value</h6>
            <div class="big-val val" id="inventoryValue">---</div>
          </div>
          <div class="col-auto">
            <h6>Fees Paid</h6>
            <div class="big-val val" id="totalFees">---</div>
          </div>
        </div>
      </div>
    </div>
  </div>
</div>

<!-- Row 1: Market + Position + Risk + Engine -->
<div class="row g-2 mb-2">

  <div class="col-6 col-md-3">
    <div class="card h-100">
      <div class="card-header py-1">Market</div>
      <div class="card-body py-2">
        <h6>Mid</h6><div class="big-val val" id="mid">---</div>
        <div class="row mt-1">
          <div class="col-6"><h6>Bid</h6><span class="val buy" id="bid">---</span></div>
          <div class="col-6"><h6>Ask</h6><span class="val sell" id="ask">---</span></div>
        </div>
        <h6 class="mt-1">Spread</h6><span class="val" id="spread">---</span>
      </div>
    </div>
  </div>

  <div class="col-6 col-md-3">
    <div class="card h-100">
      <div class="card-header py-1">Position</div>
      <div class="card-body py-2">
        <h6 id="baseLabel">Base</h6>
        <div class="big-val val" id="baseQty">---</div>
        <h6 class="mt-1" id="baseValueLabel">Value</h6>
        <span class="val" id="baseValue">---</span>
        <div class="row mt-1">
          <div class="col-6"><h6>Avg Cost</h6><span class="val" id="avgCost">---</span></div>
          <div class="col-6"><h6>Fills</h6><span class="val" id="fillCount">---</span></div>
        </div>
        <h6 class="mt-1">Net Quote Flow</h6><span class="val" id="quoteSpent">---</span>
      </div>
    </div>
  </div>

  <div class="col-6 col-md-3">
    <div class="card h-100">
      <div class="card-header py-1">Risk</div>
      <div class="card-body py-2">
        <h6>Status</h6>
        <div class="big-val" id="riskStatus">---</div>
        <div class="row mt-1">
          <div class="col-6"><h6>Max Pos</h6><span class="val" id="maxPos">---</span></div>
          <div class="col-6"><h6>Max Loss</h6><span class="val" id="maxLoss">---</span></div>
        </div>
        <h6 class="mt-1">Max Orders</h6><span class="val" id="maxOrders">---</span>
      </div>
    </div>
  </div>

  <div class="col-6 col-md-3">
    <div class="card h-100">
      <div class="card-header py-1">Engine</div>
      <div class="card-body py-2">
        <h6>Open / Desired</h6>
        <div class="big-val"><span class="val" id="openOrders">---</span> / <span class="val" id="desiredOrders">---</span></div>
        <div class="row mt-1">
          <div class="col-6"><h6>Last Fill</h6><span class="val" id="lastFillAgo">---</span></div>
          <div class="col-6"><h6>Last Place</h6><span class="val" id="lastPlaceAgo">---</span></div>
        </div>
        <h6 class="mt-1">Consec Errors</h6><span class="val" id="consecErrors">0</span>
      </div>
    </div>
  </div>

</div>

<!-- Row 2: Settings -->
<div class="row g-2 mb-2">
  <div class="col-12">
    <div class="card">
      <div class="card-header py-1">Settings</div>
      <div class="card-body py-2">
        <div class="row" id="settingsRow" style="font-size:0.82rem">
        </div>
      </div>
    </div>
  </div>
</div>

<!-- Row 3: Orders + Grid -->
<div class="row g-2 mb-2">

  <div class="col-12 col-lg-6">
    <div class="card">
      <div class="card-header py-1">Open Orders <span class="badge bg-secondary" id="ordersBadge">0</span></div>
      <div class="card-body p-0">
        <div class="orders-table">
          <table class="table table-hover table-sm mb-0">
            <thead><tr><th>Side</th><th>Price</th><th>Size</th><th>Age</th></tr></thead>
            <tbody id="ordersBody"></tbody>
          </table>
        </div>
      </div>
    </div>
  </div>

  <div class="col-12 col-lg-6" id="gridCard" style="display:none">
    <div class="card">
      <div class="card-header py-1">Grid State <span class="badge bg-secondary" id="gridBadge">0</span>
        <span class="ms-2" style="font-size:0.75rem; color:#a0a0c0" id="gridMeta"></span>
      </div>
      <div class="card-body p-0">
        <div class="grid-table">
          <table class="table table-hover table-sm mb-0">
            <thead><tr><th>#</th><th>Side</th><th>Lower</th><th>Upper</th><th>Size</th></tr></thead>
            <tbody id="gridBody"></tbody>
          </table>
        </div>
      </div>
    </div>
  </div>

</div>

</div>

<script>
const $ = id => document.getElementById(id);
const fmt = (v, d=6) => v != null ? Number(v).toFixed(d) : '---';

function update(d) {
  $('symbol').textContent = d.symbol;
  $('strategy').textContent = d.strategy;
  $('version').textContent = d.version;
  $('uptime').textContent = d.uptime || '---';
  $('tickCount').textContent = d.engine.tickCount;

  // Market
  $('mid').textContent = fmt(d.market.mid);
  $('bid').textContent = fmt(d.market.bid);
  $('ask').textContent = fmt(d.market.ask);
  $('spread').textContent = fmt(d.market.spread);

  // Profit banner
  const pnl = d.position.totalPnL;
  const pnlEl = $('totalPnL');
  pnlEl.textContent = (pnl >= 0 ? '+' : '') + fmt(pnl, 4) + ' USDT';
  pnlEl.className = 'big-val ' + (pnl >= 0 ? 'val-ok' : 'val-warn');
  $('inventoryValue').textContent = fmt(d.position.inventoryValue, 4) + ' USDT';
  $('totalFees').textContent = fmt(d.position.totalFees, 4) + ' USDT';

  if (d.grid) {
    const gp = $('gridProfit');
    gp.textContent = (d.grid.totalProfitUSDT >= 0 ? '+' : '') + fmt(d.grid.totalProfitUSDT, 4) + ' USDT';
    gp.className = 'big-val ' + (d.grid.totalProfitUSDT >= 0 ? 'val-ok' : 'val-warn');
    $('completedCycles').textContent = d.grid.completedCycles;
    $('profitPerCycle').textContent = fmt(d.grid.profitPerCycle, 4) + ' USDT';
  }

  // Position — with currency labels
  const base = d.baseCurrency || 'BASE';
  const quote = d.quoteCurrency || 'USDT';
  $('baseLabel').textContent = base;
  $('baseQty').textContent = fmt(d.position.baseQty, 4) + ' ' + base;
  $('baseValueLabel').textContent = 'Value in ' + quote;
  $('baseValue').textContent = fmt(d.position.inventoryValue, 4) + ' ' + quote;
  $('avgCost').textContent = fmt(d.position.avgCost);
  $('fillCount').textContent = d.position.fillCount;
  $('quoteSpent').textContent = fmt(d.position.quoteSpent, 4) + ' ' + quote;

  // Risk
  const killed = d.risk.killed;
  const rs = $('riskStatus');
  rs.textContent = killed ? 'KILLED: ' + d.risk.killReason : 'OK';
  rs.className = 'big-val ' + (killed ? 'val-warn' : 'val-ok');
  $('maxPos').textContent = fmt(d.risk.maxPositionSize, 2);
  $('maxLoss').textContent = fmt(d.risk.maxLossUSDT, 2);
  $('maxOrders').textContent = d.risk.maxOpenOrders;

  // Engine
  $('openOrders').textContent = d.orders.total;
  $('desiredOrders').textContent = d.engine.lastDesiredLen;
  $('lastFillAgo').textContent = d.engine.lastFillAgo;
  $('lastPlaceAgo').textContent = d.engine.lastPlaceAgo;
  $('consecErrors').textContent = d.engine.consecErrors;
  $('consecErrors').className = d.engine.consecErrors > 0 ? 'val val-warn' : 'val';

  // Highlight if open != desired
  const oEl = $('openOrders');
  oEl.className = (d.orders.total !== d.engine.lastDesiredLen && d.engine.lastDesiredLen > 0) ? 'val val-warn' : 'val';

  // Settings
  const c = d.config;
  $('settingsRow').innerHTML = [
    ['Tick', c.tickInterval], ['Reconcile', c.reconcileInterval],
    ['Dead Man', c.deadManTimeoutMs + 'ms'], ['Amend BPS', c.amendBPS],
    ['Watchdog', c.watchdogInterval], ['Max Errors', c.maxConsecErrors],
    ['Verbose', c.verbose],
  ].map(([k,v]) => '<div class="col-auto me-3"><h6>' + k + '</h6><span class="val">' + v + '</span></div>').join('');

  // Orders table
  $('ordersBadge').textContent = d.orders.total + ' (' + d.orders.buys + 'B/' + d.orders.sells + 'S)';
  const ob = $('ordersBody');
  ob.innerHTML = d.orders.list.map(o => {
    const cls = o.side === 'BUY' ? 'buy' : 'sell';
    return '<tr><td><span class="badge badge-' + o.side.toLowerCase() + '">' + o.side +
      '</span></td><td class="val">' + fmt(o.price) + '</td><td class="val">' +
      fmt(o.size, 4) + '</td><td>' + o.age + '</td></tr>';
  }).join('');

  // Grid
  if (d.grid && d.grid.levels) {
    $('gridCard').style.display = '';
    $('gridBadge').textContent = d.grid.buyCount + 'B / ' + d.grid.sellCount + 'S';
    $('gridMeta').textContent = 'ratio=' + d.grid.ratio.toFixed(6) + '  perGrid=' + d.grid.sizePerGrid.toFixed(2) + ' USDT';
    const gb = $('gridBody');
    gb.innerHTML = d.grid.levels.map(l => {
      const cls = l.side === 'BUY' ? 'buy' : 'sell';
      const isMid = d.grid.lastMid >= l.lower && d.grid.lastMid < l.upper;
      return '<tr class="' + (isMid ? 'mid-marker' : '') + '"><td>' + l.index +
        '</td><td class="' + cls + '">' + l.side +
        '</td><td class="val">' + fmt(l.lower) + '</td><td class="val">' + fmt(l.upper) +
        '</td><td class="val">' + fmt(l.size, 4) + '</td></tr>';
    }).join('');
  }
}

async function poll() {
  try {
    const r = await fetch('/api/status');
    if (r.ok) update(await r.json());
  } catch(e) {}
}
poll();
setInterval(poll, 2000);
</script>
</body>
</html>
`
