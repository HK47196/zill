// SPDX-License-Identifier: GPL-3.0-or-later

// Package ppssppdebug exposes PPSSPP's remote debugger as a JSONL bridge.
package ppssppdebug

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	BridgeFormat       = "zill-ppsspp-debugger-bridge"
	BridgeVersion      = 3
	Subprotocol        = "debugger.ppsspp.org"
	defaultTimeout     = 5 * time.Second
	defaultEventBuffer = 1024
	defaultInlineBytes = 4096
	maxMemoryBytes     = 65536
)

// Options configures the debugger bridge.
type Options struct {
	Host             string
	Port             int
	AllowRemote      bool
	AllowMemoryWrite bool
	Timeout          time.Duration
	ConnectTimeout   time.Duration
	EventBuffer      int
	MaxInlineBytes   int
}

// DefaultOptions returns the safe local-only bridge configuration.
func DefaultOptions() Options {
	return Options{Host: "127.0.0.1", Timeout: defaultTimeout, ConnectTimeout: defaultTimeout, EventBuffer: defaultEventBuffer, MaxInlineBytes: defaultInlineBytes}
}

func (o Options) Validate() error {
	if o.Port < 1 || o.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if o.Host == "" {
		return errors.New("host must not be empty")
	}
	if !o.AllowRemote && !isLoopback(o.Host) {
		return errors.New("non-loopback host requires allow-remote")
	}
	if o.Timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	if o.ConnectTimeout <= 0 {
		return errors.New("connect-timeout must be positive")
	}
	if o.EventBuffer < 1 {
		return errors.New("event-buffer must be positive")
	}
	if o.MaxInlineBytes < 1 {
		return errors.New("max-inline-bytes must be positive")
	}
	return nil
}

type bridgeError struct {
	message string
	details any
}

