package auth

import (
	"strings"
	"testing"
)

func TestSafeLog(t *testing.T) {
	if got := SafeLog("mrossi\nlevel=ERROR msg=falso\r"); strings.ContainsAny(got, "\r\n") {
		t.Fatalf("a capo non rimossi: %q", got)
	}
	if got := SafeLog(strings.Repeat("a", 500)); len(got) > 64 {
		t.Fatalf("valore non troncato: %d caratteri", len(got))
	}
	if got := SafeLog("mrossi@comune.local"); got != "mrossi@comune.local" {
		t.Fatalf("valore normale alterato: %q", got)
	}
}
