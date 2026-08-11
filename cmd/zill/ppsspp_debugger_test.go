// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunAdvertisesPPSSPPDebugger(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if exit := run([]string{"help"}, strings.NewReader(""), &stdout, &stderr); exit != 0 {
		t.Fatalf("run(help) exit = %d, stderr = %q", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ppsspp-debugger") {
		t.Fatalf("help does not advertise debugger command: %q", stdout.String())
	}
}

func TestRunPPSSPPDebuggerRequiresSafeConnectionOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "port", args: nil, want: "port must be between 1 and 65535"},
		{name: "remote opt-in", args: []string{"--port", "56244", "--host", "192.0.2.4"}, want: "non-loopback host requires allow-remote"},
		{name: "finite timeout", args: []string{"--port", "56244", "--timeout", "Inf"}, want: "timeout must be positive"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := runPPSSPPDebugger(test.args, strings.NewReader(""), &stdout, &stderr)
			if exit != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("exit=%d stdout=%q stderr=%q, want fail-closed error containing %q", exit, stdout.String(), stderr.String(), test.want)
			}
		})
	}
}