func (e *bridgeError) Error() string { return e.message }
func fail(s string, d ...any) error {
	var x any
	if len(d) > 0 {
		x = d[0]
	}
	return &bridgeError{s, x}
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
func url(o Options) string {
	h := o.Host
	if strings.Contains(h, ":") && !strings.HasPrefix(h, "[") {
		h = "[" + h + "]"
	}
	return fmt.Sprintf("ws://%s:%d/debugger", h, o.Port)
}

type event struct {
	sequence int
	message  map[string]any
}
type connection struct {
	ws                      *websocket.Conn
	ctx                     context.Context
	opts                    Options
	mu                      sync.Mutex
	cond                    *sync.Cond
	send                    sync.Mutex
	pending                 map[string]bool
	responses               map[string]map[string]any
	events                  []event
	sequence, dropped, next int
	failure                 string
	closing                 bool
}

func newConnection(ctx context.Context, ws *websocket.Conn, o Options) *connection {
	c := &connection{ws: ws, ctx: ctx, opts: o, pending: map[string]bool{}, responses: map[string]map[string]any{}, next: 1}
	c.cond = sync.NewCond(&c.mu)
	go c.read()
	return c
}
func (c *connection) read() {
	for {
		_, b, err := c.ws.Read(c.ctx)
		if err != nil {
			c.mu.Lock()
			if !c.closing {
				c.failure = "debugger connection closed: " + err.Error()
			}
			c.cond.Broadcast()
			c.mu.Unlock()
			return
		}
		var m map[string]any
		if json.Unmarshal(b, &m) != nil || asString(m["event"]) == "" {
			c.mu.Lock()
			c.failure = "PPSSPP sent an invalid message"
			c.cond.Broadcast()
			c.mu.Unlock()
			return
		}
		c.mu.Lock()
		c.sequence++
		ticket := asString(m["ticket"])
		if ticket != "" && c.pending[ticket] {
			c.responses[ticket] = m
		} else {
			if len(c.events) >= c.opts.EventBuffer {
				c.events = c.events[1:]
				c.dropped++
			}
			c.events = append(c.events, event{c.sequence, m})
		}
		c.cond.Broadcast()
		c.mu.Unlock()
	}
}
func asString(v any) string { s, _ := v.(string); return s }
func public(m map[string]any) map[string]any {
	r := make(map[string]any, len(m))
	for k, v := range m {
		if k != "ticket" {
			r[k] = v
		}
	}
	return r
}
func (c *connection) broken() string { c.mu.Lock(); defer c.mu.Unlock(); return c.failure }
func (c *connection) close() {
	c.mu.Lock()
	c.closing = true
	c.cond.Broadcast()
	c.mu.Unlock()
	_ = c.ws.CloseNow()
}
func (c *connection) ticket() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := fmt.Sprintf("zill-bridge-%d", c.next)
	c.next++
	c.pending[t] = true
	return t
}
func (c *connection) sendMessage(ctx context.Context, m map[string]any) error {
	b, _ := json.Marshal(m)
	c.send.Lock()
	defer c.send.Unlock()
	if err := c.ws.Write(ctx, websocket.MessageText, b); err != nil {
		c.mu.Lock()
		c.failure = "failed to send debugger message: " + err.Error()
		c.cond.Broadcast()
		c.mu.Unlock()
		return fail(c.failure)
	}
	return nil
}
func responseError(m map[string]any) error {
	if asString(m["event"]) == "error" {
		message, ok := m["message"].(string)
		if !ok || message == "" {
			message = "PPSSPP returned an error"
		}
		return fail(message, map[string]any{"ppsspp": public(m)})
	}
	return nil
}
func (c *connection) request(event string, params map[string]any, timeout time.Duration) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(c.ctx, timeout)
	defer cancel()
	t := c.ticket()
	m := map[string]any{"event": event, "ticket": t}
	for k, v := range params {
		m[k] = v
	}
	if err := c.sendMessage(ctx, m); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		if r := c.responses[t]; r != nil {
			delete(c.responses, t)
			delete(c.pending, t)
			if err := responseError(r); err != nil {
				return nil, err
			}
			return public(r), nil
		}
		if c.failure != "" {
			delete(c.pending, t)
			return nil, fail(c.failure)
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			delete(c.pending, t)
			return nil, fail(fmt.Sprintf("timed out waiting for %q response", event))
		}
		if err := ctx.Err(); err != nil {
			delete(c.pending, t)
			return nil, fail("debugger operation canceled: " + err.Error())
		}
		c.mu.Unlock()
		time.Sleep(time.Millisecond)
		c.mu.Lock()
	}
}
func (c *connection) sendWait(event string, params map[string]any, want string, timeout time.Duration) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(c.ctx, timeout)
	defer cancel()
	c.mu.Lock()
	after := c.sequence
	c.mu.Unlock()
	t := c.ticket()
	m := map[string]any{"event": event, "ticket": t}
	for k, v := range params {
		m[k] = v
	}
	if err := c.sendMessage(ctx, m); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		if r := c.responses[t]; r != nil {
			delete(c.responses, t)
			if err := responseError(r); err != nil {
				delete(c.pending, t)
				return nil, err
			}
		}
		for i, e := range c.events {
			if e.sequence > after && asString(e.message["event"]) == want {
				c.events = append(c.events[:i], c.events[i+1:]...)
				delete(c.pending, t)
				return public(e.message), nil
			}
		}
		if c.failure != "" {
			delete(c.pending, t)
			return nil, fail(c.failure)
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			delete(c.pending, t)
			return nil, fail(fmt.Sprintf("timed out waiting for %q event", want))
		}
		if err := ctx.Err(); err != nil {
			delete(c.pending, t)
			return nil, fail("debugger operation canceled: " + err.Error())
		}
		c.mu.Unlock()
		time.Sleep(time.Millisecond)
		c.mu.Lock()
	}
}
func (c *connection) waitEvent(want string, buffered bool, timeout time.Duration) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(c.ctx, timeout)
	defer cancel()
	c.mu.Lock()
	after := c.sequence
	if buffered {
		after = 0
	}
	defer c.mu.Unlock()
	for {
		for i, e := range c.events {
			if e.sequence > after && asString(e.message["event"]) == want {
				c.events = append(c.events[:i], c.events[i+1:]...)
				return public(e.message), nil
			}
		}
		if c.failure != "" {
			return nil, fail(c.failure)
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fail(fmt.Sprintf("timed out waiting for %q event", want))
		}
		if err := ctx.Err(); err != nil {
			return nil, fail("debugger operation canceled: " + err.Error())
		}
		c.mu.Unlock()
		time.Sleep(time.Millisecond)
		c.mu.Lock()
	}
}
func (c *connection) drain(limit int) map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := limit
	if n > len(c.events) {
		n = len(c.events)
	}
	out := make([]any, n)
	for i := 0; i < n; i++ {
		out[i] = public(c.events[i].message)
	}
	c.events = c.events[n:]
	d := c.dropped
	c.dropped = 0
	return map[string]any{"events": out, "dropped": d, "remaining": len(c.events)}
}

