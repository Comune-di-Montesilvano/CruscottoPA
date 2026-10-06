package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/auth"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/backup"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

var fixedNow = time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC) // 10:00 a Roma

type fakeAuth struct {
	ok, admin bool
	err       error
}

func (f fakeAuth) Authenticate(_, _ string) (bool, bool, error) { return f.ok, f.admin, f.err }

// serverExits raccoglie le chiamate a exit del servizio backup di ogni server di test.
var serverExits = map[*Server]chan int{}

func newTestServer(t *testing.T, a auth.Authenticator) (*Server, *database.DB) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	uploadDir := filepath.Join(dir, "uploads")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	rome, _ := time.LoadLocation("Europe/Rome")
	if a == nil {
		a = fakeAuth{ok: true, admin: true}
	}
	exits := make(chan int, 4)
	bk, err := backup.New(backup.Options{
		Store: db, DBPath: dbPath, UploadDir: uploadDir, AppVersion: "test",
		MaxSchema: database.CurrentSchemaVersion(), Location: rome,
		Now:  func() time.Time { return fixedNow },
		Exit: func(code int) { exits <- code },
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{
		DB:           db,
		Config:       config.Config{SessionSecret: strings.Repeat("s", 32), DBPath: dbPath, UploadDir: uploadDir, Location: rome},
		Auth:         a,
		Backup:       bk,
		RestoreDelay: time.Nanosecond,
		Version:      "test",
		WebDir:       "../../web",
		Now:          func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	serverExits[s] = exits
	return s, db
}

// do esegue una richiesta sul handler completo (middleware inclusi).
func do(t *testing.T, s *Server, method, target string, form url.Values, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(t, s, "GET", "/health", nil, nil, nil)
	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if rec.Code != 200 || body["status"] != "ok" || body["version"] != "test" {
		t.Fatalf("health: %d %v", rec.Code, body)
	}
}

func TestSecurityHeaders(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(t, s, "GET", "/", nil, nil, nil)
	if got := rec.Header().Get("Content-Security-Policy"); got != cspPolicy {
		t.Fatalf("CSP: %q", got)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatalf("header mancanti: %v", rec.Header())
	}
}

func TestCrossOriginPostRejected(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(t, s, "POST", "/admin/login", url.Values{"username": {"x"}}, nil,
		map[string]string{"Sec-Fetch-Site": "cross-site"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST cross-origin: atteso 403, ottenuto %d", rec.Code)
	}
}

func TestStaticServed(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if rec := do(t, s, "GET", "/static/js/htmx.min.js", nil, nil, nil); rec.Code != 200 {
		t.Fatalf("static: %d", rec.Code)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func url_(k, v string) url.Values { return url.Values{k: {v}} }
