package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func login(t *testing.T, s *Server) *http.Cookie {
	t.Helper()
	rec := do(t, s, "POST", "/admin/login", url.Values{"username": {"mrossi"}, "password": {"pw"}}, nil, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login: %d\n%s", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionName {
			return c
		}
	}
	t.Fatal("cookie di sessione mancante")
	return nil
}

func sessionCookie(rec interface{ Result() *http.Response }) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionName {
			return c
		}
	}
	return nil
}

func TestAdminRequiresLogin(t *testing.T) {
	s, _ := newTestServer(t, nil)

	rec := do(t, s, "GET", "/admin", nil, nil, nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("senza sessione: atteso 303 → /admin/login, ottenuto %d %q", rec.Code, rec.Header().Get("Location"))
	}

	rec = do(t, s, "GET", "/admin", nil, nil, map[string]string{"HX-Request": "true"})
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("HX-Redirect") != "/admin/login" {
		t.Fatalf("HTMX senza sessione: atteso 401 + HX-Redirect, ottenuto %d %v", rec.Code, rec.Header())
	}
}

func TestLoginAndOverview(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	if !c.HttpOnly || c.Path != "/admin" || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("attributi cookie: %+v", c)
	}

	rec := do(t, s, "GET", "/admin", nil, c, nil)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Panoramica") || !strings.Contains(body, "mrossi") {
		t.Fatalf("panoramica: %d\n%s", rec.Code, body)
	}
	if !strings.Contains(body, "Applicativi da completare") || !strings.Contains(body, "Rubrica") {
		t.Fatal("le app del seed senza URL devono comparire come da completare")
	}

	// Già autenticato: la pagina di login rimanda al pannello.
	if rec := do(t, s, "GET", "/admin/login", nil, c, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("login con sessione attiva: %d", rec.Code)
	}
}

func TestSecureCookieBehindHTTPSProxy(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(t, s, "POST", "/admin/login", url.Values{"username": {"mrossi"}, "password": {"pw"}}, nil,
		map[string]string{"X-Forwarded-Proto": "https"})
	if c := sessionCookie(rec); c == nil || !c.Secure {
		t.Fatalf("dietro proxy HTTPS il cookie deve essere Secure: %+v", c)
	}
}

func TestLoginNonAdminForbidden(t *testing.T) {
	s, _ := newTestServer(t, fakeAuth{ok: true, admin: false})
	rec := do(t, s, "POST", "/admin/login", url.Values{"username": {"gbianchi"}, "password": {"pw"}}, nil, nil)
	if rec.Code != http.StatusForbidden || sessionCookie(rec) != nil {
		t.Fatalf("non admin: atteso 403 senza cookie, ottenuto %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "non è amministratore") {
		t.Fatal("messaggio di accesso non autorizzato mancante")
	}
}

func TestLoginRateLimited(t *testing.T) {
	s, _ := newTestServer(t, fakeAuth{ok: false})
	form := url.Values{"username": {"mrossi"}, "password": {"sbagliata"}}
	for i := 0; i < 5; i++ {
		if rec := do(t, s, "POST", "/admin/login", form, nil, nil); rec.Code != http.StatusUnauthorized {
			t.Fatalf("tentativo %d: atteso 401, ottenuto %d", i+1, rec.Code)
		}
	}
	rec := do(t, s, "POST", "/admin/login", form, nil, nil)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("6° tentativo: atteso 429 con Retry-After, ottenuto %d", rec.Code)
	}
}

func TestLogout(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	rec := do(t, s, "POST", "/admin/logout", nil, c, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logout: %d", rec.Code)
	}
	if out := sessionCookie(rec); out == nil || out.MaxAge >= 0 {
		t.Fatalf("logout deve scadere il cookie: %+v", out)
	}
}
