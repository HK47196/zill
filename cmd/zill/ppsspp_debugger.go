// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/HK47196/zill/internal/ppssppdebug"
)

const ppssppDebuggerUsage = `Usage: zill ppsspp-debugger --port PORT [options]

Options:
	--host HOST             Debugger host (default 127.0.0.1)
	--allow-remote          Permit a non-loopback host (no authentication or TLS)
	--allow-memory-write    Enable memory and register mutation
	--timeout SECONDS       Per-command timeout (default 5)
	--connect-timeout SECS  Connection timeout (default 5)
	--event-buffer COUNT    Buffered broadcast limit (default 1024)
	--max-inline-bytes N    Largest inline memory read (default 4096)
`

func runPPSSPPDebugger(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	options := ppssppdebug.DefaultOptions()
	timeoutSeconds := options.Timeout.Seconds()
	connectTimeoutSeconds := options.ConnectTimeout.Seconds()

	flags := flag.NewFlagSet("ppsspp-debugger", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.IntVar(&options.Port, "port", 0, "PPSSPP debugger port")
	flags.StringVar(&options.Host, "host", options.Host, "PPSSPP debugger host")
	flags.BoolVar(&options.AllowRemote, "allow-remote", false, "allow a non-loopback host")
	flags.BoolVar(&options.AllowMemoryWrite, "allow-memory-write", false, "allow memory mutation")
	flags.Float64Var(&timeoutSeconds, "timeout", timeoutSeconds, "per-command timeout in seconds")
	flags.Float64Var(&connectTimeoutSeconds, "connect-timeout", connectTimeoutSeconds, "connection timeout in seconds")
	flags.IntVar(&options.EventBuffer, "event-buffer", options.EventBuffer, "broadcast buffer size")
	flags.IntVar(&options.MaxInlineBytes, "max-inline-bytes", options.MaxInlineBytes, "inline memory read limit")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, ppssppDebuggerUsage)
			return 0
		}
		fmt.Fprintf(stderr, "zill: ppsspp-debugger: %v\n", err)
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "zill: ppsspp-debugger: unexpected argument %q\n", flags.Arg(0))
		return 2
	}
	maximumSeconds := float64(math.MaxInt64) / float64(time.Second)
	if math.IsNaN(timeoutSeconds) || math.IsInf(timeoutSeconds, 0) || timeoutSeconds <= 0 || timeoutSeconds > maximumSeconds {
		fmt.Fprintln(stderr, "zill: ppsspp-debugger: timeout must be positive")
		return 2
	}
	if math.IsNaN(connectTimeoutSeconds) || math.IsInf(connectTimeoutSeconds, 0) || connectTimeoutSeconds <= 0 || connectTimeoutSeconds > maximumSeconds {
		fmt.Fprintln(stderr, "zill: ppsspp-debugger: connect-timeout must be positive")
		return 2
	}
	options.Timeout = time.Duration(timeoutSeconds * float64(time.Second))
	options.ConnectTimeout = time.Duration(connectTimeoutSeconds * float64(time.Second))
	if err := options.Validate(); err != nil {
		fmt.Fprintf(stderr, "zill: ppsspp-debugger: %v\n", err)
		return 2
	}
	return ppssppdebug.Run(context.Background(), stdin, stdout, stderr, options)
}