type runner struct {
	opts Options
	conn *connection
}

func (r *runner) connect(ctx context.Context) (map[string]any, error) {
	dctx, cancel := context.WithTimeout(ctx, r.opts.ConnectTimeout)
	defer cancel()
	ws, resp, err := websocket.Dial(dctx, url(r.opts), &websocket.DialOptions{Subprotocols: []string{Subprotocol}})
	if err != nil {
		return nil, fail("cannot connect to " + url(r.opts) + ": " + err.Error())
	}
	_ = resp
	// PPSSPP replay.flush responses are unbounded and destructive: PPSSPP clears
	// the recording after serializing it. Preserve the legacy bridge behavior so
	// a large valid response is not discarded after the source has been cleared.
	ws.SetReadLimit(-1)
	if ws.Subprotocol() != Subprotocol {
		_ = ws.CloseNow()
		return nil, fail("PPSSPP did not negotiate the required " + Subprotocol + " subprotocol")
	}
	connection := newConnection(ctx, ws, r.opts)
	v, err := connection.request("version", map[string]any{"name": "zill-debugger-bridge", "version": strconv.Itoa(BridgeVersion)}, r.opts.Timeout)
	if err != nil {
		connection.close()
		return nil, err
	}
	ready := map[string]any{"event": "ready", "bridge_format": BridgeFormat, "bridge_version": BridgeVersion, "url": url(r.opts), "subprotocol": ws.Subprotocol(), "server": v, "suppressed_broadcasts": []string{}, "memory_write_enabled": r.opts.AllowMemoryWrite}
	// PPSSPP 1.20.4 lazily initializes these keys after its first event.
	time.Sleep(50 * time.Millisecond)
	if _, err := connection.request("broadcast.config.set", map[string]any{"disallowed": map[string]any{"logger": true, "input": true}}, r.opts.Timeout); err == nil {
		ready["suppressed_broadcasts"] = []string{"logger", "input"}
	} else {
		ready["warning"] = "could not suppress noisy broadcasts: " + err.Error()
	}
	r.conn = connection
	return ready, nil
}

// Run reads commands from stdin and writes exactly one JSON object per command to stdout.
func Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, opts Options) int {
	if err := opts.Validate(); err != nil {
		fmt.Fprintln(stderr, "ppsspp-debug:", err)
		return 2
	}
	r := &runner{opts: opts}
	ready, err := r.connect(ctx)
	if err != nil {
		emit(stdout, map[string]any{"event": "fatal", "error": err.Error(), "details": details(err)})
		return 1
	}
	defer func() {
		if r.conn != nil {
			r.conn.close()
		}
	}()
	emit(stdout, ready)
	reader := bufio.NewReader(stdin)
	line := 0
	for {
		data, readErr := reader.ReadBytes('\n')
		if len(data) == 0 {
			if errors.Is(readErr, io.EOF) {
				return 0
			}
			if readErr != nil {
				fmt.Fprintln(stderr, "ppsspp-debug:", readErr)
				return 1
			}
			continue
		}
		line++
		if strings.TrimSpace(string(data)) == "" {
			continue
		}
		cmd, err := decodeCommand(data)
		if err != nil {
			emit(stdout, errorRecord(nil, fail(fmt.Sprintf("line %d is not valid JSON: %v", line, err))))
			continue
		}
		id := commandID(cmd["id"])
		if id == nil {
			emit(stdout, errorRecord(nil, fail("id must be a string or integer")))
			continue
		}
		name := asString(cmd["command"])
		if broken := r.conn.broken(); broken != "" && name != "drain" && name != "quit" {
			r.conn.close()
			info, e := r.connect(ctx)
			if e != nil {
				emit(stdout, errorRecord(id, fail("PPSSPP disconnected ("+broken+"); reconnect failed: "+e.Error())))
				continue
			}
			info["event"] = "reconnected"
			info["previous_error"] = broken
			emit(stdout, info)
		}
		result, quit, e := r.execute(cmd)
		if e != nil {
			emit(stdout, errorRecord(id, e))
			continue
		}
		emit(stdout, map[string]any{"id": id, "ok": true, "result": result})
		if quit {
			return 0
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			fmt.Fprintln(stderr, "ppsspp-debug:", readErr)
			return 1
		}
	}
}
func emit(w io.Writer, v any) { b, _ := json.Marshal(v); fmt.Fprintln(w, string(b)) }
func decodeCommand(data []byte) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple JSON values on one command line")
		}
		return nil, err
	}
	command, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("command line must be a JSON object")
	}
	return command, nil
}
func commandID(v any) any {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		if !strings.ContainsAny(x.String(), ".eE") {
			return x
		}
	}
	return nil
}
func details(err error) any {
	var e *bridgeError
	if errors.As(err, &e) {
		return e.details
	}
	return nil
}
func errorRecord(id any, err error) map[string]any {
	x := map[string]any{"id": id, "ok": false, "error": map[string]any{"message": err.Error()}}
	if d := details(err); d != nil {
		x["error"].(map[string]any)["details"] = d
	}
	return x
}

