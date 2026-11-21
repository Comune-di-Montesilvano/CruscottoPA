package web

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/errtrack"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// newTrackedServer: server di prova con un tracker che registra gli eventi
// e il logger di default che passa dal tracker (ripristinato alla fine).
func newTrackedServer(t *testing.T) (*Server, func() []*sentry.Event) {
	t.Helper()
	tr, events := errtrack.NewForTest(t)
	s, _ := newTestServerWith(t, nil, func(o *Options) { o.Errors = tr })
	prev := slog.Default()
	slog.SetDefault(slog.New(tr.Handler(slog.NewTextHandler(&bytes.Buffer{}, nil))))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return s, events
}

func TestPanicReported(t *testing.T) {
	s, events := newTrackedServer(t)
	s.mux.HandleFunc("GET /panico-test", func(http.ResponseWriter, *http.Request) {
		panic("boom mario@example.it")
	})
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	rec := do(t, s, "GET", "/panico-test?x=1", nil, c, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	evs := events()
	if len(evs) != 1 {
		t.Fatalf("eventi = %d, want 1 (panic non duplicato dal log)", len(evs))
	}
	ev := evs[0]
	if len(ev.Exception) != 1 || ev.Exception[0].Value != "boom <mail>" {
		t.Fatalf("exception = %+v", ev.Exception)
	}
	if ev.Request == nil || ev.Request.URL != "/panico-test" {
		t.Fatalf("request = %+v", ev.Request)
	}
	if ev.User.ID != s.errs.UserID("mrossi") || ev.User.ID == "" {
		t.Fatalf("user = %+v", ev.User)
	}
}

func TestPanicAbortHandler(t *testing.T) {
	s, events := newTrackedServer(t)
	s.mux.HandleFunc("GET /abort-test", func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	})
	func() {
		defer func() {
			if v := recover(); v != http.ErrAbortHandler {
				t.Fatalf("recover = %v, want ErrAbortHandler", v)
			}
		}()
		do(t, s, "GET", "/abort-test", nil, nil, nil)
	}()
	if n := len(events()); n != 0 {
		t.Fatalf("eventi = %d", n)
	}
}

func serverErrorA(s *Server, w http.ResponseWriter) { s.serverError(w, errors.New("a")) }
func serverErrorB(s *Server, w http.ResponseWriter) { s.serverError(w, errors.New("b")) }

func TestServerErrorFingerprint(t *testing.T) {
	s, events := newTrackedServer(t)
	s.mux.HandleFunc("GET /err-a", func(w http.ResponseWriter, _ *http.Request) { serverErrorA(s, w) })
	s.mux.HandleFunc("GET /err-b", func(w http.ResponseWriter, _ *http.Request) { serverErrorB(s, w) })
	do(t, s, "GET", "/err-a", nil, nil, nil)
	do(t, s, "GET", "/err-b", nil, nil, nil)
	evs := events()
	if len(evs) != 2 {
		t.Fatalf("eventi = %d, want 2", len(evs))
	}
	a, b := evs[0].Fingerprint[0], evs[1].Fingerprint[0]
	if a == b || !strings.Contains(a, "serverErrorA") || !strings.Contains(b, "serverErrorB") {
		t.Fatalf("fingerprint = %q, %q", a, b)
	}
	if strings.Contains(a, ".serverError|") || strings.HasSuffix(a, ".serverError") {
		t.Fatalf("fingerprint col frame di serverError: %q", a)
	}
}

func renderA(s *Server, w http.ResponseWriter) { s.render(w, 200, "inesistente-a", nil) }
func renderB(s *Server, w http.ResponseWriter) { s.render(w, 200, "inesistente-b", nil) }

// Errori di template: una issue per template e per punto del codice, non una
// sola per tutti (il PC sarebbe sempre quello di render).
func TestTemplateErrorFingerprint(t *testing.T) {
	s, events := newTrackedServer(t)
	s.mux.HandleFunc("GET /tpl-a", func(w http.ResponseWriter, _ *http.Request) { renderA(s, w) })
	s.mux.HandleFunc("GET /tpl-b", func(w http.ResponseWriter, _ *http.Request) { renderB(s, w) })
	do(t, s, "GET", "/tpl-a", nil, nil, nil)
	do(t, s, "GET", "/tpl-b", nil, nil, nil)
	evs := events()
	if len(evs) != 2 {
		t.Fatalf("eventi = %d, want 2", len(evs))
	}
	a, b := evs[0].Fingerprint[0], evs[1].Fingerprint[0]
	if !strings.Contains(a, "inesistente-a") || !strings.Contains(a, "renderA") || !strings.Contains(b, "renderB") {
		t.Fatalf("fingerprint = %q, %q", a, b)
	}
}
