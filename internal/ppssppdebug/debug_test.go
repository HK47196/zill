// SPDX-License-Identifier: GPL-3.0-or-later

package ppssppdebug

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestValidateRejectsRemoteHostWithoutOptIn(t *testing.T) {
	o := DefaultOptions()
	o.Port = 56244
	o.Host = "192.0.2.4"
	if err := o.Validate(); err == nil || !strings.Contains(err.Error(), "allow-remote") {
		t.Fatalf("Validate() error = %v, want remote-host safety gate", err)
	}
}

func TestRunNegotiatesDebuggerAndSuppressesNoisyBroadcasts(t *testing.T) {
	host, port, closeServer := startFakeDebugger(t, nil)
	defer closeServer()
	o := DefaultOptions()
	o.Host = host
	o.Port = port
	o.Timeout = time.Second
	o.ConnectTimeout = time.Second
	var output bytes.Buffer
	exit := Run(context.Background(), strings.NewReader("{\"id\":1,\"command\":\"status\"}\n{\"id\":9007199254740993,\"command\":\"quit\"}\n"), &output, &bytes.Buffer{}, o)
	if exit != 0 {
		t.Fatalf("Run() exit = %d, output = %s", exit, output.String())
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("records = %d, want ready plus two command results: %s", len(lines), output.String())
	}
	if !strings.Contains(lines[0], `"subprotocol":"debugger.ppsspp.org"`) || !strings.Contains(lines[0], `"suppressed_broadcasts":["logger","input"]`) {
		t.Fatalf("ready record does not report negotiated, filtered debugger: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"ok":true`) || !strings.Contains(lines[1], `"game":{"event":"game.status","game":"running"}`) {
		t.Fatalf("status result = %s", lines[1])
	}
	if !strings.Contains(lines[2], `"id":9007199254740993`) {
		t.Fatalf("large integer command ID was not preserved exactly: %s", lines[2])
	}
}

func startFakeDebugger(t *testing.T, observe func(string)) (string, int, func()) {
	return startFakeDebuggerWithConfig(t, fakeDebuggerConfig{observe: observe})
}

type fakeDebuggerConfig struct {
	observe               func(string)
	omitSteppingBroadcast bool
	disconnectEventOnce   string
}

func startFakeDebuggerWithConfig(t *testing.T, config fakeDebuggerConfig) (string, int, func()) {
	t.Helper()
	var disconnect sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/debugger" {
			http.NotFound(w, req)
			return
		}
		if !strings.Contains(req.Header.Get("Sec-WebSocket-Protocol"), Subprotocol) {
			http.Error(w, "missing debugger protocol", http.StatusBadRequest)
			return
		}
		ws, err := websocket.Accept(w, req, &websocket.AcceptOptions{Subprotocols: []string{Subprotocol}})
		if err != nil {
			return
		}
		ws.SetReadLimit(-1)
		defer ws.Close(websocket.StatusNormalClosure, "")
		stepping := false
		for {
			_, data, err := ws.Read(context.Background())
			if err != nil {
				return
			}
			var request map[string]any
			if json.Unmarshal(data, &request) != nil {
				return
			}
			event, _ := request["event"].(string)
			if config.observe != nil {
				config.observe(event)
			}
			disconnected := false
			if event == config.disconnectEventOnce {
				disconnect.Do(func() { disconnected = true })
			}
			if disconnected {
				_ = ws.CloseNow()
				return
			}
			response := map[string]any{"event": event, "ticket": request["ticket"]}
			sendResponse := true
			switch event {
			case "version":
				response["server"] = "fake"
			case "game.status":
				response["game"] = "running"
			case "cpu.status":
				response["stepping"] = stepping
				response["paused"] = false
			case "cpu.stepping":
				stepping = true
				sendResponse = false
			case "cpu.resume":
				stepping = false
				sendResponse = false
			case "memory.read":
				size, _ := request["size"].(float64)
				response["base64"] = base64.StdEncoding.EncodeToString(make([]byte, int(size)))
			}
			if sendResponse {
				encoded, _ := json.Marshal(response)
				if ws.Write(context.Background(), websocket.MessageText, encoded) != nil {
					return
				}
			}
			if (event == "cpu.stepping" || event == "cpu.resume") && !config.omitSteppingBroadcast {
				broadcast, _ := json.Marshal(map[string]any{"event": event})
				if ws.Write(context.Background(), websocket.MessageText, broadcast) != nil {
					return
				}
			}
		}
	}))
	hostPort := strings.TrimPrefix(server.URL, "http://")
	host, portText, _ := strings.Cut(hostPort, ":")
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return host, port, server.Close
}