func timeout(cmd map[string]any, def time.Duration) (time.Duration, error) {
	v, ok := cmd["timeout"]
	if !ok {
		return def, nil
	}
	n, ok := number(v)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 || n > float64(math.MaxInt64)/float64(time.Second) {
		return 0, fail("timeout must be a positive finite number")
	}
	return time.Duration(n * float64(time.Second)), nil
}
func fields(cmd map[string]any, allowed ...string) error {
	ok := map[string]bool{"id": true, "command": true}
	for _, s := range allowed {
		ok[s] = true
	}
	var extra []string
	for k := range cmd {
		if !ok[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		return fail("unsupported command fields: " + strings.Join(extra, ", "))
	}
	return nil
}
func requiredString(c map[string]any, n string) (string, error) {
	s, ok := c[n].(string)
	if !ok || s == "" {
		return "", fail(n + " must be a non-empty string")
	}
	return s, nil
}
func requiredInt(c map[string]any, n string, min, max, def int) (int, error) {
	v, ok := c[n]
	if !ok {
		if def < min || def > max {
			return 0, fail(n + " is required")
		}
		return def, nil
	}
	integer, ok := integer(v)
	if !ok {
		return 0, fail(n + " must be an integer")
	}
	if integer < int64(min) || integer > int64(max) {
		return 0, fail(fmt.Sprintf("%s must be between %d and %d", n, min, max))
	}
	return int(integer), nil
}
func address(c map[string]any) (any, error) {
	v := c["address"]
	switch x := v.(type) {
	case string:
		if x != "" {
			return x, nil
		}
	case json.Number:
		n, err := x.Int64()
		if err == nil && n >= -(1<<31) && n <= (1<<32)-1 {
			return n, nil
		}
	}
	return nil, fail("address must be a 32-bit integer or numeric string")
}
func noWrap(a any, n int) error {
	var x int64
	var err error
	switch v := a.(type) {
	case int64:
		x = v
	case string:
		x, err = strconv.ParseInt(v, 0, 64)
		if err != nil {
			x, err = strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil
			}
		}
	}
	if x >= -(1<<31) && x <= (1<<32)-1 && uint64(uint32(x))+uint64(n) > 1<<32 {
		return fail("memory range wraps past the end of the 32-bit address space")
	}
	return nil
}

func number(value any) (float64, bool) {
	switch value := value.(type) {
	case json.Number:
		number, err := value.Float64()
		return number, err == nil
	case float64:
		return value, true
	default:
		return 0, false
	}
}

func integer(value any) (int64, bool) {
	switch value := value.(type) {
	case json.Number:
		if strings.ContainsAny(value.String(), ".eE") {
			return 0, false
		}
		integer, err := value.Int64()
		return integer, err == nil
	case float64:
		if math.Trunc(value) == value && value >= math.MinInt64 && value <= math.MaxInt64 {
			return int64(value), true
		}
	}
	return 0, false
}

