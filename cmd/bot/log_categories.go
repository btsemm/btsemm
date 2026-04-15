// log_categories.go: prefix-based fan-out of log lines to per-category files.
//
// When -log-dir is set, every line written through the global logger is
// inspected here. Lines that match a category's prefix(es) get tee'd to that
// category's file in addition to flowing through the existing combined file
// and stdout. Multi-line state dumps (between `=== STATE DUMP ===` and
// `=== END STATE DUMP ===`) are routed in their entirety to state.log via a
// small in-writer state machine.
//
// The combined file always receives every line; the per-category files are
// strict subsets meant for targeted tail/grep workflows.

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// categoryWriter implements io.Writer. It buffers partial lines, splits on '\n',
// and routes each complete line to one or more category files based on prefix.
type categoryWriter struct {
	heartbeat io.Writer
	state     io.Writer
	orders    io.Writer
	fills     io.Writer
	midprice  io.Writer

	buf       []byte
	inDump    bool // we're inside a multi-line `=== STATE DUMP ===` block
	inGridDmp bool // we're inside a multi-line `=== GRID STATE ===` block
}

// newCategoryWriter creates dir if needed and opens one file per category.
// Returns the writer, a close function for deferred cleanup, and any error.
func newCategoryWriter(dir string) (io.Writer, func(), error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, func() {}, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	stamp := time.Now().Format("2006-01-02-15-04")

	open := func(name string) (*os.File, error) {
		path := filepath.Join(dir, fmt.Sprintf("%s-%s.log", name, stamp))
		return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	}

	heartbeatF, err := open("heartbeat")
	if err != nil {
		return nil, func() {}, err
	}
	stateF, err := open("state")
	if err != nil {
		heartbeatF.Close()
		return nil, func() {}, err
	}
	ordersF, err := open("orders")
	if err != nil {
		heartbeatF.Close()
		stateF.Close()
		return nil, func() {}, err
	}
	fillsF, err := open("fills")
	if err != nil {
		heartbeatF.Close()
		stateF.Close()
		ordersF.Close()
		return nil, func() {}, err
	}
	midpriceF, err := open("midprice")
	if err != nil {
		heartbeatF.Close()
		stateF.Close()
		ordersF.Close()
		fillsF.Close()
		return nil, func() {}, err
	}

	cw := &categoryWriter{
		heartbeat: heartbeatF,
		state:     stateF,
		orders:    ordersF,
		fills:     fillsF,
		midprice:  midpriceF,
	}
	closeFn := func() {
		heartbeatF.Close()
		stateF.Close()
		ordersF.Close()
		fillsF.Close()
		midpriceF.Close()
	}
	return cw, closeFn, nil
}

// Write splits the input on newlines and routes each complete line to the
// matching category file(s). Partial lines are buffered until the next call.
func (c *categoryWriter) Write(p []byte) (int, error) {
	c.buf = append(c.buf, p...)
	for {
		i := bytes.IndexByte(c.buf, '\n')
		if i < 0 {
			break
		}
		line := append([]byte(nil), c.buf[:i+1]...) // include the newline
		c.buf = c.buf[i+1:]
		c.routeLine(line)
	}
	return len(p), nil
}

// routeLine inspects a single line (incl. its trailing newline) and tee's it
// to the appropriate category file. The prefix used here is the substring
// that appears AFTER the standard log timestamp: the global log is configured
// with Ldate|Ltime|Lmicroseconds, producing lines like
// "2026/04/15 22:16:01.123456 engine: HEARTBEAT …". We locate the first space
// after the timestamp and inspect the body that follows.
func (c *categoryWriter) routeLine(line []byte) {
	body := bodyAfterTimestamp(line)

	// Multi-line dump tracking: once we see the open marker, route every
	// subsequent line to state.log until the close marker. This captures the
	// per-order and per-grid lines that don't carry a recognisable prefix.
	if c.inDump || c.inGridDmp {
		c.write(c.state, line)
		if bytes.Contains(body, []byte("=== END STATE DUMP ===")) {
			c.inDump = false
		}
		if bytes.Contains(body, []byte("=== END GRID STATE ===")) {
			c.inGridDmp = false
		}
		return
	}

	switch {
	case bytes.HasPrefix(body, []byte("engine: HEARTBEAT")):
		c.write(c.heartbeat, line)
	case bytes.Contains(body, []byte("=== STATE DUMP ===")):
		c.inDump = true
		c.write(c.state, line)
	case bytes.Contains(body, []byte("=== GRID STATE")):
		c.inGridDmp = true
		c.write(c.state, line)
	case bytes.HasPrefix(body, []byte("midprice:")):
		c.write(c.midprice, line)
	case bytes.HasPrefix(body, []byte("fills:")):
		c.write(c.fills, line)
	case isOrdersLine(body):
		c.write(c.orders, line)
	}
	// Lines that match no category are silently dropped from category files
	// (they still appear in the combined log via the MultiWriter chain).
}

// bodyAfterTimestamp returns the slice of line that follows the standard
// "YYYY/MM/DD HH:MM:SS.uuuuuu " prefix produced by log.Ldate|Ltime|Lmicroseconds.
// If the line is too short or doesn't match the expected shape, returns line.
func bodyAfterTimestamp(line []byte) []byte {
	const tsLen = len("2006/01/02 15:04:05.000000 ")
	if len(line) < tsLen {
		return line
	}
	return line[tsLen:]
}

// isOrdersLine returns true for log lines that represent an order-lifecycle
// event (place / cancel / amend / reconcile / grid fill).
func isOrdersLine(body []byte) bool {
	s := string(body)
	switch {
	case strings.HasPrefix(s, "engine: placing"),
		strings.HasPrefix(s, "engine: cancelling"),
		strings.HasPrefix(s, "engine: amending"),
		strings.HasPrefix(s, "engine: reconcile"),
		strings.HasPrefix(s, "engine: post-only"),
		strings.HasPrefix(s, "engine: place failed"),
		strings.HasPrefix(s, "engine: amend failed"),
		strings.HasPrefix(s, "engine: cancel failed"),
		strings.HasPrefix(s, "orders: reconcile"),
		strings.HasPrefix(s, "grid: ["),
		strings.HasPrefix(s, "grid: FILL"):
		return true
	}
	return false
}

// write is a small wrapper that ignores errors from per-category files —
// failure to write to a category file should never crash the bot or block
// the main log.
func (c *categoryWriter) write(w io.Writer, line []byte) {
	_, _ = w.Write(line)
}
