// SPDX-License-Identifier: GPL-3.0-or-later

package cdccontext

import "testing"

func TestParseScenarioReserveMarkerRequiresExactlyTwoEventDigits(t *testing.T) {
	event, label, ok := parseScenarioReserveMarker("予備メッセージ１<line-break>０９始まりの地<end>")
	if !ok || event != 9 || label != "始まりの地" {
		t.Fatalf("valid marker = %d, %q, %t", event, label, ok)
	}
	for _, text := range []string{
		"予備メッセージ１<line-break>０始まりの地<end>",
		"予備メッセージ１<line-break>０９１始まりの地<end>",
	} {
		if event, label, ok := parseScenarioReserveMarker(text); ok {
			t.Fatalf("malformed marker = %d, %q", event, label)
		}
	}
}