func (r *runner) cpu(t time.Duration) (map[string]any, error) {
	return r.conn.request("cpu.status", nil, t)
}
func (r *runner) pause(t time.Duration) (map[string]any, error) {
	s, e := r.cpu(t)
	if e != nil {
		return nil, e
	}
	if p, _ := s["paused"].(bool); p {
		return nil, fail("PPSSPP is paused by its UI, not by the debugger; resume gameplay before using pause or observe", map[string]any{"cpu": s})
	}
	if q, _ := s["stepping"].(bool); q {
		return map[string]any{"changed": false, "cpu": s}, nil
	}
	e1, e := r.conn.sendWait("cpu.stepping", nil, "cpu.stepping", t)
	if e == nil {
		return map[string]any{"changed": true, "event": e1}, nil
	}
	// The state transition can succeed even if its broadcast is delayed or
	// dropped. Confirm the resulting state so observe still owns restoration.
	confirmed, confirmErr := r.cpu(t)
	if confirmErr == nil && confirmed["stepping"] == true && confirmed["paused"] != true {
		return map[string]any{"changed": true, "cpu": confirmed}, nil
	}
	return nil, e
}
func (r *runner) resume(t time.Duration) (map[string]any, error) {
	s, e := r.cpu(t)
	if e != nil {
		return nil, e
	}
	if q, _ := s["stepping"].(bool); !q {
		return map[string]any{"changed": false, "cpu": s}, nil
	}
	e1, e := r.conn.sendWait("cpu.resume", nil, "cpu.resume", t)
	if e == nil {
		return map[string]any{"changed": true, "event": e1}, nil
	}
	confirmed, confirmErr := r.cpu(t)
	if confirmErr == nil && confirmed["stepping"] != true {
		return map[string]any{"changed": true, "cpu": confirmed}, nil
	}
	return nil, e
}
func (r *runner) execute(c map[string]any) (map[string]any, bool, error) {
	name, e := requiredString(c, "command")
	if e != nil {
		return nil, false, e
	}
	t, e := timeout(c, r.opts.Timeout)
	if e != nil {
		return nil, false, e
	}
	switch name {
	case "status":
		if e = fields(c, "timeout"); e == nil {
			g, x := r.conn.request("game.status", nil, t)
			if x != nil {
				return nil, false, x
			}
			cpu, x := r.cpu(t)
			return map[string]any{"game": g, "cpu": cpu}, false, x
		}
	case "pause":
		if e = fields(c, "timeout"); e == nil {
			v, x := r.pause(t)
			return v, false, x
		}
	case "resume":
		if e = fields(c, "timeout"); e == nil {
			v, x := r.resume(t)
			return v, false, x
		}
	case "buttons":
		if e = fields(c, "buttons", "timeout"); e == nil {
			b, ok := c["buttons"].(map[string]any)
			if !ok || len(b) == 0 {
				return nil, false, fail("buttons must be a non-empty object")
			}
			for k, v := range b {
				if k == "" {
					return nil, false, fail("buttons keys must be strings and values must be booleans")
				}
				if _, ok := v.(bool); !ok {
					return nil, false, fail("buttons keys must be strings and values must be booleans")
				}
			}
			v, x := r.conn.request("input.buttons.send", map[string]any{"buttons": b}, t)
			return map[string]any{"response": v}, false, x
		}
	case "analog":
		if e = fields(c, "stick", "x", "y", "timeout"); e == nil {
			stick := "left"
			if v, ok := c["stick"]; ok {
				stick, ok = v.(string)
				if !ok || (stick != "left" && stick != "right") {
					return nil, false, fail("stick must be 'left' or 'right'")
				}
			}
			x, xok := number(c["x"])
			y, yok := number(c["y"])
			if !xok || !yok || math.IsNaN(x) || math.IsNaN(y) || x < -1 || x > 1 || y < -1 || y > 1 {
				return nil, false, fail("x and y must be between -1.0 and 1.0")
			}
			v, z := r.conn.request("input.analog.send", map[string]any{"stick": stick, "x": x, "y": y}, t)
			return map[string]any{"response": v}, false, z
		}
	case "press":
		if e = fields(c, "button", "duration", "timeout"); e == nil {
			b, x := requiredString(c, "button")
			if x != nil {
				return nil, false, x
			}
			d, x := requiredInt(c, "duration", 0, math.MaxInt32, 1)
			if x != nil {
				return nil, false, x
			}
			s, x := r.cpu(t)
			if x != nil {
				return nil, false, x
			}
			if s["stepping"] == true || s["paused"] == true {
				return nil, false, fail("press requires the game to be advancing; use buttons to set a held state before resuming instead", map[string]any{"cpu": s})
			}
			v, x := r.conn.request("input.buttons.press", map[string]any{"button": b, "duration": d}, t)
			return map[string]any{"response": v, "vblank_changes": d + 1}, false, x
		}
	case "wait":
		if e = fields(c, "event", "buffered", "timeout"); e == nil {
			w, x := requiredString(c, "event")
			if x != nil {
				return nil, false, x
			}
			buffered := true
			if v, ok := c["buffered"]; ok {
				var good bool
				buffered, good = v.(bool)
				if !good {
					return nil, false, fail("buffered must be a boolean")
				}
			}
			v, x := r.conn.waitEvent(w, buffered, t)
			return map[string]any{"event": v}, false, x
		}
	case "drain":
		if e = fields(c, "limit"); e == nil {
			n, x := requiredInt(c, "limit", 1, r.opts.EventBuffer, r.opts.EventBuffer)
			return r.conn.drain(n), false, x
		}
	case "read_memory":
		return r.readMemory(c, t)
	case "write_memory":
		return r.writeMemory(c, t)
	case "observe":
		return r.observe(c, t)
	case "raw":
		return r.raw(c, t)
	case "quit":
		if e = fields(c); e == nil {
			return map[string]any{"closed": true}, true, nil
		}
	default:
		return nil, false, fail(fmt.Sprintf("unsupported command %q", name))
	}
	return nil, false, e
}

