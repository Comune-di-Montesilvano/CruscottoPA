package web

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity/ntlmtest"
)

func ntlmHeader(msg []byte) map[string]string {
	return map[string]string{"Authorization": "NTLM " + base64.StdEncoding.EncodeToString(msg)}
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestIoHandshake(t *testing.T) {
	s, _ := newTestServer(t, nil)

	rec := do(t, s, "GET", "/io", nil, nil, nil)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") != "NTLM" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("passo 0: %d %v", rec.Code, rec.Header())
	}
	if c := cookieNamed(rec, identity.CookieName); c == nil || c.MaxAge != int(identity.AnonymousTTL.Seconds()) {
		t.Fatal("passo 0: atteso il cookie anonimo da 24 ore")
	}

	rec = do(t, s, "GET", "/io", nil, nil, ntlmHeader(ntlmtest.Negotiate()))
	if rec.Code != http.StatusUnauthorized || !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "NTLM ") {
		t.Fatalf("tipo 1: %d %q", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}

	rec = do(t, s, "GET", "/io", nil, nil, ntlmHeader(ntlmtest.Authenticate("comune-ms", "MRossi", "PC-1")))
	c := cookieNamed(rec, identity.CookieName)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"riconosciuto":true`) || !strings.Contains(rec.Body.String(), `"nome":"Mario"`) || c == nil {
		t.Fatalf("tipo 3: %d %s", rec.Code, rec.Body)
	}
	if !c.HttpOnly || c.Path != "/" || c.SameSite != http.SameSiteLaxMode || c.MaxAge != int(identity.UserTTL.Seconds()) {
		t.Fatalf("attributi del cookie: %+v", c)
	}
	if u, ok := s.cookies.Decode(c.Value); !ok || u.Name != "Mario Rossi" || u.Anonymous {
		t.Fatalf("contenuto del cookie: %+v %v", u, ok)
	}
}

func TestIoRejections(t *testing.T) {
	s, _ := newTestServer(t, nil)
	anon := func(rec *httptest.ResponseRecorder) bool {
		c := cookieNamed(rec, identity.CookieName)
		if c == nil {
			return false
		}
		u, ok := s.cookies.Decode(c.Value)
		return ok && u.Anonymous
	}
	if rec := do(t, s, "GET", "/io", nil, nil, ntlmHeader(ntlmtest.Authenticate("ALTRO", "mrossi", "W"))); rec.Code != 200 || !anon(rec) {
		t.Fatalf("dominio sbagliato: %d", rec.Code)
	}
	if rec := do(t, s, "GET", "/io", nil, nil, ntlmHeader(ntlmtest.Authenticate("COMUNE-MS", "sconosciuto", "W"))); rec.Code != 200 || !anon(rec) {
		t.Fatalf("utente sconosciuto: %d", rec.Code)
	}
	for _, h := range []map[string]string{
		{"Authorization": "NTLM !!!"},
		{"Authorization": "Basic eDp5"},
		ntlmHeader([]byte("NTLMSSP\x00\x03\x00\x00\x00troppo corto")),
	} {
		if rec := do(t, s, "GET", "/io", nil, nil, h); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: atteso 400, ottenuto %d", h, rec.Code)
		}
	}
}

func TestIoLDAPDown(t *testing.T) {
	s, _ := newTestServerWith(t, nil, func(o *Options) { o.Directory = fakeDirectory{err: errors.New("giù")} })
	rec := do(t, s, "GET", "/io", nil, nil, ntlmHeader(ntlmtest.Authenticate("COMUNE-MS", "mrossi", "W")))
	if c := cookieNamed(rec, identity.CookieName); rec.Code != http.StatusServiceUnavailable || (c != nil && c.MaxAge >= 0) {
		t.Fatalf("LDAP giù: atteso 503 senza cookie valido, ottenuto %d", rec.Code)
	}
}

func TestIoDisabled(t *testing.T) {
	s, _ := newTestServerWith(t, nil, func(o *Options) { o.Config.NTLMDomain = "" })
	if rec := do(t, s, "GET", "/io", nil, nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("riconoscimento spento: %d", rec.Code)
	}
}

// Identità dichiarata: il cookie utente non deve mai aprire l'admin.
func TestViewerCookieDoesNotOpenAdmin(t *testing.T) {
	s, _ := newTestServer(t, nil)
	v, _ := s.cookies.Encode(identity.User{Username: "mrossi", Name: "Mario Rossi"})
	rec := do(t, s, "GET", "/admin", nil, &http.Cookie{Name: identity.CookieName, Value: v}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("/admin con il solo cookie utente: atteso 303, ottenuto %d", rec.Code)
	}
}

func TestDashboardGreetsRecognizedUser(t *testing.T) {
	s, _ := newTestServer(t, nil)
	v, _ := s.cookies.Encode(identity.User{Username: "mrossi", Name: "Mario Rossi"})
	body := do(t, s, "GET", "/", nil, &http.Cookie{Name: identity.CookieName, Value: v}, nil).Body.String()
	if !strings.Contains(body, `<span class="hello-name">, Mario</span>`) || strings.Contains(body, "data-riconosci") {
		t.Fatal("utente riconosciuto: saluto per nome e nessun nuovo tentativo")
	}
	v, _ = s.cookies.Encode(identity.User{Username: "senzanome"})
	if body := do(t, s, "GET", "/", nil, &http.Cookie{Name: identity.CookieName, Value: v}, nil).Body.String(); !strings.Contains(body, `<span class="hello-name"></span>`) {
		t.Fatal("senza nome visualizzato: solo il saluto")
	}
}

func TestDashboardRecognizeAttribute(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if body := do(t, s, "GET", "/", nil, nil, nil).Body.String(); !strings.Contains(body, "data-riconosci") {
		t.Fatal("senza cookie: data-riconosci atteso")
	}
	v, _ := s.cookies.Encode(identity.User{Anonymous: true})
	if body := do(t, s, "GET", "/", nil, &http.Cookie{Name: identity.CookieName, Value: v}, nil).Body.String(); strings.Contains(body, "data-riconosci") {
		t.Fatal("con il cookie anonimo non si ritenta")
	}
	s2, _ := newTestServerWith(t, nil, func(o *Options) { o.Config.NTLMDomain = "" })
	if body := do(t, s2, "GET", "/", nil, nil, nil).Body.String(); strings.Contains(body, "data-riconosci") {
		t.Fatal("riconoscimento spento: nessun tentativo")
	}
}

// Cookie Secure su HTTP in chiaro: il browser lo scarterebbe e la pagina si
// ricaricherebbe all'infinito. Nessun tentativo di riconoscimento.
func TestRecognitionSkippedWhenCookieDropped(t *testing.T) {
	s, _ := newTestServerWith(t, nil, func(o *Options) { o.Config.SecureCookies = true })
	if body := do(t, s, "GET", "/", nil, nil, nil).Body.String(); strings.Contains(body, "data-riconosci") {
		t.Fatal("cookie che verrebbe scartato: niente data-riconosci")
	}
	if rec := do(t, s, "GET", "/io", nil, nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("/io con cookie che verrebbe scartato: atteso 404, ottenuto %d", rec.Code)
	}
	https := map[string]string{"X-Forwarded-Proto": "https"}
	if body := do(t, s, "GET", "/", nil, nil, https).Body.String(); !strings.Contains(body, "data-riconosci") {
		t.Fatal("dietro proxy HTTPS il riconoscimento deve restare attivo")
	}
}

// AD giù a metà handshake: il cookie anonimo del primo passo va tolto,
// altrimenti l'utente resterebbe anonimo per 24 ore.
func TestIoLDAPDownExpiresAnonymousCookie(t *testing.T) {
	s, _ := newTestServerWith(t, nil, func(o *Options) { o.Directory = fakeDirectory{err: errors.New("giù")} })
	rec := do(t, s, "GET", "/io", nil, nil, ntlmHeader(ntlmtest.Authenticate("COMUNE-MS", "mrossi", "W")))
	c := cookieNamed(rec, identity.CookieName)
	if rec.Code != http.StatusServiceUnavailable || c == nil || c.MaxAge >= 0 {
		t.Fatalf("LDAP giù: atteso 503 con il cookie scaduto, ottenuto %d %+v", rec.Code, c)
	}
}
