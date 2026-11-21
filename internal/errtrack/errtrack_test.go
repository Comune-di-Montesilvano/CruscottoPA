package errtrack

import (
	"regexp"
	"strings"
	"testing"
)

func TestNewEmptyDSN(t *testing.T) {
	tr, err := New(Options{DSN: "  "})
	if tr != nil || err != nil {
		t.Fatalf("New vuota = %v, %v", tr, err)
	}
	// metodi sicuri su nil
	tr.CaptureMessage("x", "x", nil, nil, "")
	tr.CapturePanic("x", Request{}, "")
	tr.CaptureBrowser(BrowserError{Message: "x"}, "")
	tr.Flush(0)
	if tr.Enabled() || tr.UserID("mrossi") != "" {
		t.Fatal("tracker nil attivo")
	}
}

func TestNewInvalidDSN(t *testing.T) {
	if _, err := New(Options{DSN: "non-una-dsn"}); err == nil {
		t.Fatal("DSN non valida accettata")
	}
}

func TestCaptureMessage(t *testing.T) {
	tr, events := NewForTest(t)
	fp := "ticket: registro|web.(*Server).handleTicket"
	tr.CaptureMessage("ticket: registro", fp, map[string]string{
		"err":  "bind CN=X,DC=it: fail",
		"mail": "a@example.it",
	}, nil, "")
	ev := one(t, events())
	if ev.Level != "error" || ev.Message != "ticket: registro" {
		t.Fatalf("level/message = %q %q", ev.Level, ev.Message)
	}
	if len(ev.Fingerprint) != 1 || ev.Fingerprint[0] != fp {
		t.Fatalf("fingerprint = %v", ev.Fingerprint)
	}
	if ev.Tags["origine"] != "server" || ev.Environment != "test" || ev.Release != "test" {
		t.Fatalf("tag/env/release = %v %q %q", ev.Tags, ev.Environment, ev.Release)
	}
	if ev.Contexts["dettagli"]["err"] != "bind <dn>: fail" {
		t.Fatalf("err = %v", ev.Contexts["dettagli"]["err"])
	}
	if _, ok := ev.Contexts["dettagli"]["mail"]; ok {
		t.Fatal("attributo personale inviato")
	}
}

func TestUserID(t *testing.T) {
	tr, _ := NewForTest(t)
	id := tr.UserID("mrossi")
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(id) {
		t.Fatalf("id = %q", id)
	}
	if tr.UserID("MRossi") != id {
		t.Fatal("hash sensibile alle maiuscole")
	}
	other, _ := New(Options{DSN: "https://k@glitchtip.example.it/1", Secret: "altro", Transport: &recorder{}})
	if other.UserID("mrossi") == id {
		t.Fatal("segreti diversi, stesso hash")
	}
	if tr.UserID("") != "" {
		t.Fatal("anonimo con id")
	}
}

func TestNoPII(t *testing.T) {
	tr, events := NewForTest(t)
	uid := tr.UserID("mrossi")
	tr.CaptureMessage("errore interno", "fp", nil, &Request{Method: "GET", Path: "/ticket/42?q=mario@example.it"}, uid)
	ev := one(t, events())
	if ev.User.ID != uid || ev.User.Username != "" || ev.User.Email != "" || ev.User.IPAddress != "" || ev.User.Name != "" {
		t.Fatalf("user = %+v", ev.User)
	}
	r := ev.Request
	if r == nil || r.URL != "/ticket/{id}" || r.Method != "GET" {
		t.Fatalf("request = %+v", r)
	}
	if len(r.Headers) != 0 || r.Cookies != "" || r.Data != "" || r.QueryString != "" || len(r.Env) != 0 {
		t.Fatalf("request con dati: %+v", r)
	}
	if ev.ServerName != "cruscottopa" {
		t.Fatalf("server_name = %q", ev.ServerName)
	}
	if len(ev.Modules) != 0 || len(ev.Contexts) != 0 { // nessun attributo: nessun contesto
		t.Fatalf("modules/contexts: %v %v", ev.Modules, ev.Contexts)
	}
}

func panicker() { panic("boom mario@example.it") }

func TestCapturePanic(t *testing.T) {
	tr, events := NewForTest(t)
	func() {
		defer func() {
			if v := recover(); v != nil {
				tr.CapturePanic(v, Request{Method: "GET", Path: "/"}, "")
			}
		}()
		panicker()
	}()
	ev := one(t, events())
	if len(ev.Exception) != 1 || ev.Exception[0].Value != "boom <mail>" {
		t.Fatalf("exception = %+v", ev.Exception)
	}
	st := ev.Exception[0].Stacktrace
	if st == nil || len(st.Frames) == 0 {
		t.Fatal("stack assente")
	}
	if last := st.Frames[len(st.Frames)-1]; !strings.Contains(last.Function, "panicker") {
		t.Fatalf("frame finale = %s", last.Function)
	}
	if !strings.Contains(ev.Fingerprint[0], "panicker") {
		t.Fatalf("fingerprint = %v", ev.Fingerprint)
	}
	if ev.Tags["origine"] != "server" {
		t.Fatal("origine")
	}
}

func TestCaptureBrowser(t *testing.T) {
	tr, events := NewForTest(t)
	tr.CaptureBrowser(BrowserError{
		Message: "x is undefined", File: "/static/js/dashboard.js", Line: 10, Col: 3,
		Stack: "at f (/static/js/dashboard.js:10:3) mario@example.it" + strings.Repeat("a", 9000),
		Page:  "/ticket/12", UserAgent: "Mozilla/5.0",
	}, "")
	ev := one(t, events())
	if ev.Tags["origine"] != "browser" || ev.Tags["pagina"] != "/ticket/{id}" {
		t.Fatalf("tags = %v", ev.Tags)
	}
	if ev.Fingerprint[0] != "x is undefined|/static/js/dashboard.js|10" {
		t.Fatalf("fingerprint = %v", ev.Fingerprint)
	}
	stack, _ := ev.Contexts["dettagli"]["stack"].(string)
	if strings.Contains(stack, "@example.it") || len(stack) > maxStack+len("…") {
		t.Fatalf("stack non ripulito o troppo lungo (%d)", len(stack))
	}
}

func TestThrottleApplied(t *testing.T) {
	tr, events := NewForTest(t)
	for i := 0; i < 3; i++ {
		tr.CaptureMessage("m", "fp", nil, nil, "u1")
	}
	tr.CaptureMessage("m", "fp", nil, nil, "u2")
	if n := len(events()); n != 2 {
		t.Fatalf("eventi = %d, want 2", n)
	}
}

func one(t *testing.T, evs []*sentryEvent) *sentryEvent {
	t.Helper()
	if len(evs) != 1 {
		t.Fatalf("eventi = %d, want 1", len(evs))
	}
	return evs[0]
}