func (r *runner) readMemory(c map[string]any, t time.Duration) (map[string]any, bool, error) {
	if e := fields(c, "address", "size", "replacements", "path", "timeout"); e != nil {
		return nil, false, e
	}
	a, e := address(c)
	if e != nil {
		return nil, false, e
	}
	n, e := requiredInt(c, "size", 1, maxMemoryBytes, -1)
	if e != nil {
		return nil, false, e
	}
	if e = noWrap(a, n); e != nil {
		return nil, false, e
	}
	rep := true
	if v, ok := c["replacements"]; ok {
		var good bool
		rep, good = v.(bool)
		if !good {
			return nil, false, fail("replacements must be a boolean")
		}
	}
	path, has := c["path"].(string)
	if _, ok := c["path"]; ok && (!has || path == "") {
		return nil, false, fail("path must be a non-empty string")
	}
	if !has && n > r.opts.MaxInlineBytes {
		return nil, false, fail(fmt.Sprintf("read size exceeds %d inline bytes; provide path", r.opts.MaxInlineBytes))
	}
	resp, e := r.conn.request("memory.read", map[string]any{"address": a, "size": n, "replacements": rep}, t)
	if e != nil {
		return nil, false, e
	}
	enc, ok := resp["base64"].(string)
	if !ok {
		return nil, false, fail("base64 must be a base64 string")
	}
	data, e := decodeBase64(enc)
	if e != nil {
		return nil, false, fail("base64 is not valid base64")
	}
	if len(data) != n {
		return nil, false, fail(fmt.Sprintf("PPSSPP returned %d memory bytes, expected %d", len(data), n))
	}
	sum := sha256.Sum256(data)
	out := map[string]any{"bytes": len(data), "sha256": "sha256:" + fmt.Sprintf("%x", sum)}
	if has {
		p, e := writeBytes(path, data)
		if e != nil {
			return nil, false, e
		}
		out["path"] = p
	} else {
		out["base64"] = enc
	}
	return out, false, nil
}
func (r *runner) writeMemory(c map[string]any, t time.Duration) (map[string]any, bool, error) {
	if e := fields(c, "address", "base64", "path", "timeout"); e != nil {
		return nil, false, e
	}
	if !r.opts.AllowMemoryWrite {
		return nil, false, fail("memory mutation requires --allow-memory-write")
	}
	a, e := address(c)
	if e != nil {
		return nil, false, e
	}
	b64Value, hasBase64 := c["base64"]
	pathValue, hasPath := c["path"]
	if hasBase64 == hasPath {
		return nil, false, fail("provide exactly one of base64 or path")
	}
	b64, base64OK := b64Value.(string)
	path, pathOK := pathValue.(string)
	var data []byte
	if hasPath {
		if !pathOK || path == "" {
			return nil, false, fail("path must be a non-empty string")
		}
		data, e = readMemoryWriteFile(path)
		if e != nil {
			return nil, false, e
		}
		b64 = base64.StdEncoding.EncodeToString(data)
	} else {
		if !base64OK {
			return nil, false, fail("base64 must be a base64 string")
		}
		if len(b64) > base64.StdEncoding.EncodedLen(maxMemoryBytes) {
			return nil, false, fail(fmt.Sprintf("memory write exceeds %d bytes", maxMemoryBytes))
		}
		data, e = decodeBase64(b64)
		if e != nil {
			return nil, false, fail("base64 is not valid base64")
		}
	}
	if len(data) == 0 {
		return nil, false, fail("memory write data must not be empty")
	}
	if len(data) > maxMemoryBytes {
		return nil, false, fail(fmt.Sprintf("memory write exceeds %d bytes", maxMemoryBytes))
	}
	if e = noWrap(a, len(data)); e != nil {
		return nil, false, e
	}
	resp, e := r.conn.request("memory.write", map[string]any{"address": a, "base64": b64}, t)
	return map[string]any{"bytes": len(data), "response": resp}, false, e
}

