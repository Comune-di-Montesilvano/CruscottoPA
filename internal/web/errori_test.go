package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

func postErrori(t *testing.T, s *Server, body string, c *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/errori", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if c != nil {
		req.AddCookie(c)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

const erroreJS = `{"message":"x is undefined","stack":"at f (/static/js/dashboard.js:10:3)","file":"/static/js/dashboard.js","line":10,"col":3,"page":"/ticket/5","ua":"Mozilla/5.0"}`

func TestErroriReported(t *testing.T) {
	s, events := newTrackedServer(t)
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	rec := postErrori(t, s, erroreJS, c, nil)
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"ok":true}` {
		t.Fatalf("risposta = %d %s", rec.Code, rec.Body)
	}
	evs := events()
	if len(evs) != 1 {
		t.Fatalf("eventi = %d", len(evs))
	}
	ev := evs[0]
	if ev.Tags["origine"] != "browser" || ev.Tags["pagina"] != "/ticket/{id}" {
		t.Fatalf("tags = %v", ev.Tags)
	}
	if ev.User.ID != s.errs.UserID("mrossi") {
		t.Fatalf("user = %+v", ev.User)
	}
}

func TestErroriAbsoluteSameHost(t *testing.T) {
	s, events := newTrackedServer(t)
	body := strings.Replace(erroreJS, `"file":"/static/js/dashboard.js"`, `"file":"http://example.com/static/js/dashboard.js?v=2"`, 1)
	postErrori(t, s, body, nil, nil) // httptest: Host = example.com
	evs := events()
	if len(evs) != 1 {
		t.Fatalf("eventi = %d", len(evs))
	}
	if f := evs[0].Contexts["dettagli"]["file"]; f != "/static/js/dashboard.js" {
		t.Fatalf("file = %v", f)
	}
}

func TestErroriAlways200(t *testing.T) {
	s, events := newTrackedServer(t)
	for _, body := range []string{
		"{non json",
		`{"message":"","file":"/static/js/a.js"}`,
		`{"message":"` + strings.Repeat("a", 17<<10) + `"}`,
	} {
		rec := postErrori(t, s, body, nil, nil)
		if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"ok":false}` {
			t.Fatalf("risposta = %d %s", rec.Code, rec.Body)
		}
	}
	if n := len(events()); n != 0 {
		t.Fatalf("eventi = %d", n)
	}
}

func TestErroriFiltered(t *testing.T) {
	s, events := newTrackedServer(t)
	for _, body := range []string{
		`{"message":"Script error.","file":"","line":0}`,
		`{"message":"x","file":"chrome-extension://abc/x.js","line":1}`,
		`{"message":"x","file":"/static/js/a.js","line":1,"stack":"at moz-extension://abc/y.js:1:1"}`,
		`{"message":"x","file":"https://altro.example.org/x.js","line":1}`,
	} {
		if rec := postErrori(t, s, body, nil, nil); rec.Code != 200 {
			t.Fatalf("status = %d", rec.Code)
		}
	}
	if n := len(events()); n != 0 {
		t.Fatalf("eventi = %d", n)
	}
}

func TestErroriDisabled(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := postErrori(t, s, erroreJS, nil, nil)
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"ok":true}` {
		t.Fatalf("risposta = %d %s", rec.Code, rec.Body)
	}
}

func TestErroriCrossOrigin(t *testing.T) {
	s, events := newTrackedServer(t)
	rec := postErrori(t, s, erroreJS, nil, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(events()) != 0 {
		t.Fatal("evento da un altro sito")
	}
}

func TestErroriScriptIncluded(t *testing.T) {
	const tag = `<script src="/static/js/errori.js"></script>`
	s, _ := newTrackedServer(t)
	for _, page := range []struct {
		url    string
		cookie *http.Cookie
	}{{"/", nil}, {"/admin", login(t, s)}} {
		body := do(t, s, "GET", page.url, nil, page.cookie, nil).Body.String()
		i, j := strings.Index(body, tag), strings.Index(body, "htmx.min.js")
		if i < 0 || j < 0 || i > j {
			t.Fatalf("%s: errori.js assente o dopo htmx (%d, %d)", page.url, i, j)
		}
	}
	off, _ := newTestServer(t, nil)
	if strings.Contains(do(t, off, "GET", "/", nil, nil, nil).Body.String(), "errori.js") {
		t.Fatal("errori.js incluso con GlitchTip spento")
	}
}

func TestErroriJS(t *testing.T) {
	b, err := os.ReadFile("../../web/static/js/errori.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, want := range []string{"unhandledrejection", "sendBeacon", "keepalive", "chrome-extension://", "moz-extension://", "Script error.", "MAX_INVII = 10", "location.pathname"} {
		if !strings.Contains(js, want) {
			t.Errorf("errori.js senza %q", want)
		}
	}
	if strings.Contains(js, "location.search") || strings.Contains(js, "location.href") {
		t.Error("errori.js non deve mandare query o URL completo")
	}
}

func TestErroriProxyHost(t *testing.T) {
	s, events := newTrackedServer(t)
	body := strings.Replace(erroreJS, `"file":"/static/js/dashboard.js"`, `"file":"https://cruscotto.example.it/static/js/dashboard.js"`, 1)
	req := httptest.NewRequest("POST", "/errori", strings.NewReader(body))
	req.Host = "cruscottopa:8080" // il proxy può passare il nome dell'upstream
	req.Header.Set("Origin", "https://cruscotto.example.it")
	req.Header.Set("Sec-Fetch-Site", "same-origin") // come i browser veri
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	evs := events()
	if len(evs) != 1 || evs[0].Contexts["dettagli"]["file"] != "/static/js/dashboard.js" {
		t.Fatalf("eventi = %d", len(evs))
	}
}

func TestErroriBeaconTextPlain(t *testing.T) {
	b, _ := os.ReadFile("../../web/static/js/errori.js")
	if !strings.Contains(string(b), `type: "text/plain"`) || strings.Contains(string(b), `type: "application/json"`) {
		t.Fatal("sendBeacon deve usare un Blob text/plain (tipo ammesso senza CORS)")
	}
}