func TestRunBlocksMemoryMutationBeforeSendingIt(t *testing.T) {
	var mutex sync.Mutex
	var events []string
	host, port, closeServer := startFakeDebugger(t, func(event string) {
		mutex.Lock()
		defer mutex.Unlock()
		events = append(events, event)
	})
	defer closeServer()
	o := DefaultOptions()
	o.Host = host
	o.Port = port
	o.Timeout = time.Second
	o.ConnectTimeout = time.Second
	var output, errors bytes.Buffer
	input := strings.Join([]string{
		`{"id":1,"command":"write_memory","address":0,"base64":"AA=="}`,
		`{"id":2,"command":"raw","event":"cpu.setReg","params":{"name":"pc","value":0}}`,
		`{"id":3,"command":"read_memory","address":0}`,
		`{"id":4,"command":"read_memory","address":0,"size":1.0}`,
		`{"id":5,"command":"quit"}`,
	}, "\n") + "\n"
	exit := Run(context.Background(), strings.NewReader(input), &output, &errors, o)
	if exit != 0 || errors.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, output.String(), errors.String())
	}
	if strings.Count(output.String(), `"ok":false`) != 4 || !strings.Contains(output.String(), "memory mutation requires --allow-memory-write") || !strings.Contains(output.String(), "size is required") || !strings.Contains(output.String(), "size must be an integer") {
		t.Fatalf("unsafe commands were not rejected as documented: %s", output.String())
	}
	mutex.Lock()
	defer mutex.Unlock()
	for _, event := range events {
		if event == "memory.write" || event == "cpu.setReg" || event == "memory.read" {
			t.Fatalf("forbidden debugger event %q was sent; all events: %v", event, events)
		}
	}
}