func (r *runner) observe(c map[string]any, timeout time.Duration) (map[string]any, bool, error) {
	if err := fields(c, "path", "restore", "timeout"); err != nil {
		return nil, false, err
	}
	path, err := requiredString(c, "path")
	if err != nil {
		return nil, false, err
	}
	restore := true
	if v, ok := c["restore"]; ok {
		var good bool
		restore, good = v.(bool)
		if !good {
			return nil, false, fail("restore must be a boolean")
		}
	}
	paused, err := r.pause(timeout)
	if err != nil {
		return nil, false, err
	}
	pausedHere, _ := paused["changed"].(bool)
	var primary error
	var result map[string]any
	if os.Getenv("DISPLAY") == "" {
		primary = fail("observe requires an X11 DISPLAY")
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "xdotool", "search", "--onlyvisible", "--class", "--classname", "--any", "[Pp][Pp][Ss][Ss][Pp][Pp]").Output()
		if err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				primary = fail("observe requires xdotool")
			} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				primary = fail("timed out locating the PPSSPP window")
			} else {
				primary = fail("could not find a visible PPSSPP X11 window; make sure PPSSPP is running and not minimized")
			}
		} else {
			seen := map[string]bool{}
			var ids []string
			for _, id := range strings.Fields(string(out)) {
				if _, e := strconv.ParseUint(id, 10, 64); e == nil && !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
			if len(ids) == 0 {
				primary = fail("could not find a visible PPSSPP X11 window; make sure PPSSPP is running and not minimized")
			} else if len(ids) != 1 {
				primary = fail("found multiple visible PPSSPP windows; close extra PPSSPP windows before observe (" + strings.Join(ids, ", ") + ")")
			} else {
				result, primary = captureX11(ctx, ids[0], path, pausedHere)
			}
		}
	}
	var restored bool
	if pausedHere && restore {
		if _, err := r.resume(timeout); err != nil {
			if primary == nil {
				primary = err
			} else {
				primary = fail(primary.Error() + "; also failed to restore CPU state: " + err.Error())
			}
		} else {
			restored = true
		}
	}
	if primary != nil {
		return nil, false, primary
	}
	result["restored"] = restored
	return result, false, nil
}

