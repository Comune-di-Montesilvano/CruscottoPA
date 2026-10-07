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

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/auth"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/backup"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

var fixedNow = time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC) // 10:00 a Roma

type fakeAuth struct {
	ok, admin bool
	err       error
}

func (f fakeAuth) Authenticate(_, _ string) (bool, bool, error) { return f.ok, f.admin, f.err }

type fakeDirectory struct {
	people   map[string]identity.Person
	profiles map[string]audience.Profile
	groups   []identity.ADGroup
	values   map[string][]string
	members  []identity.Person
	err      error
	calls    *int // conta le chiamate a Profile (test della cache)
}

func (f fakeDirectory) Profile(u string, _ []string) (audience.Profile, error) {
	if f.calls != nil {
		*f.calls++
	}
	if f.err != nil {
		return audience.Profile{}, f.err
	}
	p, ok := f.profiles[strings.ToLower(u)]
	if !ok {
		return audience.Profile{}, identity.ErrUnknownUser
	}
	return p, nil
}

func (f fakeDirectory) SearchGroups(string) ([]identity.ADGroup, error) { return f.groups, f.err }
func (f fakeDirectory) SearchUsers(string) ([]identity.Person, error)   { return f.members, f.err }
func (f fakeDirectory) AttributeValues(a string) ([]string, error)      { return f.values[a], f.err }

// Members: come AD, un gruppo senza regole positive non ha membri.
func (f fakeDirectory) Members(rules []audience.Rule, _ []string) (int, []identity.Person, error) {
	if f.err != nil {
		return 0, nil, f.err
	}
	for _, r := range rules {
		if r.Kind != audience.KindExclude {
			return len(f.members), f.members, nil
		}
	}
	return 0, []identity.Person{}, nil
}

func (f fakeDirectory) AttributeStats(q string) ([]identity.AttrStat, error) {
	if f.err != nil {
		return nil, f.err
	}
	all := []identity.AttrStat{{Name: "physicalDeliveryOfficeName", Count: 218, Examples: []string{"POLIZIA LOCALE", "TRIBUTI"}}}
	if q != "" && !strings.Contains(strings.ToLower(all[0].Name), strings.ToLower(q)) {
		return []identity.AttrStat{}, nil
	}
	return all, nil
}

func (f fakeDirectory) Lookup(u string) (identity.Person, error) {
	if f.err != nil {
		return identity.Person{}, f.err
	}
	p, ok := f.people[strings.ToLower(u)]
	if !ok {
		return identity.Person{}, identity.ErrUnknownUser
	}
	return p, nil
}

var testDirectory = fakeDirectory{
	people: map[string]identity.Person{
		"mrossi":    {Username: "mrossi", Name: "Mario Rossi"},
		"senzanome": {Username: "senzanome"},
	},
	profiles: map[string]audience.Profile{
		"mrossi":    {Username: "mrossi", Attrs: map[string][]string{"physicaldeliveryofficename": {"TRIBUTI"}}, Groups: []string{"CN=SHARE_TRIBUTI_RW,DC=test"}},
		"senzanome": {Username: "senzanome", Attrs: map[string][]string{}},
	},
	groups:  []identity.ADGroup{{DN: "CN=SHARE_TRIBUTI_RW,DC=test", Name: "SHARE_TRIBUTI_RW"}},
	values:  map[string][]string{"physicalDeliveryOfficeName": {"LLPP", "TRIBUTI"}},
	members: []identity.Person{{Username: "mrossi", Name: "Mario Rossi", GivenName: "Mario", Attrs: map[string]string{"physicalDeliveryOfficeName": "TRIBUTI"}}},
}

// serverExits raccoglie le chiamate a exit del servizio backup di ogni server di test.
var serverExits = map[*Server]chan int{}

func newTestServer(t *testing.T, a auth.Authenticator) (*Server, *database.DB) {
	t.Helper()
	return newTestServerWith(t, a, nil)
}

// newTestServerWith è newTestServer con la possibilità di ritoccare le Options.
func newTestServerWith(t *testing.T, a auth.Authenticator, edit func(*Options)) (*Server, *database.DB) {
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
	o := Options{
		DB:           db,
		Config:       config.Config{SessionSecret: strings.Repeat("s", 32), DBPath: dbPath, UploadDir: uploadDir, Location: rome, NTLMDomain: "COMUNE-MS"},
		Auth:         a,
		Directory:    testDirectory,
		Backup:       bk,
		RestoreDelay: time.Nanosecond,
		Version:      "test",
		WebDir:       "../../web",
		Now:          func() time.Time { return fixedNow },
	}
	if edit != nil {
		edit(&o)
	}
	s, err := New(o)
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

// Dopo un aggiornamento il browser non deve usare JS/CSS vecchi dalla cache:
// gli statici si rivalidano sempre (304 se invariati).
func TestStaticRevalidated(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(t, s, "GET", "/static/js/dashboard.js", nil, nil, nil)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("statici: %d Cache-Control=%q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

// /static/ non deve elencare il contenuto delle cartelle.
func TestStaticNoDirectoryListing(t *testing.T) {
	s, _ := newTestServer(t, nil)
	for _, p := range []string{"/static/", "/static/js/", "/static/img/"} {
		if rec := do(t, s, "GET", p, nil, nil, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: atteso 404, ottenuto %d", p, rec.Code)
		}
	}
	if rec := do(t, s, "GET", "/static/js/dashboard.js", nil, nil, nil); rec.Code != 200 {
		t.Fatalf("file statico: %d", rec.Code)
	}
}