func TestObserveRestoresRunningGameWhenCaptureCannotStart(t *testing.T) {
	t.Setenv("DISPLAY", "")
	var mutex sync.Mutex
	var events []string
	host, port, closeServer := startFakeDebugger(t, func(event string) {
		mutex.Lock()
		defer mutex.Unlock()
		events = append(events, event)
	})
	defer closeServer()

	destination := filepath.Join(t.TempDir(), "must-not-exist.png")
	options := DefaultOptions()
	options.Host = host
	options.Port = port
	options.Timeout = 5 * time.Second
	options.ConnectTimeout = time.Second
	input := fmt.Sprintf("{\"id\":1,\"command\":\"observe\",\"path\":%q}\n{\"id\":2,\"command\":\"quit\"}\n", destination)
	var stdout, stderr bytes.Buffer
	if exit := Run(context.Background(), strings.NewReader(input), &stdout, &stderr, options); exit != 0 {
		t.Fatalf("Run() exit = %d, stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "observe requires an X11 DISPLAY") {
		t.Fatalf("observe failure was not reported: %s", stdout.String())
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("forbidden screenshot output exists or stat failed unexpectedly: %v", err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	stepped, resumed := false, false
	for _, event := range events {
		if event == "cpu.stepping" {
			stepped = true
		}
		if event == "cpu.resume" {
			resumed = true
		}
	}
	if !stepped || !resumed {
		t.Fatalf("observe did not restore debugger state after capture failure; events=%v", events)
	}
}

func TestObserveRestoresWhenSteppingBroadcastIsLost(t *testing.T) {
	t.Setenv("DISPLAY", "")
	var mutex sync.Mutex
	var events []string
	host, port, closeServer := startFakeDebuggerWithConfig(t, fakeDebuggerConfig{
		omitSteppingBroadcast: true,
		observe: func(event string) {
			mutex.Lock()
			defer mutex.Unlock()
			events = append(events, event)
		},
	})
	defer closeServer()

	options := DefaultOptions()
	options.Host = host
	options.Port = port
	options.Timeout = 50 * time.Millisecond
	options.ConnectTimeout = time.Second
	input := "{\"id\":1,\"command\":\"observe\",\"path\":\"ignored.png\"}\n{\"id\":2,\"command\":\"quit\"}\n"
	var stdout, stderr bytes.Buffer
	if exit := Run(context.Background(), strings.NewReader(input), &stdout, &stderr, options); exit != 0 {
		t.Fatalf("Run() exit = %d, stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "observe requires an X11 DISPLAY") || strings.Contains(stdout.String(), "failed to restore CPU state") {
		t.Fatalf("observe did not report the capture failure after restoring state: %s", stdout.String())
	}
	mutex.Lock()
	defer mutex.Unlock()
	if !eventSequenceContains(events, "cpu.stepping", "cpu.status", "cpu.resume", "cpu.status") {
		t.Fatalf("observe did not confirm and restore state after lost broadcasts; events=%v", events)
	}
}

func TestRunReconnectsWithoutReplayingInterruptedCommand(t *testing.T) {
	var mutex sync.Mutex
	var events []string
	host, port, closeServer := startFakeDebuggerWithConfig(t, fakeDebuggerConfig{
		disconnectEventOnce: "game.reset",
		observe: func(event string) {
			mutex.Lock()
			defer mutex.Unlock()
			events = append(events, event)
		},
	})
	defer closeServer()

	options := DefaultOptions()
	options.Host = host
	options.Port = port
	options.Timeout = time.Second
	options.ConnectTimeout = time.Second
	input := strings.Join([]string{
		`{"id":1,"command":"raw","event":"game.reset"}`,
		`{"id":2,"command":"status"}`,
		`{"id":3,"command":"quit"}`,
	}, "\n") + "\n"
	var stdout, stderr bytes.Buffer
	if exit := Run(context.Background(), strings.NewReader(input), &stdout, &stderr, options); exit != 0 {
		t.Fatalf("Run() exit = %d, stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 5 || !strings.Contains(lines[1], `"id":1`) || !strings.Contains(lines[1], `"ok":false`) || !strings.Contains(lines[2], `"event":"reconnected"`) || !strings.Contains(lines[3], `"id":2`) || !strings.Contains(lines[3], `"ok":true`) {
		t.Fatalf("reconnect lifecycle records = %q", lines)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if countEvent(events, "game.reset") != 1 {
		t.Fatalf("interrupted mutating command was replayed; events=%v", events)
	}
}

func TestRunAcceptsLargeRawJSONLCommand(t *testing.T) {
	var mutex sync.Mutex
	var events []string
	host, port, closeServer := startFakeDebugger(t, func(event string) {
		mutex.Lock()
		defer mutex.Unlock()
		events = append(events, event)
	})
	defer closeServer()

	command, err := json.Marshal(map[string]any{
		"id":      1,
		"command": "raw",
		"event":   "replay.execute",
		"params":  map[string]any{"base64": strings.Repeat("A", (2<<20)+4)},
	})
	if err != nil {
		t.Fatal(err)
	}
	input := string(command) + "\n{\"id\":2,\"command\":\"quit\"}\n"
	options := DefaultOptions()
	options.Host = host
	options.Port = port
	options.Timeout = time.Second
	options.ConnectTimeout = time.Second
	var stdout, stderr bytes.Buffer
	if exit := Run(context.Background(), strings.NewReader(input), &stdout, &stderr, options); exit != 0 {
		t.Fatalf("Run() exit = %d, stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if strings.Count(stdout.String(), `"ok":true`) != 2 {
		t.Fatalf("large raw command did not receive one successful result: %s", stdout.String())
	}
	mutex.Lock()
	defer mutex.Unlock()
	if countEvent(events, "replay.execute") != 1 {
		t.Fatalf("large raw command was not sent exactly once; events=%v", events)
	}
}

func TestObserveCapturesPNGAndRestoresRunningGame(t *testing.T) {
	toolDir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"xdotool", "import"} {
		script := fmt.Sprintf("#!/bin/sh\nexec %q -test.run=TestDebuggerToolHelper -- %s \"$@\"\n", executable, tool)
		if err := os.WriteFile(filepath.Join(toolDir, tool), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GO_WANT_PPSSPP_TOOL_HELPER", "1")
	t.Setenv("DISPLAY", ":99")
	t.Setenv("PATH", toolDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var mutex sync.Mutex
	var events []string
	host, port, closeServer := startFakeDebugger(t, func(event string) {
		mutex.Lock()
		defer mutex.Unlock()
		events = append(events, event)
	})
	defer closeServer()

	destination := filepath.Join(t.TempDir(), "capture.png")
	options := DefaultOptions()
	options.Host = host
	options.Port = port
	options.Timeout = 5 * time.Second
	options.ConnectTimeout = time.Second
	input := fmt.Sprintf("{\"id\":1,\"command\":\"observe\",\"path\":%q}\n{\"id\":2,\"command\":\"quit\"}\n", destination)
	var stdout, stderr bytes.Buffer
	if exit := Run(context.Background(), strings.NewReader(input), &stdout, &stderr, options); exit != 0 {
		t.Fatalf("Run() exit = %d, stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"source":"x11-window"`) || !strings.Contains(stdout.String(), `"width":2`) || !strings.Contains(stdout.String(), `"height":3`) || !strings.Contains(stdout.String(), `"paused_by_bridge":true`) || !strings.Contains(stdout.String(), `"restored":true`) {
		t.Fatalf("successful observe metadata = %s", stdout.String())
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("observe output does not exist: %v", err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if !eventSequenceContains(events, "cpu.stepping", "cpu.resume") {
		t.Fatalf("observe did not pause and restore running game; events=%v", events)
	}
}

func TestObserveRejectsInvalidPNGAndRestoresRunningGame(t *testing.T) {
	toolDir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"xdotool", "import"} {
		script := fmt.Sprintf("#!/bin/sh\nexec %q -test.run=TestDebuggerToolHelper -- %s \"$@\"\n", executable, tool)
		if err := os.WriteFile(filepath.Join(toolDir, tool), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GO_WANT_PPSSPP_TOOL_HELPER", "1")
	t.Setenv("GO_PPSSPP_TOOL_INVALID_PNG", "1")
	t.Setenv("DISPLAY", ":99")
	t.Setenv("PATH", toolDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var mutex sync.Mutex
	var events []string
	host, port, closeServer := startFakeDebugger(t, func(event string) {
		mutex.Lock()
		defer mutex.Unlock()
		events = append(events, event)
	})
	defer closeServer()

	destination := filepath.Join(t.TempDir(), "must-not-exist.png")
	options := DefaultOptions()
	options.Host = host
	options.Port = port
	options.Timeout = 5 * time.Second
	options.ConnectTimeout = time.Second
	input := fmt.Sprintf("{\"id\":1,\"command\":\"observe\",\"path\":%q}\n{\"id\":2,\"command\":\"quit\"}\n", destination)
	var stdout, stderr bytes.Buffer
	if exit := Run(context.Background(), strings.NewReader(input), &stdout, &stderr, options); exit != 0 {
		t.Fatalf("Run() exit = %d, stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "did not produce a valid PNG") {
		t.Fatalf("invalid screenshot was not rejected: %s", stdout.String())
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("forbidden screenshot output exists or stat failed unexpectedly: %v", err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if !eventSequenceContains(events, "cpu.stepping", "cpu.resume") {
		t.Fatalf("observe did not restore running game after invalid capture; events=%v", events)
	}
}

func TestDebuggerToolHelper(t *testing.T) {
	if os.Getenv("GO_WANT_PPSSPP_TOOL_HELPER") != "1" {
		return
	}
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		os.Exit(2)
	}
	switch os.Args[separator+1] {
	case "xdotool":
		fmt.Println("123")
	case "import":
		target := strings.TrimPrefix(os.Args[len(os.Args)-1], "png:")
		png := []byte("not a PNG")
		if os.Getenv("GO_PPSSPP_TOOL_INVALID_PNG") != "1" {
			png = []byte{'\x89', 'P', 'N', 'G', '\r', '\n', '\x1a', '\n', 0, 0, 0, 13, 'I', 'H', 'D', 'R', 0, 0, 0, 2, 0, 0, 0, 3}
		}
		if err := os.WriteFile(target, png, 0644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func countEvent(events []string, want string) int {
	count := 0
	for _, event := range events {
		if event == want {
			count++
		}
	}
	return count
}

func eventSequenceContains(events []string, want ...string) bool {
	position := 0
	for _, event := range events {
		if position < len(want) && event == want[position] {
			position++
		}
	}
	return position == len(want)
}

func TestReadMemorySupportsDocumentedMaximumTransfer(t *testing.T) {
	host, port, closeServer := startFakeDebugger(t, nil)
	defer closeServer()

	destination := filepath.Join(t.TempDir(), "memory.bin")
	options := DefaultOptions()
	options.Host = host
	options.Port = port
	options.Timeout = time.Second
	options.ConnectTimeout = time.Second
	input := fmt.Sprintf("{\"id\":1,\"command\":\"read_memory\",\"address\":\"0x08800000\",\"size\":65536,\"path\":%q}\n{\"id\":2,\"command\":\"quit\"}\n", destination)
	var stdout, stderr bytes.Buffer
	if exit := Run(context.Background(), strings.NewReader(input), &stdout, &stderr, options); exit != 0 {
		t.Fatalf("Run() exit = %d, stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"bytes":65536`) || !strings.Contains(stdout.String(), `"ok":true`) {
		t.Fatalf("maximum memory read was not reported as successful: %s", stdout.String())
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read memory output: %v", err)
	}
	if len(data) != maxMemoryBytes {
		t.Fatalf("memory output length = %d, want %d", len(data), maxMemoryBytes)
	}
}