func captureX11(ctx context.Context, window, destination string, paused bool) (map[string]any, error) {
	path, err := filepath.Abs(destination)
	if err != nil {
		return nil, fail("cannot prepare screenshot path " + destination + ": " + err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fail("cannot prepare screenshot path " + path + ": " + err.Error())
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.png")
	if err != nil {
		return nil, fail("cannot prepare screenshot path " + path + ": " + err.Error())
	}
	temp := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(temp)
	if out, err := exec.CommandContext(ctx, "import", "-window", window, "png:"+temp).CombinedOutput(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fail("observe requires ImageMagick's import command")
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fail("timed out capturing the PPSSPP window")
		}
		message := strings.TrimSpace(string(out))
		if message != "" {
			return nil, fail("could not capture the PPSSPP window: " + message)
		}
		return nil, fail("could not capture the PPSSPP window")
	}
	b, err := os.ReadFile(temp)
	if err != nil {
		return nil, fail("cannot read captured screenshot " + temp + ": " + err.Error())
	}
	if len(b) < 24 || string(b[:8]) != "\x89PNG\r\n\x1a\n" || string(b[12:16]) != "IHDR" {
		return nil, fail("X11 screenshot tool did not produce a valid PNG")
	}
	if err := os.Rename(temp, path); err != nil {
		return nil, fail("cannot write screenshot " + path + ": " + err.Error())
	}
	width := uint32(b[16])<<24 | uint32(b[17])<<16 | uint32(b[18])<<8 | uint32(b[19])
	height := uint32(b[20])<<24 | uint32(b[21])<<16 | uint32(b[22])<<8 | uint32(b[23])
	sum := sha256.Sum256(b)
	return map[string]any{"path": path, "bytes": len(b), "sha256": "sha256:" + fmt.Sprintf("%x", sum), "width": width, "height": height, "source": "x11-window", "window_id": window, "paused_by_bridge": paused}, nil
}
func (r *runner) raw(c map[string]any, t time.Duration) (map[string]any, bool, error) {
	if e := fields(c, "event", "params", "expect", "wait_event", "timeout"); e != nil {
		return nil, false, e
	}
	event, e := requiredString(c, "event")
	if e != nil {
		return nil, false, e
	}
	if !r.opts.AllowMemoryWrite && map[string]bool{"cpu.setReg": true, "memory.assemble": true, "memory.write": true, "memory.write_u8": true, "memory.write_u16": true, "memory.write_u32": true}[event] {
		return nil, false, fail(event + " requires --allow-memory-write")
	}
	p := map[string]any{}
	if v, ok := c["params"]; ok {
		var good bool
		p, good = v.(map[string]any)
		if !good {
			return nil, false, fail("params must be an object")
		}
		if _, ok := p["event"]; ok {
			return nil, false, fail("params must not contain event or ticket")
		}
		if _, ok := p["ticket"]; ok {
			return nil, false, fail("params must not contain event or ticket")
		}
	}
	expect := "response"
	if value, ok := c["expect"]; ok {
		var valid bool
		expect, valid = value.(string)
		if !valid {
			return nil, false, fail("expect must be 'response', 'event', or 'none'")
		}
	}
	switch expect {
	case "response":
		v, x := r.conn.request(event, p, t)
		return map[string]any{"response": v}, false, x
	case "none":
		ctx, cancel := context.WithTimeout(r.conn.ctx, t)
		defer cancel()
		x := r.conn.sendMessage(ctx, merge(map[string]any{"event": event}, p))
		return map[string]any{"sent": event}, false, x
	case "event":
		want := event
		if v, ok := c["wait_event"]; ok {
			var good bool
			want, good = v.(string)
			if !good || want == "" {
				return nil, false, fail("wait_event must be a non-empty string")
			}
		}
		v, x := r.conn.sendWait(event, p, want, t)
		return map[string]any{"event": v}, false, x
	}
	return nil, false, fail("expect must be 'response', 'event', or 'none'")
}
func merge(a, b map[string]any) map[string]any {
	for k, v := range b {
		a[k] = v
	}
	return a
}
func writeBytes(path string, data []byte) (string, error) {
	p, e := filepath.Abs(path)
	if e != nil {
		return "", fail("cannot write " + path + ": " + e.Error())
	}
	if e = os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		return "", fail("cannot write " + p + ": " + e.Error())
	}
	if e = os.WriteFile(p, data, 0644); e != nil {
		return "", fail("cannot write " + p + ": " + e.Error())
	}
	return p, nil
}

func readMemoryWriteFile(path string) ([]byte, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fail("cannot read " + path + ": " + err.Error())
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxMemoryBytes+1))
	if err != nil {
		return nil, fail("cannot read " + path + ": " + err.Error())
	}
	if len(data) > maxMemoryBytes {
		return nil, fail(fmt.Sprintf("memory write exceeds %d bytes", maxMemoryBytes))
	}
	return data, nil
}

func decodeBase64(encoded string) ([]byte, error) {
	if strings.ContainsAny(encoded, "\r\n") {
		return nil, errors.New("base64 is not valid base64")
	}
	return base64.StdEncoding.Strict().DecodeString(encoded)
}
