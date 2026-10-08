# Apertura ticket verso OTRS — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** tile «Apri un ticket» in plancia con un dialog che apre un ticket in OTRS (via GenericInterface REST), con allegati, registro locale e pagina admin.

**Architecture:** nuovo pacchetto `internal/otrs` (unico che conosce OTRS: `Client` HTTP + `Mock`); migrazione v11 con `tickets_sent`; in `internal/web` upload a pezzi pubblici per utenti riconosciuti (`ticket_uploads.go`), invio `POST /ticket` (`ticket.go`), pagina `/admin/ticket` (`admin_ticket.go`); in plancia template `ticket_tile`/`ticket_dialog` e `web/static/js/ticket.js`.

**Tech Stack:** Go 1.27 (`net/http`, `encoding/json`), SQLite `modernc.org/sqlite`, `html/template`, JS vanilla (CSP stretta), Playwright in container per la prova del dialog.

**Spec:** `docs/superpowers/specs/2026-10-08-ticket-otrs-design.md`

## Global Constraints

- Tutte le risposte dei flussi pubblici del ticket sono **HTTP 200 + JSON** `{"ok":…}` (il proxy sostituisce 4xx/5xx).
- Nessun dato dell'ente nel codice o nei test: host, nome del web service, coda, agente arrivano solo da variabili d'ambiente (`OTRS_URL`, `OTRS_ROUTE_CREATE`, `OTRS_ROUTE_UPDATE`, `OTRS_USER`, `OTRS_PASSWORD`, `OTRS_QUEUE`, `OTRS_FALLBACK_EMAIL`). Nei test usare `example.it` e nomi inventati.
- Nuova env var = tre posti: `docker-compose.yml` (`environment:` con `${VAR}`), `.env.example`, `internal/config`.
- `OTRS_PASSWORD` e `UserLogin` mai nei log né negli errori restituiti.
- Migrazioni: aggiungere `migrateV11TicketsSent` in coda, mai toccare le precedenti.
- CSP: niente `<script>`/`<style>` inline né `on*=`; nuovo JS in `web/static/js/ticket.js`.
- Allegati: PNG, JPEG, WebP, PDF (tipo dal contenuto); 5 MB per file, 3 file, 10 MB totali; pezzi da 512 KB.
- Limiti testo: oggetto 120, descrizione 10000, telefono 40 caratteri (rune).
- Limite: 5 ticket per utente nell'ultima ora.
- Valori fissi OTRS: `State:"new"`, `Priority:"3 normal"`, `ArticleType:"webrequest"`, `SenderType:"customer"`, `ContentType:"text/plain; charset=utf8"`.
- Test di `internal/web` in container (su Windows `go test ./internal/web/` mente): `MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)":/src -v "$(pwd -W)/.devcache":/cache -e GOPATH=/cache/gopath -e GOCACHE=/cache/build -e CGO_ENABLED=0 -w /src golang:1.27-alpine go test ./internal/web/ -run <Nome>`.
- Versione: 0.12.0 (`publiccode.yml` `softwareVersion`/`releaseDate`).

## Review Focus

- Nome e mail inviati dal form diversi da quelli di AD: devono vincere sempre quelli di AD (test in Task 5).
- Upload di un allegato da parte di un altro utente o riuso dello stesso id: rifiutati (test in Task 4 e 5).
- OTRS che risponde con la pagina HTML del proxy (500) o con `{"Error"}`: messaggio generico, nessuna riga nel registro, temporanei cancellati (test in Task 2 e 5).
- Profilo AD senza `mail`: niente modulo, spiegazione (test in Task 5).
- Testo con accenti e a capo nella descrizione: arriva a OTRS identico in UTF-8 (test in Task 2).

---

### Task 1: Configurazione OTRS

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `docker-compose.yml` (blocco `environment:`)
- Modify: `.env.example`

**Interfaces:**
- Produces: `config.OTRS{URL, RouteCreate, RouteUpdate, User, Password, Queue, FallbackEmail string}`, campo `Config.OTRS`, metodo `func (o OTRS) Enabled() bool` (URL non vuota), `func (o OTRS) Mock() bool` (URL == "mock").

- [ ] **Step 1: Test che falliscono** — in `config_test.go` aggiungere le variabili a `allVars` e i test:

```go
// in allVars aggiungere:
"OTRS_URL", "OTRS_ROUTE_CREATE", "OTRS_ROUTE_UPDATE", "OTRS_USER", "OTRS_PASSWORD", "OTRS_QUEUE", "OTRS_FALLBACK_EMAIL",

func TestLoadOTRSDisabledByDefault(t *testing.T) {
	clearEnv(t)
	t.Setenv("LDAP_HOST", "mock")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OTRS.Enabled() || cfg.OTRS.RouteCreate != "/TicketCreate" || cfg.OTRS.RouteUpdate != "/TicketUpdate" {
		t.Fatalf("default OTRS: %+v", cfg.OTRS)
	}
}

func TestLoadOTRSValidation(t *testing.T) {
	base := func(t *testing.T) {
		clearEnv(t)
		t.Setenv("LDAP_HOST", "ldaps://dc.example.local:636")
		t.Setenv("SESSION_SECRET", strings.Repeat("a", 32))
		t.Setenv("OTRS_URL", "https://otrs.example.it/otrs/nph-genericinterface.pl/Webservice/Prova")
		t.Setenv("OTRS_USER", "agente")
		t.Setenv("OTRS_PASSWORD", "segreta")
		t.Setenv("OTRS_QUEUE", "Coda di prova")
	}
	base(t)
	cfg, err := Load()
	if err != nil || !cfg.OTRS.Enabled() || cfg.OTRS.Mock() {
		t.Fatalf("config valida: %v %+v", err, cfg.OTRS)
	}
	for _, tc := range []struct{ key, val, want string }{
		{"OTRS_URL", "http://otrs.example.it/x", "https"},
		{"OTRS_USER", "", "OTRS_USER"},
		{"OTRS_PASSWORD", "", "OTRS_PASSWORD"},
		{"OTRS_QUEUE", "", "OTRS_QUEUE"},
		{"OTRS_ROUTE_CREATE", "TicketCreate", "OTRS_ROUTE_CREATE"},
		{"OTRS_ROUTE_UPDATE", "x", "OTRS_ROUTE_UPDATE"},
		{"OTRS_URL", "mock", "mock"}, // mock solo con LDAP_HOST=mock
	} {
		base(t)
		t.Setenv(tc.key, tc.val)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s=%q: atteso errore con %q, ottenuto %v", tc.key, tc.val, tc.want, err)
		}
	}
}

func TestLoadOTRSMock(t *testing.T) {
	clearEnv(t)
	t.Setenv("LDAP_HOST", "mock")
	t.Setenv("OTRS_URL", "mock")
	cfg, err := Load()
	if err != nil || !cfg.OTRS.Mock() || !cfg.OTRS.Enabled() {
		t.Fatalf("mock: %v %+v", err, cfg.OTRS)
	}
}

func TestLoadOTRSURLTrailingSlash(t *testing.T) {
	clearEnv(t)
	t.Setenv("LDAP_HOST", "mock")
	t.Setenv("OTRS_URL", "https://otrs.example.it/ws/Prova/")
	t.Setenv("OTRS_USER", "a")
	t.Setenv("OTRS_PASSWORD", "b")
	t.Setenv("OTRS_QUEUE", "c")
	cfg, err := Load()
	if err != nil || cfg.OTRS.URL != "https://otrs.example.it/ws/Prova" {
		t.Fatalf("slash finale non tolta: %v %q", err, cfg.OTRS.URL)
	}
}
```

- [ ] **Step 2: Verificare che falliscano** — `go test ./internal/config/` → FAIL (`cfg.OTRS undefined`).

- [ ] **Step 3: Implementazione** — in `config.go`:

```go
// OTRS: invio dei ticket dalla plancia (GenericInterface REST). URL vuota = modulo spento.
type OTRS struct {
	URL           string // base del web service, senza "/" finale; "mock" = client finto (solo sviluppo)
	RouteCreate   string // route POST di TicketCreate
	RouteUpdate   string // route PATCH di TicketUpdate
	User          string
	Password      string
	Queue         string
	FallbackEmail string // casella mostrata se OTRS non risponde
}

func (o OTRS) Enabled() bool { return o.URL != "" }
func (o OTRS) Mock() bool    { return o.URL == "mock" }
```

Campo in `Config`: `OTRS OTRS` (commento: `// OTRS: modulo ticket (vuoto = spento).`). In `Load`, dentro il letterale:

```go
		OTRS: OTRS{
			URL:           strings.TrimRight(strings.TrimSpace(os.Getenv("OTRS_URL")), "/"),
			RouteCreate:   getEnv("OTRS_ROUTE_CREATE", "/TicketCreate"),
			RouteUpdate:   getEnv("OTRS_ROUTE_UPDATE", "/TicketUpdate"),
			User:          os.Getenv("OTRS_USER"),
			Password:      os.Getenv("OTRS_PASSWORD"),
			Queue:         strings.TrimSpace(os.Getenv("OTRS_QUEUE")),
			FallbackEmail: strings.TrimSpace(os.Getenv("OTRS_FALLBACK_EMAIL")),
		},
```

Dopo i controlli LDAP/SESSION_SECRET, prima della generazione del segreto casuale:

```go
	if err := cfg.OTRS.validate(cfg.LDAP.Host == "mock"); err != nil {
		return Config{}, err
	}
```

```go
func (o OTRS) validate(ldapMock bool) error {
	if !o.Enabled() {
		return nil
	}
	if o.Mock() {
		if !ldapMock {
			return errors.New("OTRS_URL=mock ammesso solo con LDAP_HOST=mock")
		}
		return nil
	}
	if !strings.HasPrefix(o.URL, "https://") {
		return errors.New("OTRS_URL deve iniziare con https://")
	}
	for _, r := range []struct{ name, val string }{{"OTRS_ROUTE_CREATE", o.RouteCreate}, {"OTRS_ROUTE_UPDATE", o.RouteUpdate}} {
		if !strings.HasPrefix(r.val, "/") {
			return fmt.Errorf("%s deve iniziare con /", r.name)
		}
	}
	for _, r := range []struct{ name, val string }{{"OTRS_USER", o.User}, {"OTRS_PASSWORD", o.Password}, {"OTRS_QUEUE", o.Queue}} {
		if r.val == "" {
			return fmt.Errorf("%s obbligatorio quando OTRS_URL è impostato", r.name)
		}
	}
	return nil
}
```

`docker-compose.yml`, dopo `VAPID_SUBJECT`:

```yaml
      - OTRS_URL=${OTRS_URL}
      - OTRS_ROUTE_CREATE=${OTRS_ROUTE_CREATE:-/TicketCreate}
      - OTRS_ROUTE_UPDATE=${OTRS_ROUTE_UPDATE:-/TicketUpdate}
      - OTRS_USER=${OTRS_USER}
      - OTRS_PASSWORD=${OTRS_PASSWORD}
      - OTRS_QUEUE=${OTRS_QUEUE}
      - OTRS_FALLBACK_EMAIL=${OTRS_FALLBACK_EMAIL}
```

`.env.example`, dopo la sezione notifiche:

```
# ── Ticket di assistenza (OTRS) ────────────────────────────────────────────
# URL del web service REST (GenericInterface), senza / finale:
# https://<host>/otrs/nph-genericinterface.pl/Webservice/<nome>
# Vuoto = modulo spento (nessuna tile). "mock" solo con LDAP_HOST=mock.
# Meglio l'indirizzo interno di OTRS, non quello dietro il reverse proxy (limite 1 MB).
OTRS_URL=
# Route configurate nel web service (POST per create, PATCH per update)
OTRS_ROUTE_CREATE=/TicketCreate
OTRS_ROUTE_UPDATE=/TicketUpdate
# Agente dedicato con permessi create e rw sulla coda
OTRS_USER=
OTRS_PASSWORD=
# Nome esatto della coda (maiuscole e minuscole contano)
OTRS_QUEUE=
# Casella dell'assistenza mostrata se OTRS non risponde
OTRS_FALLBACK_EMAIL=
```

- [ ] **Step 4: Test verdi** — `go test ./internal/config/` → PASS.
- [ ] **Step 5: Commit** — `git add internal/config docker-compose.yml .env.example && git commit -m "Config: variabili OTRS per il modulo ticket"`.

---

### Task 2: Client OTRS

**Files:**
- Create: `internal/otrs/otrs.go`
- Create: `internal/otrs/mock.go`
- Test: `internal/otrs/otrs_test.go`

**Interfaces:**
- Consumes: `config.OTRS` (Task 1).
- Produces:
  - `type Client interface { Create(ctx context.Context, t NewTicket) (Created, error) }`
  - `type NewTicket struct { Name, Email, Phone, Subject, Body string; Attachments []Attachment }`
  - `type Attachment struct { Filename, ContentType string; Content []byte }`
  - `type Created struct { TicketID, TicketNumber string; CustomerSet bool }`
  - `func New(c config.OTRS) Client` (Mock se `c.Mock()`, altrimenti `*HTTPClient`)
  - `type HTTPClient struct { Config config.OTRS; HTTP *http.Client }`
  - `type Mock struct { mu sync.Mutex; Sent []NewTicket; Err error; FailUpdate bool }` con `Create`.
  - `var ErrOTRS = errors.New("OTRS non disponibile")` (tutti gli errori del client la avvolgono: `errors.Is(err, otrs.ErrOTRS)`).

- [ ] **Step 1: Test che falliscono** — `internal/otrs/otrs_test.go`:

```go
package otrs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

type call struct {
	Method, Path string
	Body         map[string]any
}

// fakeOTRS registra le chiamate; create/update decidono la risposta.
func fakeOTRS(t *testing.T, create, update http.HandlerFunc) (*HTTPClient, *[]call) {
	t.Helper()
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, call{r.Method, r.URL.Path, body})
		switch r.URL.Path {
		case "/ws/TicketCreate":
			create(w, r)
		case "/ws/TicketUpdate":
			update(w, r)
		default:
			http.Error(w, "<html>cortesia</html>", 500)
		}
	}))
	t.Cleanup(srv.Close)
	c := &HTTPClient{Config: config.OTRS{URL: srv.URL + "/ws", RouteCreate: "/TicketCreate", RouteUpdate: "/TicketUpdate",
		User: "agente", Password: "segreta-123", Queue: "Coda prova"}, HTTP: srv.Client()}
	return c, &calls
}

func jsonReply(v string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")
		io.WriteString(w, v)
	}
}

var okCreate = jsonReply(`{"ArticleID":"1","TicketNumber":"2026100800000011","TicketID":"42"}`)
var okUpdate = jsonReply(`{"TicketNumber":"2026100800000011","TicketID":"42"}`)

func sample() NewTicket {
	return NewTicket{Name: "Mario Rossi", Email: "mrossi@example.it", Phone: "731",
		Subject: "Stampante", Body: "Non stampa più.\nÈ urgente: perché?",
		Attachments: []Attachment{{Filename: "../../scheda.png", ContentType: "image/png", Content: []byte("PNGDATA")}}}
}

func TestCreateSendsExpectedRequests(t *testing.T) {
	c, calls := fakeOTRS(t, okCreate, okUpdate)
	got, err := c.Create(context.Background(), sample())
	if err != nil {
		t.Fatal(err)
	}
	if got != (Created{TicketID: "42", TicketNumber: "2026100800000011", CustomerSet: true}) {
		t.Fatalf("risultato: %+v", got)
	}
	if len(*calls) != 2 || (*calls)[0].Method != "POST" || (*calls)[1].Method != "PATCH" {
		t.Fatalf("chiamate: %+v", *calls)
	}
	cr := (*calls)[0].Body
	ticket := cr["Ticket"].(map[string]any)
	article := cr["Article"].(map[string]any)
	if cr["UserLogin"] != "agente" || cr["Password"] != "segreta-123" || ticket["Queue"] != "Coda prova" ||
		ticket["State"] != "new" || ticket["Priority"] != "3 normal" || ticket["CustomerUser"] != "mrossi@example.it" ||
		ticket["Title"] != "Stampante" {
		t.Fatalf("ticket: %v", cr)
	}
	if article["From"] != `"Mario Rossi" <mrossi@example.it>` || article["ArticleType"] != "webrequest" ||
		article["SenderType"] != "customer" || article["ContentType"] != "text/plain; charset=utf8" {
		t.Fatalf("articolo: %v", article)
	}
	if article["Body"] != "Non stampa più.\nÈ urgente: perché?\n\nTelefono / interno: 731" {
		t.Fatalf("corpo: %q", article["Body"])
	}
	att := cr["Attachment"].([]any)[0].(map[string]any)
	if att["Filename"] != "scheda.png" || att["ContentType"] != "image/png" || att["Content"] != base64.StdEncoding.EncodeToString([]byte("PNGDATA")) {
		t.Fatalf("allegato: %v", att)
	}
	up := (*calls)[1].Body
	upT := up["Ticket"].(map[string]any)
	if up["TicketID"] != "42" || upT["CustomerUser"] != "mrossi@example.it" || upT["CustomerID"] != "mrossi@example.it" {
		t.Fatalf("update: %v", up)
	}
}

func TestCreateWithoutPhoneOrAttachments(t *testing.T) {
	c, calls := fakeOTRS(t, okCreate, okUpdate)
	tk := sample()
	tk.Phone, tk.Attachments = "", nil
	if _, err := c.Create(context.Background(), tk); err != nil {
		t.Fatal(err)
	}
	cr := (*calls)[0].Body
	if cr["Article"].(map[string]any)["Body"] != tk.Body {
		t.Fatalf("corpo senza telefono: %q", cr["Article"].(map[string]any)["Body"])
	}
	if _, ok := cr["Attachment"]; ok {
		t.Fatal("Attachment presente senza allegati")
	}
}

func TestCreateErrors(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"errore OTRS": jsonReply(`{"Error":{"ErrorCode":"TicketCreate.AuthFail","ErrorMessage":"no"}}`),
		"pagina HTML": func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "<html>cortesia</html>", 500) },
		"200 non JSON": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, "<html></html>")
		},
		"JSON senza numero": jsonReply(`{"TicketID":"42"}`),
	} {
		t.Run(name, func(t *testing.T) {
			c, calls := fakeOTRS(t, h, okUpdate)
			_, err := c.Create(context.Background(), sample())
			if !errors.Is(err, ErrOTRS) {
				t.Fatalf("atteso ErrOTRS, ottenuto %v", err)
			}
			if len(*calls) != 1 {
				t.Fatalf("dopo un create fallito nessun update: %d chiamate", len(*calls))
			}
		})
	}
}

func TestCreateUpdateFailureStillSucceeds(t *testing.T) {
	c, _ := fakeOTRS(t, okCreate, jsonReply(`{"Error":{"ErrorCode":"TicketUpdate.AccessDenied","ErrorMessage":"no"}}`))
	got, err := c.Create(context.Background(), sample())
	if err != nil || got.CustomerSet || got.TicketNumber != "2026100800000011" {
		t.Fatalf("update fallito: %+v %v", got, err)
	}
}

func TestCreateTimeoutAndRedirect(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(200 * time.Millisecond) }))
	defer slow.Close()
	c := &HTTPClient{Config: config.OTRS{URL: slow.URL, RouteCreate: "/c", RouteUpdate: "/u", User: "a", Password: "b", Queue: "q"},
		HTTP: &http.Client{Timeout: 50 * time.Millisecond}}
	if _, err := c.Create(context.Background(), sample()); !errors.Is(err, ErrOTRS) {
		t.Fatalf("timeout: %v", err)
	}
	redir := httptest.NewServer(http.RedirectHandler("https://altrove.example.it/", http.StatusFound))
	defer redir.Close()
	c2 := NewHTTPClient(config.OTRS{URL: redir.URL, RouteCreate: "/c", RouteUpdate: "/u", User: "a", Password: "b", Queue: "q"})
	if _, err := c2.Create(context.Background(), sample()); !errors.Is(err, ErrOTRS) {
		t.Fatalf("redirect seguito: %v", err)
	}
}

func TestPasswordNeverLogged(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)
	c, _ := fakeOTRS(t, jsonReply(`{"Error":{"ErrorCode":"TicketCreate.AuthFail","ErrorMessage":"no"}}`), okUpdate)
	_, err := c.Create(context.Background(), sample())
	if strings.Contains(buf.String(), "segreta-123") || strings.Contains(err.Error(), "segreta-123") || strings.Contains(err.Error(), "agente") {
		t.Fatalf("credenziali nei log o nell'errore: %s / %v", buf.String(), err)
	}
	if !strings.Contains(buf.String(), "TicketCreate.AuthFail") {
		t.Fatalf("ErrorCode non nel log: %s", buf.String())
	}
}

func TestMock(t *testing.T) {
	m := &Mock{}
	got, err := m.Create(context.Background(), sample())
	if err != nil || got.TicketNumber == "" || !got.CustomerSet || len(m.Sent) != 1 {
		t.Fatalf("mock: %+v %v", got, err)
	}
	m.Err = ErrOTRS
	if _, err := m.Create(context.Background(), sample()); !errors.Is(err, ErrOTRS) {
		t.Fatal("mock con errore")
	}
}

func TestSafeFilename(t *testing.T) {
	for in, want := range map[string]string{
		"../../scheda.png":          "scheda.png",
		`C:\Users\x\Desktop\a.pdf`:   "a.pdf",
		"":                          "allegato",
		strings.Repeat("a", 150) + ".png": strings.Repeat("a", 96) + ".png",
	} {
		if got := safeFilename(in); got != want {
			t.Errorf("safeFilename(%q) = %q, atteso %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Verificare che falliscano** — `go test ./internal/otrs/` → FAIL (pacchetto senza codice).

- [ ] **Step 3: Implementazione** — `internal/otrs/otrs.go`:

```go
// Package otrs apre i ticket in OTRS tramite il GenericInterface REST. È
// l'unico pacchetto che conosce OTRS: host, web service, route, agente e coda
// arrivano dalla configurazione.
package otrs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/mail"
	"path"
	"strings"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

// ErrOTRS: OTRS non ha creato il ticket (irraggiungibile, risposta non JSON
// come la pagina di cortesia del proxy, oppure {"Error":…}).
var ErrOTRS = errors.New("OTRS non disponibile")

type Client interface {
	Create(ctx context.Context, t NewTicket) (Created, error)
}

type NewTicket struct {
	Name, Email, Phone string
	Subject, Body      string
	Attachments        []Attachment
}

type Attachment struct {
	Filename, ContentType string
	Content               []byte
}

type Created struct {
	TicketID, TicketNumber string
	CustomerSet            bool // false se il TicketUpdate del cliente è fallito
}

// New: client finto con OTRS_URL=mock, altrimenti HTTP.
func New(c config.OTRS) Client {
	if c.Mock() {
		return &Mock{}
	}
	return NewHTTPClient(c)
}

type HTTPClient struct {
	Config config.OTRS
	HTTP   *http.Client
}

func NewHTTPClient(c config.OTRS) *HTTPClient {
	return &HTTPClient{Config: c, HTTP: &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: 10 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirect non ammesso")
		},
	}}
}

type otrsError struct {
	Error *struct{ ErrorCode, ErrorMessage string } `json:"Error"`
}

// call manda una richiesta JSON e decodifica la risposta in out. Errori
// senza credenziali: la richiesta non compare mai nel messaggio.
func (c *HTTPClient) call(ctx context.Context, method, route string, body map[string]any, out any) error {
	body["UserLogin"] = c.Config.User
	body["Password"] = c.Config.Password
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrOTRS, err)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Config.URL+route, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%w: richiesta non valida", ErrOTRS)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "CruscottoPA")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var ue interface{ Unwrap() error }
		if errors.As(err, &ue) && ue.Unwrap() != nil {
			err = ue.Unwrap() // *url.Error: senza ripetere metodo e URL
		}
		slog.Warn("otrs: non raggiungibile", "route", route, "err", err)
		return fmt.Errorf("%w: %v", ErrOTRS, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || resp.StatusCode != http.StatusOK || ct != "application/json" {
		slog.Warn("otrs: risposta non valida", "route", route, "status", resp.StatusCode, "content_type", ct)
		return fmt.Errorf("%w: risposta %d %s", ErrOTRS, resp.StatusCode, ct)
	}
	var e otrsError
	if json.Unmarshal(raw, &e) == nil && e.Error != nil {
		slog.Warn("otrs: errore", "route", route, "code", e.Error.ErrorCode, "message", e.Error.ErrorMessage)
		return fmt.Errorf("%w: %s", ErrOTRS, e.Error.ErrorCode)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		slog.Warn("otrs: JSON non valido", "route", route)
		return fmt.Errorf("%w: JSON non valido", ErrOTRS)
	}
	return nil
}

func (c *HTTPClient) Create(ctx context.Context, t NewTicket) (Created, error) {
	body := t.Body
	if p := strings.TrimSpace(t.Phone); p != "" {
		body += "\n\nTelefono / interno: " + p
	}
	req := map[string]any{
		"Ticket": map[string]any{
			"Title": t.Subject, "Queue": c.Config.Queue, "State": "new", "Priority": "3 normal",
			"CustomerUser": t.Email,
		},
		"Article": map[string]any{
			"Subject": t.Subject, "Body": body, "ContentType": "text/plain; charset=utf8",
			"ArticleType": "webrequest", "SenderType": "customer",
			"From": (&mail.Address{Name: t.Name, Address: t.Email}).String(),
		},
	}
	if len(t.Attachments) > 0 {
		atts := make([]map[string]any, len(t.Attachments))
		for i, a := range t.Attachments {
			atts[i] = map[string]any{"Filename": safeFilename(a.Filename), "ContentType": a.ContentType,
				"Content": base64.StdEncoding.EncodeToString(a.Content)}
		}
		req["Attachment"] = atts
	}
	var created struct{ TicketID, TicketNumber string }
	if err := c.call(ctx, http.MethodPost, c.Config.RouteCreate, req, &created); err != nil {
		return Created{}, err
	}
	if created.TicketID == "" || created.TicketNumber == "" {
		slog.Warn("otrs: risposta senza ticket")
		return Created{}, fmt.Errorf("%w: risposta senza ticket", ErrOTRS)
	}
	out := Created{TicketID: created.TicketID, TicketNumber: created.TicketNumber, CustomerSet: true}
	// TicketCreate salva il cliente vuoto se la mail non è un customer user di
	// OTRS: si imposta dopo, come fa il PostMaster con i ticket via mail.
	upd := map[string]any{"TicketID": created.TicketID,
		"Ticket": map[string]any{"CustomerUser": t.Email, "CustomerID": t.Email}}
	var ignored map[string]any
	if err := c.call(ctx, http.MethodPatch, c.Config.RouteUpdate, upd, &ignored); err != nil {
		slog.Warn("otrs: cliente non impostato", "ticket", created.TicketNumber, "err", err)
		out.CustomerSet = false
	}
	return out, nil
}

// safeFilename: solo il nome base (anche da percorsi Windows), max 100 byte.
func safeFilename(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	if name == "." || name == "/" || name == "" {
		return "allegato"
	}
	if len(name) > 100 {
		ext := path.Ext(name)
		if len(ext) > 10 {
			ext = ""
		}
		name = name[:100-len(ext)] + ext
		name = strings.ToValidUTF8(name, "")
	}
	return name
}
```

Nota: `mail.Address.String()` produce `"Mario Rossi" <mrossi@example.it>` (con le virgolette) e codifica RFC 2047 i nomi non ASCII: OTRS li decodifica. Il test fissa la forma ASCII.

`internal/otrs/mock.go`:

```go
package otrs

import (
	"context"
	"fmt"
	"sync"
)

// Mock: OTRS_URL=mock (sviluppo) e test. Registra i ticket in memoria.
type Mock struct {
	mu         sync.Mutex
	Sent       []NewTicket
	Err        error // se impostato, Create fallisce
	FailUpdate bool  // simula il cliente non impostato
}

func (m *Mock) Create(_ context.Context, t NewTicket) (Created, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return Created{}, m.Err
	}
	m.Sent = append(m.Sent, t)
	n := len(m.Sent)
	return Created{TicketID: fmt.Sprint(n), TicketNumber: fmt.Sprintf("20261008000000%02d", n), CustomerSet: !m.FailUpdate}, nil
}
```

- [ ] **Step 4: Test verdi** — `go test ./internal/otrs/` → PASS. Se `TestSafeFilename` fallisce sul caso lungo, correggere il troncamento finché `len == 100` con estensione conservata.
- [ ] **Step 5: Commit** — `git add internal/otrs && git commit -m "otrs: client REST per aprire i ticket"`.

---

### Task 3: Registro `tickets_sent` (migrazione v11)

**Files:**
- Modify: `internal/database/migrations.go` (elenco + funzione in coda)
- Create: `internal/database/tickets.go`
- Test: `internal/database/tickets_test.go`

**Interfaces:**
- Produces:
  - `type TicketSent struct { ID int64; Username, Name, Email, Subject, TicketID, TicketNumber string; CustomerSet bool; Attachments int; CreatedAt time.Time }`
  - `func (db *DB) RecordTicket(t TicketSent) error`
  - `func (db *DB) CountTicketsSince(username string, since time.Time) (int, error)`
  - `func (db *DB) ListTickets(limit int) ([]TicketSent, error)` (più recenti prima)

- [ ] **Step 1: Test che falliscono** — `internal/database/tickets_test.go`:

```go
package database

import (
	"strconv"
	"testing"
	"time"
)

func TestTicketsRecordCountList(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	for i, at := range []time.Time{now.Add(-2 * time.Hour), now.Add(-30 * time.Minute), now} {
		if err := db.RecordTicket(TicketSent{Username: "MRossi", Name: "Mario Rossi", Email: "mrossi@example.it",
			Subject: "Prova", TicketID: strconv.Itoa(i), TicketNumber: "N" + strconv.Itoa(i), CustomerSet: i != 1, Attachments: i, CreatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	db.RecordTicket(TicketSent{Username: "altro", Name: "A", Email: "a@example.it", Subject: "x", TicketID: "9", TicketNumber: "N9", CreatedAt: now})
	n, err := db.CountTicketsSince("mrossi", now.Add(-time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("conteggio ultima ora: %d %v", n, err)
	}
	list, err := db.ListTickets(3)
	if err != nil || len(list) != 3 || list[2].TicketNumber != "N1" {
		t.Fatalf("elenco: %+v %v", list, err)
	}
	got := list[2]
	if got.Username != "mrossi" || got.CustomerSet || got.Attachments != 1 || !got.CreatedAt.Equal(now.Add(-30*time.Minute)) {
		t.Fatalf("riga: %+v", got)
	}
}
```


- [ ] **Step 2: Verificare che fallisca** — `go test ./internal/database/ -run TestTickets` → FAIL.

- [ ] **Step 3: Implementazione** — in `migrations.go` aggiungere `migrateV11TicketsSent,` in fondo all'elenco e in fondo al file:

```go
// migrateV11TicketsSent: registro dei ticket aperti dalla plancia (chi, quando,
// numero OTRS). Il testo resta in OTRS.
func migrateV11TicketsSent(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE tickets_sent (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	username      TEXT    NOT NULL,
	name          TEXT    NOT NULL,
	email         TEXT    NOT NULL,
	subject       TEXT    NOT NULL,
	ticket_id     TEXT    NOT NULL,
	ticket_number TEXT    NOT NULL,
	customer_set  INTEGER NOT NULL,
	attachments   INTEGER NOT NULL,
	created_at    TEXT    NOT NULL
);
CREATE INDEX idx_tickets_sent_user ON tickets_sent(username, created_at);
`)
	return err
}
```

`internal/database/tickets.go`:

```go
package database

import (
	"strings"
	"time"
)

// TicketSent: un ticket aperto dalla plancia. Identità dichiarata (NTLM).
type TicketSent struct {
	ID                                  int64
	Username, Name, Email, Subject      string
	TicketID, TicketNumber              string
	CustomerSet                         bool
	Attachments                         int
	CreatedAt                           time.Time
}

func (db *DB) RecordTicket(t TicketSent) error {
	_, err := db.Exec(`INSERT INTO tickets_sent
(username, name, email, subject, ticket_id, ticket_number, customer_set, attachments, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.ToLower(strings.TrimSpace(t.Username)), t.Name, t.Email, t.Subject, t.TicketID, t.TicketNumber,
		t.CustomerSet, t.Attachments, formatTime(t.CreatedAt))
	return err
}

// CountTicketsSince: ticket aperti da username da since in poi (limite di frequenza).
func (db *DB) CountTicketsSince(username string, since time.Time) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM tickets_sent WHERE username = ? AND created_at >= ?`,
		strings.ToLower(strings.TrimSpace(username)), formatTime(since)).Scan(&n)
	return n, err
}

func (db *DB) ListTickets(limit int) ([]TicketSent, error) {
	rows, err := db.Query(`SELECT id, username, name, email, subject, ticket_id, ticket_number, customer_set, attachments, created_at
FROM tickets_sent ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TicketSent{}
	for rows.Next() {
		var t TicketSent
		var at string
		if err := rows.Scan(&t.ID, &t.Username, &t.Name, &t.Email, &t.Subject, &t.TicketID, &t.TicketNumber,
			&t.CustomerSet, &t.Attachments, &at); err != nil {
			return nil, err
		}
		if t.CreatedAt, err = parseTime(at); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
```


- [ ] **Step 4: Test verdi** — `go test ./internal/database/` → PASS (anche `TestOpenAppliesMigrationsAndSeed`, che usa `len(migrations)`).
- [ ] **Step 5: Commit** — `git add internal/database && git commit -m "database: registro dei ticket aperti (v11)"`.

---

### Task 4: Allegati del ticket (upload a pezzi per utenti riconosciuti)

**Files:**
- Create: `internal/web/ticket_uploads.go`
- Modify: `internal/web/server.go` (campo `ticketFiles ticketUploads` nel `Server`, inizializzazione in `New`, route)
- Test: `internal/web/ticket_uploads_test.go`

**Interfaces:**
- Consumes: `s.viewer(r)` (identità dal cookie), `randomHex()`, `mediaJSON`, `mediaFail`, `mediaChunk` (esistenti in `media.go`).
- Produces:
  - route `POST /ticket/allegati` (form `nome`), `POST /ticket/allegati/{id}/pezzo?n=`, `POST /ticket/allegati/{id}/fine` → JSON `{"ok":true,"id":…,"chunk":524288}` / `{"ok":true}` / `{"ok":true,"id":…,"nome":…,"size":…}` oppure `{"ok":false,"error":"…"}`.
  - `func (s *Server) takeTicketFiles(username string, ids []string) ([]otrs.Attachment, []string, error)` — restituisce gli allegati pronti (solo upload finiti, dello stesso utente, non ancora usati), i percorsi da cancellare, `errTicketFiles` se un id non è valido o i limiti sono superati. Rimuove gli id dalla mappa (monouso).
  - `func (s *Server) removeTicketFiles(paths []string)`
  - costanti `ticketMaxFile = 5 << 20`, `ticketMaxFiles = 3`, `ticketMaxTotal = 10 << 20`, `ticketUploadTTL = time.Hour`.

- [ ] **Step 1: Test che falliscono** — `internal/web/ticket_uploads_test.go`:

```go
package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// uploadTicketFile carica data a pezzi come ticket.js; risposta finale o primo errore.
func uploadTicketFile(t *testing.T, s *Server, c *http.Cookie, name string, data []byte) map[string]any {
	t.Helper()
	start := mediaPost(t, s, c, "/ticket/allegati", "application/x-www-form-urlencoded", []byte("nome="+name))
	if start["ok"] != true {
		return start
	}
	id := start["id"].(string)
	chunk := int(start["chunk"].(float64))
	for n, off := 0, 0; off < len(data); n, off = n+1, off+chunk {
		end := min(off+chunk, len(data))
		r := mediaPost(t, s, c, "/ticket/allegati/"+id+"/pezzo?n="+itoa(int64(n)), "application/octet-stream", data[off:end])
		if r["ok"] != true {
			return r
		}
	}
	return mediaPost(t, s, c, "/ticket/allegati/"+id+"/fine", "application/x-www-form-urlencoded", nil)
}

func ticketServer(t *testing.T) (*Server, *http.Cookie) {
	t.Helper()
	s, _ := newTestServer(t, nil)
	return s, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
}

func TestTicketUploadOK(t *testing.T) {
	s, c := ticketServer(t)
	big := append(append([]byte{}, pngBytes...), make([]byte, 700<<10)...)
	out := uploadTicketFile(t, s, c, "schermata.png", big)
	if out["ok"] != true || out["nome"] != "schermata.png" || out["id"] == "" {
		t.Fatalf("upload: %v", out)
	}
	atts, paths, err := s.takeTicketFiles("mrossi", []string{out["id"].(string)})
	if err != nil || len(atts) != 1 || atts[0].ContentType != "image/png" || len(atts[0].Content) != len(big) {
		t.Fatalf("take: %v %d", err, len(atts))
	}
	if _, _, err := s.takeTicketFiles("mrossi", []string{out["id"].(string)}); err == nil {
		t.Fatal("id riusato: deve fallire")
	}
	s.removeTicketFiles(paths)
	if entries, _ := os.ReadDir(filepath.Join(s.cfg.UploadDir, ".tmp", "ticket")); len(entries) != 0 {
		t.Errorf("temporanei rimasti: %d", len(entries))
	}
}

func TestTicketUploadRejects(t *testing.T) {
	s, c := ticketServer(t)
	if out := uploadTicketFile(t, s, nil, "a.png", pngBytes); out["ok"] != false {
		t.Errorf("anonimo: %v", out)
	}
	anon := viewerCookie(t, s, identity.User{Anonymous: true})
	if out := uploadTicketFile(t, s, anon, "a.png", pngBytes); out["ok"] != false {
		t.Errorf("cookie anonimo: %v", out)
	}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)
	if out := uploadTicketFile(t, s, c, "a.svg", svg); out["ok"] != false {
		t.Errorf("SVG: %v", out)
	}
	if out := uploadTicketFile(t, s, c, "big.png", append(append([]byte{}, pngBytes...), make([]byte, 5<<20)...)); out["ok"] != false {
		t.Errorf("oltre 5 MB: %v", out)
	}
	if out := uploadTicketFile(t, s, c, "doc.pdf", pdfBytes); out["ok"] != true {
		t.Errorf("PDF valido: %v", out)
	}
}

func TestTicketFilesOtherUserAndLimits(t *testing.T) {
	s, c := ticketServer(t)
	other := viewerCookie(t, s, identity.User{Username: "gbianchi", Name: "Giulia Bianchi"})
	mine := uploadTicketFile(t, s, c, "a.png", pngBytes)["id"].(string)
	theirs := uploadTicketFile(t, s, other, "b.png", pngBytes)["id"].(string)
	if _, _, err := s.takeTicketFiles("mrossi", []string{theirs}); err == nil {
		t.Fatal("allegato di un altro utente accettato")
	}
	var ids []string
	for i := 0; i < 4; i++ {
		ids = append(ids, uploadTicketFile(t, s, c, "x.png", pngBytes)["id"].(string))
	}
	if _, _, err := s.takeTicketFiles("mrossi", ids); err == nil {
		t.Fatal("più di 3 allegati accettati")
	}
	if _, _, err := s.takeTicketFiles("mrossi", []string{mine, "nonesiste"}); err == nil {
		t.Fatal("id inesistente accettato")
	}
	// total: 3 file da 4 MB = 12 MB > 10 MB
	four := append(append([]byte{}, pngBytes...), make([]byte, 4<<20)...)
	var big []string
	for i := 0; i < 3; i++ {
		big = append(big, uploadTicketFile(t, s, c, "g.png", four)["id"].(string))
	}
	if _, _, err := s.takeTicketFiles("mrossi", big); err == nil {
		t.Fatal("oltre 10 MB totali accettati")
	}
}
```

- [ ] **Step 2: Verificare che falliscano** (in container, Global Constraints) → FAIL (route inesistenti).

- [ ] **Step 3: Implementazione** — `internal/web/ticket_uploads.go`:

```go
package web

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

// Allegati dei ticket: caricati a pezzi come i media dell'admin, ma da
// utenti riconosciuti e legati allo username del cookie. Monouso: un id
// passa a OTRS una volta sola, poi il file si cancella.
const (
	ticketMaxFile    = 5 << 20
	ticketMaxFiles   = 3
	ticketMaxTotal   = 10 << 20
	ticketMaxPending = 50 // caricamenti aperti in tutto il server
	ticketUploadTTL  = time.Hour
)

var errTicketFiles = errors.New("allegati non validi")

type ticketUpload struct {
	username    string
	name        string
	path        string
	next        int
	size        int64
	done        bool
	contentType string
	started     time.Time
}

type ticketUploads struct {
	mu   sync.Mutex
	byID map[string]*ticketUpload
}

func (s *Server) ticketTmpDir() string { return filepath.Join(s.cfg.UploadDir, ".tmp", "ticket") }

// ticketUser: username del cookie, "" se anonimo o senza cookie.
func (s *Server) ticketUser(r *http.Request) string {
	u, ok := s.viewer(r)
	if !ok || u.Anonymous {
		return ""
	}
	return u.Username
}

// expireTicketUploads (con il lock): via i caricamenti più vecchi di un'ora.
func (s *Server) expireTicketUploads() {
	now := s.now()
	for id, u := range s.ticketFiles.byID {
		if now.Sub(u.started) > ticketUploadTTL {
			os.Remove(u.path)
			delete(s.ticketFiles.byID, id)
		}
	}
}

func (s *Server) handleTicketFileStart(w http.ResponseWriter, r *http.Request) {
	user := s.ticketUser(r)
	if user == "" {
		mediaFail(w, "Per allegare file devi essere riconosciuto.")
		return
	}
	name := r.FormValue("nome")
	if !utf8.ValidString(name) || len(name) > 255 {
		name = "allegato"
	}
	s.ticketFiles.mu.Lock()
	defer s.ticketFiles.mu.Unlock()
	s.expireTicketUploads()
	if len(s.ticketFiles.byID) >= ticketMaxPending {
		mediaFail(w, "Troppi caricamenti in corso, riprova fra poco.")
		return
	}
	if err := os.MkdirAll(s.ticketTmpDir(), 0o750); err != nil {
		slog.Error("ticket: cartella temporanea", "err", err)
		mediaFail(w, "Caricamento non riuscito.")
		return
	}
	id := randomHex()
	s.ticketFiles.byID[id] = &ticketUpload{username: user, name: name, path: filepath.Join(s.ticketTmpDir(), id), started: s.now()}
	mediaJSON(w, map[string]any{"ok": true, "id": id, "chunk": mediaChunk})
}

func (s *Server) handleTicketFileChunk(w http.ResponseWriter, r *http.Request) {
	user := s.ticketUser(r)
	n, err := strconv.Atoi(r.URL.Query().Get("n"))
	id := r.PathValue("id")
	s.ticketFiles.mu.Lock()
	defer s.ticketFiles.mu.Unlock()
	u := s.ticketFiles.byID[id]
	if u == nil || u.username != user || u.done {
		mediaFail(w, "Caricamento scaduto, riprova.")
		return
	}
	abort := func(msg string) {
		delete(s.ticketFiles.byID, id)
		os.Remove(u.path)
		mediaFail(w, msg)
	}
	if err != nil || n != u.next {
		abort("Caricamento interrotto, riprova.")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, mediaChunk+1))
	if err != nil || len(data) > mediaChunk || u.size+int64(len(data)) > ticketMaxFile {
		abort("File troppo grande (massimo 5 MB).")
		return
	}
	f, err := os.OpenFile(u.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err == nil {
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		slog.Error("ticket: scrittura pezzo", "err", err)
		abort("Caricamento non riuscito.")
		return
	}
	u.next++
	u.size += int64(len(data))
	mediaJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleTicketFileFinish(w http.ResponseWriter, r *http.Request) {
	user := s.ticketUser(r)
	id := r.PathValue("id")
	s.ticketFiles.mu.Lock()
	defer s.ticketFiles.mu.Unlock()
	u := s.ticketFiles.byID[id]
	if u == nil || u.username != user || u.done {
		mediaFail(w, "Caricamento scaduto, riprova.")
		return
	}
	head := make([]byte, 512)
	k := 0
	if f, err := os.Open(u.path); err == nil {
		k, _ = io.ReadFull(f, head)
		f.Close()
	}
	ct := ticketContentType(head[:k])
	if ct == "" {
		delete(s.ticketFiles.byID, id)
		os.Remove(u.path)
		mediaFail(w, "Formato non ammesso: usa PNG, JPEG, WebP o PDF.")
		return
	}
	u.done, u.contentType = true, ct
	mediaJSON(w, map[string]any{"ok": true, "id": id, "nome": u.name, "size": u.size})
}

// ticketContentType: tipo dal contenuto, mai dal nome. "" = non ammesso.
func ticketContentType(head []byte) string {
	if bytes.HasPrefix(head, []byte("%PDF-")) {
		return "application/pdf"
	}
	switch ct := http.DetectContentType(head); ct {
	case "image/png", "image/jpeg", "image/webp":
		return ct
	}
	return ""
}

// takeTicketFiles prende gli allegati di username per l'invio: tutti validi o
// nessuno. Gli id escono dalla mappa (monouso); i file vanno cancellati con
// removeTicketFiles dopo l'invio, riuscito o no.
func (s *Server) takeTicketFiles(username string, ids []string) ([]otrs.Attachment, []string, error) {
	if len(ids) == 0 {
		return nil, nil, nil
	}
	if len(ids) > ticketMaxFiles {
		return nil, nil, errTicketFiles
	}
	s.ticketFiles.mu.Lock()
	ups := make([]*ticketUpload, 0, len(ids))
	var total int64
	seen := map[string]bool{}
	for _, id := range ids {
		u := s.ticketFiles.byID[id]
		if u == nil || seen[id] || !u.done || u.username != username {
			s.ticketFiles.mu.Unlock()
			return nil, nil, errTicketFiles
		}
		seen[id] = true
		total += u.size
		ups = append(ups, u)
	}
	if total > ticketMaxTotal {
		s.ticketFiles.mu.Unlock()
		return nil, nil, errTicketFiles
	}
	for _, id := range ids {
		delete(s.ticketFiles.byID, id)
	}
	s.ticketFiles.mu.Unlock()

	atts := make([]otrs.Attachment, 0, len(ups))
	paths := make([]string, 0, len(ups))
	for _, u := range ups {
		paths = append(paths, u.path)
	}
	for _, u := range ups {
		data, err := os.ReadFile(u.path)
		if err != nil {
			slog.Error("ticket: lettura allegato", "err", err)
			s.removeTicketFiles(paths)
			return nil, nil, errTicketFiles
		}
		atts = append(atts, otrs.Attachment{Filename: u.name, ContentType: u.contentType, Content: data})
	}
	return atts, paths, nil
}

func (s *Server) removeTicketFiles(paths []string) {
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("ticket: rimozione allegato", "err", err)
		}
	}
}
```

In `server.go`: campo `ticketFiles ticketUploads // allegati dei ticket in caricamento`, in `New` `ticketFiles: ticketUploads{byID: map[string]*ticketUpload{}},`. In `New`, dopo `s.routes()`, svuotare i temporanei lasciati da un riavvio: `os.RemoveAll(s.ticketTmpDir())` (import `os`). Route, vicino a `/presenza`:

```go
	s.mux.HandleFunc("POST /ticket/allegati", s.handleTicketFileStart)
	s.mux.HandleFunc("POST /ticket/allegati/{id}/pezzo", s.handleTicketFileChunk)
	s.mux.HandleFunc("POST /ticket/allegati/{id}/fine", s.handleTicketFileFinish)
```

- [ ] **Step 4: Test verdi** (container) `-run TestTicket` → PASS.
- [ ] **Step 5: Commit** — `git add internal/web && git commit -m "Ticket: allegati caricati a pezzi dagli utenti riconosciuti"`.

---

### Task 5: Invio del ticket `POST /ticket`

**Files:**
- Create: `internal/web/ticket.go`
- Modify: `internal/web/server.go` (`Options.Tickets otrs.Client`, campo `tickets otrs.Client`, route)
- Modify: `internal/web/profiles.go` (`profileFor` chiede sempre anche `mail` e `telephoneNumber`)
- Modify: `cmd/server/main.go` (wiring del client)
- Test: `internal/web/ticket_test.go`

**Interfaces:**
- Consumes: `otrs.Client`, `otrs.NewTicket`, `otrs.ErrOTRS` (Task 2); `db.RecordTicket`, `db.CountTicketsSince`, `database.TicketSent` (Task 3); `s.takeTicketFiles`, `s.removeTicketFiles`, `s.ticketUser` (Task 4); `s.profileFor(username) (audience.Profile, ok, down bool)`.
- Produces:
  - `Options.Tickets otrs.Client` (nil = modulo spento); `func (s *Server) ticketsEnabled() bool`.
  - `type ticketRequester struct { Name, Email, Phone string }` e `func (s *Server) ticketRequester(r *http.Request) (req ticketRequester, problem string)` — `problem` ∈ `""`, `"anonimo"`, `"ad"`, `"mail"`.
  - route `POST /ticket` → JSON come da spec (`numero`, `campi`, `errore`, `casella`).
  - costanti `ticketsPerHour = 5`, `maxSubject = 120`, `maxBody = 10000`, `maxPhone = 40`.

- [ ] **Step 1: Test che falliscono** — `internal/web/ticket_test.go`:

```go
package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

var ticketDirectory = fakeDirectory{
	people: testDirectory.people,
	profiles: map[string]audience.Profile{
		"mrossi":    {Username: "mrossi", Attrs: map[string][]string{"mail": {"mrossi@example.it"}, "telephonenumber": {"731"}}},
		"senzamail": {Username: "senzamail", Attrs: map[string][]string{}},
	},
}

func ticketTestServer(t *testing.T, m *otrs.Mock) (*Server, *http.Cookie) {
	t.Helper()
	s, _ := newTestServerWith(t, nil, func(o *Options) {
		o.Directory = ticketDirectory
		o.Tickets = m
		o.Config.OTRS.FallbackEmail = "assistenza@example.it"
	})
	return s, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
}

func postTicket(t *testing.T, s *Server, c *http.Cookie, form url.Values) map[string]any {
	t.Helper()
	rec := do(t, s, "POST", "/ticket", form, c, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if rec.Code != 200 {
		t.Fatalf("status %d (deve essere sempre 200)", rec.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("JSON non valido: %s", rec.Body)
	}
	return out
}

func validTicket() url.Values {
	return url.Values{"oggetto": {"Stampante"}, "descrizione": {"Non stampa più.\nÈ urgente"}, "telefono": {"0851234"}}
}

func TestTicketSendOK(t *testing.T) {
	m := &otrs.Mock{}
	s, c := ticketTestServer(t, m)
	form := validTicket()
	form.Set("nome", "Altro Nome") // ignorati: valgono quelli di AD
	form.Set("mail", "altro@example.it")
	out := postTicket(t, s, c, form)
	if out["ok"] != true || out["numero"] != "2026100800000001" || out["mail"] != "mrossi@example.it" {
		t.Fatalf("invio: %v", out)
	}
	sent := m.Sent[0]
	if sent.Name != "Mario Rossi" || sent.Email != "mrossi@example.it" || sent.Phone != "0851234" || sent.Body != "Non stampa più.\nÈ urgente" {
		t.Fatalf("ticket inviato: %+v", sent)
	}
	list, _ := s.db.ListTickets(10)
	if len(list) != 1 || list[0].Username != "mrossi" || list[0].TicketNumber != "2026100800000001" || !list[0].CustomerSet {
		t.Fatalf("registro: %+v", list)
	}
}

func TestTicketPhoneFromAD(t *testing.T) {
	m := &otrs.Mock{}
	s, c := ticketTestServer(t, m)
	form := validTicket()
	form.Del("telefono")
	postTicket(t, s, c, form)
	if m.Sent[0].Phone != "731" {
		t.Fatalf("telefono da AD: %q", m.Sent[0].Phone)
	}
}

func TestTicketValidation(t *testing.T) {
	m := &otrs.Mock{}
	s, c := ticketTestServer(t, m)
	for name, edit := range map[string]func(url.Values){
		"oggetto vuoto":       func(f url.Values) { f.Set("oggetto", "  ") },
		"descrizione vuota":   func(f url.Values) { f.Set("descrizione", "") },
		"oggetto lungo":       func(f url.Values) { f.Set("oggetto", strings.Repeat("è", 121)) },
		"descrizione lunga":   func(f url.Values) { f.Set("descrizione", strings.Repeat("a", 10001)) },
		"telefono lungo":      func(f url.Values) { f.Set("telefono", strings.Repeat("1", 41)) },
		"allegato non valido": func(f url.Values) { f.Add("allegato", "nonesiste") },
	} {
		f := validTicket()
		edit(f)
		out := postTicket(t, s, c, f)
		if out["ok"] != false || out["campi"] == nil {
			t.Errorf("%s: %v", name, out)
		}
	}
	if len(m.Sent) != 0 {
		t.Fatalf("inviati ticket non validi: %d", len(m.Sent))
	}
}

func TestTicketRequesterProblems(t *testing.T) {
	m := &otrs.Mock{}
	s, _ := ticketTestServer(t, m)
	if out := postTicket(t, s, nil, validTicket()); out["errore"] != "anonimo" {
		t.Errorf("senza cookie: %v", out)
	}
	if out := postTicket(t, s, viewerCookie(t, s, identity.User{Anonymous: true}), validTicket()); out["errore"] != "anonimo" {
		t.Errorf("anonimo: %v", out)
	}
	if out := postTicket(t, s, viewerCookie(t, s, identity.User{Username: "senzamail", Name: "S"}), validTicket()); out["errore"] != "mail" {
		t.Errorf("senza mail: %v", out)
	}
	down, _ := newTestServerWith(t, nil, func(o *Options) { o.Directory = fakeDirectory{err: errors.New("giù")}; o.Tickets = m })
	if out := postTicket(t, down, viewerCookie(t, down, identity.User{Username: "mrossi", Name: "Mario Rossi"}), validTicket()); out["errore"] != "ad" {
		t.Errorf("AD giù: %v", out)
	}
	if len(m.Sent) != 0 {
		t.Fatal("ticket inviato senza richiedente valido")
	}
}

func TestTicketRateLimit(t *testing.T) {
	m := &otrs.Mock{}
	s, c := ticketTestServer(t, m)
	for i := 0; i < 5; i++ {
		if out := postTicket(t, s, c, validTicket()); out["ok"] != true {
			t.Fatalf("ticket %d: %v", i+1, out)
		}
	}
	if out := postTicket(t, s, c, validTicket()); out["errore"] != "limite" || out["casella"] != "assistenza@example.it" {
		t.Fatalf("sesto ticket: %v", out)
	}
}

func TestTicketOTRSDown(t *testing.T) {
	m := &otrs.Mock{Err: otrs.ErrOTRS}
	s, c := ticketTestServer(t, m)
	id := uploadTicketFile(t, s, c, "a.png", pngBytes)["id"].(string)
	f := validTicket()
	f.Add("allegato", id)
	out := postTicket(t, s, c, f)
	if out["errore"] != "otrs" || out["casella"] != "assistenza@example.it" {
		t.Fatalf("OTRS giù: %v", out)
	}
	if list, _ := s.db.ListTickets(10); len(list) != 0 {
		t.Fatal("riga nel registro con OTRS giù")
	}
	if _, _, err := s.takeTicketFiles("mrossi", []string{id}); err == nil {
		t.Fatal("allegato ancora disponibile dopo l'invio fallito")
	}
}

func TestTicketWithAttachmentAndCustomerNotSet(t *testing.T) {
	m := &otrs.Mock{FailUpdate: true}
	s, c := ticketTestServer(t, m)
	id := uploadTicketFile(t, s, c, "schermata.png", pngBytes)["id"].(string)
	f := validTicket()
	f.Add("allegato", id)
	if out := postTicket(t, s, c, f); out["ok"] != true {
		t.Fatalf("invio: %v", out)
	}
	if len(m.Sent[0].Attachments) != 1 || m.Sent[0].Attachments[0].Filename != "schermata.png" {
		t.Fatalf("allegati: %+v", m.Sent[0].Attachments)
	}
	list, _ := s.db.ListTickets(10)
	if list[0].CustomerSet || list[0].Attachments != 1 {
		t.Fatalf("registro: %+v", list[0])
	}
}

func TestTicketDisabled(t *testing.T) {
	s, _ := newTestServer(t, nil) // Tickets nil
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	if out := postTicket(t, s, c, validTicket()); out["ok"] != false || out["errore"] != "spento" {
		t.Fatalf("modulo spento: %v", out)
	}
}
```

- [ ] **Step 2: Verificare che falliscano** (container `-run TestTicket`) → FAIL (`o.Tickets` inesistente).

- [ ] **Step 3: Implementazione**

`profiles.go`, in `profileFor`, dopo il ciclo che riempie `names`:

```go
	// mail e telefono servono al modulo ticket anche se non sono attributi dei gruppi.
	names = append(names, "mail", "telephoneNumber")
```

`server.go`: in `Options` `// Tickets: invio dei ticket a OTRS (nil = modulo spento).` `Tickets otrs.Client`; nel `Server` `tickets otrs.Client`; in `New` `tickets: o.Tickets,`; route `s.mux.HandleFunc("POST /ticket", s.handleTicketSend)`; import `internal/otrs`.

`internal/web/ticket.go`:

```go
package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

const (
	ticketsPerHour = 5
	maxSubject     = 120
	maxBody        = 10000
	maxPhone       = 40
)

func (s *Server) ticketsEnabled() bool { return s.tickets != nil }

// ticketRequester: chi apre il ticket, da AD (mai dal form). Identità
// DICHIARATA: le risposte di OTRS vanno comunque alla mail vera.
type ticketRequester struct{ Name, Email, Phone string }

func firstAttr(p audience.Profile, name string) string {
	for _, v := range p.Attrs[strings.ToLower(name)] {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// ticketRequester: problem = "anonimo" | "ad" | "mail" se non si può aprire un ticket.
func (s *Server) ticketRequester(r *http.Request) (ticketRequester, string) {
	u, ok := s.viewer(r)
	if !ok || u.Anonymous || u.Username == "" {
		return ticketRequester{}, "anonimo"
	}
	p, ok, down := s.profileFor(u.Username)
	switch {
	case down:
		return ticketRequester{}, "ad"
	case !ok:
		return ticketRequester{}, "anonimo"
	}
	req := ticketRequester{Name: u.Name, Email: firstAttr(p, "mail"), Phone: firstAttr(p, "telephoneNumber")}
	if req.Name == "" {
		req.Name = u.Username
	}
	if req.Email == "" {
		return req, "mail"
	}
	return req, ""
}

func (s *Server) handleTicketSend(w http.ResponseWriter, r *http.Request) {
	reply := func(v map[string]any) {
		if v["ok"] != true {
			v["ok"] = false
			if c := s.cfg.OTRS.FallbackEmail; c != "" {
				v["casella"] = c
			}
		}
		mediaJSON(w, v)
	}
	if !s.ticketsEnabled() {
		reply(map[string]any{"errore": "spento"})
		return
	}
	req, problem := s.ticketRequester(r)
	if problem != "" {
		reply(map[string]any{"errore": problem})
		return
	}
	subject := strings.TrimSpace(r.FormValue("oggetto"))
	body := strings.TrimSpace(strings.ReplaceAll(r.FormValue("descrizione"), "\r\n", "\n"))
	phone := strings.TrimSpace(r.FormValue("telefono"))
	if phone == "" {
		phone = req.Phone
	}
	fields := map[string]string{}
	switch n := utf8.RuneCountInString(subject); {
	case n == 0:
		fields["oggetto"] = "Scrivi l'oggetto."
	case n > maxSubject:
		fields["oggetto"] = "Massimo 120 caratteri."
	}
	switch n := utf8.RuneCountInString(body); {
	case n == 0:
		fields["descrizione"] = "Descrivi il problema."
	case n > maxBody:
		fields["descrizione"] = "Massimo 10.000 caratteri."
	}
	if utf8.RuneCountInString(phone) > maxPhone {
		fields["telefono"] = "Massimo 40 caratteri."
	}
	user := s.ticketUser(r)
	n, err := s.db.CountTicketsSince(user, s.now().Add(-time.Hour))
	if err != nil {
		slog.Error("ticket: conteggio", "err", err)
		reply(map[string]any{"errore": "otrs"})
		return
	}
	if n >= ticketsPerHour {
		reply(map[string]any{"errore": "limite"})
		return
	}
	if len(fields) > 0 {
		reply(map[string]any{"campi": fields})
		return
	}
	atts, paths, err := s.takeTicketFiles(user, r.Form["allegato"])
	if err != nil {
		reply(map[string]any{"campi": map[string]string{"allegati": "Allegati non validi: ricaricali."}})
		return
	}
	defer s.removeTicketFiles(paths)
	ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
	defer cancel()
	created, err := s.tickets.Create(ctx, otrs.NewTicket{Name: req.Name, Email: req.Email, Phone: phone,
		Subject: subject, Body: body, Attachments: atts})
	if err != nil {
		if !errors.Is(err, otrs.ErrOTRS) {
			slog.Error("ticket: invio", "err", err)
		}
		reply(map[string]any{"errore": "otrs"})
		return
	}
	if err := s.db.RecordTicket(database.TicketSent{Username: user, Name: req.Name, Email: req.Email, Subject: subject,
		TicketID: created.TicketID, TicketNumber: created.TicketNumber, CustomerSet: created.CustomerSet,
		Attachments: len(atts), CreatedAt: s.now()}); err != nil {
		slog.Error("ticket: registro", "ticket", created.TicketNumber, "err", err) // il ticket esiste comunque
	}
	slog.Info("ticket aperto", "ticket", created.TicketNumber, "user", user, "allegati", len(atts))
	reply(map[string]any{"ok": true, "numero": created.TicketNumber, "mail": req.Email})
}
```

Nota: `r.FormValue` chiama `ParseMultipartForm`/`ParseForm`, quindi `r.Form["allegato"]` è popolato. Il form arriva `application/x-www-form-urlencoded` (Task 6).

`cmd/server/main.go`, prima di `web.New`:

```go
	var tickets otrs.Client
	if cfg.OTRS.Enabled() {
		tickets = otrs.New(cfg.OTRS)
		if directory == nil || cfg.NTLMDomain == "" {
			slog.Warn("OTRS_URL impostato ma riconoscimento utente spento: nessuno potrà aprire ticket dalla plancia")
		}
	}
```

e in `web.Options` `Tickets: tickets,` (import `internal/otrs`). Attenzione: con `tickets` nil passare un'interfaccia nil vera (variabile dichiarata `var tickets otrs.Client`, come sopra), non un `*otrs.HTTPClient` nil.

- [ ] **Step 4: Test verdi** (container `-run 'TestTicket|TestProfile|TestDashboard'`) → PASS; poi `go build ./...` e `go vet ./...`.
- [ ] **Step 5: Commit** — `git add internal/web cmd/server && git commit -m "Ticket: invio a OTRS con richiedente da AD, limite e registro"`.

---

### Task 6: Tile, dialog e `ticket.js` in plancia

**Files:**
- Modify: `internal/web/dashboard.go` (`dashboardView.Ticket ticketView`)
- Modify: `web/templates/dashboard.html` (tile in testa a `.apps`, dialog prima di `</body>`, `<script src="/static/js/ticket.js" defer>` in `plancia_head`)
- Create: `web/templates/partials_ticket.html` (template `ticket_tile`, `ticket_dialog`)
- Create: `web/static/js/ticket.js`
- Modify: `web/static/css/plancia.css`
- Test: `internal/web/ticket_page_test.go`

**Interfaces:**
- Consumes: `s.ticketsEnabled()`, `s.ticketRequester(r)` (Task 5), route `/ticket` e `/ticket/allegati*` (Task 4–5).
- Produces: `type ticketView struct { Enabled bool; Problem string; Name, Email, Phone, Fallback string }`; markup con attributi `data-ticket-open`, `dialog.ticket`, `data-ticket-form`, `data-ticket-files`, `data-ticket-done`, `data-ticket-error`, `[data-campo="oggetto|descrizione|telefono|allegati"]`.

- [ ] **Step 1: Test che falliscono** — `internal/web/ticket_page_test.go`:

```go
package web

import (
	"os"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

func TestDashboardTicketTile(t *testing.T) {
	off, _ := newTestServer(t, nil)
	if body := do(t, off, "GET", "/", nil, nil, nil).Body.String(); strings.Contains(body, "data-ticket-open") {
		t.Fatal("tile presente con il modulo spento")
	}
	s, c := ticketTestServer(t, &otrs.Mock{})
	body := do(t, s, "GET", "/", nil, c, nil).Body.String()
	for _, want := range []string{"data-ticket-open", `class="ticket"`, "Mario Rossi", "mrossi@example.it", `value="731"`, "data-ticket-form", "/static/js/ticket.js"} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	if strings.Contains(body, `name="mail"`) || strings.Contains(body, `name="nome"`) {
		t.Error("nome e mail non devono essere campi del form")
	}
	anon := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Anonymous: true}), nil).Body.String()
	if !strings.Contains(anon, "data-ticket-open") || strings.Contains(anon, "data-ticket-form") || !strings.Contains(anon, "assistenza@example.it") {
		t.Error("anonimo: tile sì, modulo no, casella sì")
	}
	noMail := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Username: "senzamail", Name: "S"}), nil).Body.String()
	if strings.Contains(noMail, "data-ticket-form") || !strings.Contains(noMail, "manca la mail") {
		t.Error("utente senza mail in AD: niente modulo, spiegazione")
	}
}

// ticket.js: nessuna API del browser senza try/catch su sessionStorage, invio
// come form urlencoded, allegati a pezzi.
func TestTicketJS(t *testing.T) {
	js, err := os.ReadFile("../../web/static/js/ticket.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(js)
	for _, want := range []string{"/ticket/allegati", "URLSearchParams", "sessionStorage", "paste", "data-ticket-open"} {
		if !strings.Contains(src, want) {
			t.Errorf("ticket.js: manca %q", want)
		}
	}
	if strings.Count(src, "sessionStorage") > strings.Count(src, "try {")*2 {
		t.Error("sessionStorage senza try/catch")
	}
}
```

- [ ] **Step 2: Verificare che falliscano** (container `-run 'TestDashboardTicketTile|TestTicketJS'`) → FAIL.

- [ ] **Step 3: Implementazione**

`dashboard.go`: aggiungere a `dashboardView` `Ticket ticketView // tile e dialog «Apri un ticket»`; tipo e costruzione:

```go
type ticketView struct {
	Enabled  bool
	Problem  string // "" | anonimo | ad | mail
	Name     string
	Email    string
	Phone    string
	Fallback string
}

func (s *Server) ticketViewFor(r *http.Request) ticketView {
	if !s.ticketsEnabled() {
		return ticketView{}
	}
	req, problem := s.ticketRequester(r)
	return ticketView{Enabled: true, Problem: problem, Name: req.Name, Email: req.Email, Phone: req.Phone, Fallback: s.cfg.OTRS.FallbackEmail}
}
```

In `handleDashboard`, nel letterale `dashboardView{…}`: `Ticket: s.ticketViewFor(r),`.

`web/templates/partials_ticket.html`:

```html
{{define "ticket_tile"}}{{if .Enabled}}
<div class="tile tile-ticket">
	<div class="tile-card">
	<button type="button" class="tile-main" data-ticket-open>
		<span class="tile-icon"><span class="material-icons" aria-hidden="true">confirmation_number</span></span>
		<span class="tile-text">
			<span class="tile-title">Apri un ticket</span>
			<span class="tile-desc">Chiedi assistenza al CED</span>
		</span>
	</button>
	</div>
</div>
{{end}}{{end}}

{{define "ticket_dialog"}}{{if .Enabled}}
<dialog class="ticket" aria-labelledby="ticket-title">
	<h2 id="ticket-title">Apri un ticket</h2>
	{{if eq .Problem ""}}
	<form data-ticket-form novalidate>
		<p class="ticket-who"><span class="material-icons" aria-hidden="true">person</span><span><strong>{{.Name}}</strong><br><small>Le risposte arriveranno a {{.Email}}</small></span></p>
		<label>Oggetto<input name="oggetto" maxlength="120" required autocomplete="off"></label>
		<small class="ticket-err" data-campo="oggetto" hidden></small>
		<label>Descrizione del problema<textarea name="descrizione" rows="7" maxlength="10000" required></textarea></label>
		<small class="ticket-err" data-campo="descrizione" hidden></small>
		<label>Telefono / interno<input name="telefono" maxlength="40" value="{{.Phone}}" autocomplete="off"></label>
		<small class="ticket-err" data-campo="telefono" hidden></small>
		<div class="ticket-files">
			<button type="button" data-ticket-attach><span class="material-icons" aria-hidden="true">attach_file</span>Allega</button>
			<small>PNG, JPEG, WebP o PDF, fino a 3 file (5 MB ciascuno). Puoi anche incollare uno screenshot.</small>
			<input type="file" accept="image/png,image/jpeg,image/webp,application/pdf" multiple hidden data-ticket-input>
			<ul data-ticket-files></ul>
			<small class="ticket-err" data-campo="allegati" hidden></small>
		</div>
		<p class="ticket-err ticket-general" data-ticket-error hidden></p>
		<div class="ticket-actions">
			<button type="button" data-ticket-close>Annulla</button>
			<button type="submit" class="primary" data-ticket-submit>Invia</button>
		</div>
	</form>
	<div class="ticket-done" data-ticket-done hidden>
		<span class="material-icons" aria-hidden="true">check_circle</span>
		<p>Ticket <strong data-ticket-number></strong> aperto.<br>Le risposte arriveranno per mail a {{.Email}}.</p>
		<div class="ticket-actions"><button type="button" class="primary" data-ticket-close>Chiudi</button></div>
	</div>
	{{else}}
	<p>{{if eq .Problem "mail"}}Non posso aprire un ticket a tuo nome: nella rete del Comune manca la mail del tuo account.{{else if eq .Problem "ad"}}Non riesco a leggere i tuoi dati dalla rete del Comune: riprova tra poco.{{else}}Per aprire un ticket dalla plancia il Cruscotto deve riconoscerti (accesso dal PC dell'ufficio con Edge o Chrome; su Firefox segui l'aiuto in alto nella pagina).{{end}}</p>
	{{with .Fallback}}<p>Puoi sempre scrivere a <a href="mailto:{{.}}">{{.}}</a>.</p>{{end}}
	<div class="ticket-actions"><button type="button" class="primary" data-ticket-close>Chiudi</button></div>
	{{end}}
</dialog>
{{end}}{{end}}
```

Il messaggio «manca la mail» deve contenere la stringa `manca la mail` (test).

`dashboard.html`: in `plancia_head` aggiungere `<script src="/static/js/ticket.js" defer></script>`; in `.apps`, prima di `{{range .Categories}}`: `{{if .Ticket.Enabled}}<div class="category ticket-row"><div class="grid">{{template "ticket_tile" .Ticket}}</div></div>{{end}}`; prima di `</body>`: `{{template "ticket_dialog" .Ticket}}`.

`web/static/js/ticket.js`:

```js
// Dialog «Apri un ticket»: allegati a pezzi (il proxy taglia oltre 1 MB),
// invio come form; ogni risposta del server è 200 + JSON.
(function () {
	"use strict";
	const dlg = document.querySelector("dialog.ticket");
	const opener = document.querySelector("[data-ticket-open]");
	if (!dlg || !opener) return;
	const form = dlg.querySelector("[data-ticket-form]");
	const DRAFT = "cruscotto-ticket-bozza";
	const files = []; // {id, nome, li}

	function readDraft() {
		try { return JSON.parse(sessionStorage.getItem(DRAFT) || "{}"); } catch (e) { return {}; }
	}
	function saveDraft() {
		if (!form) return;
		const d = { oggetto: form.oggetto.value, descrizione: form.descrizione.value, telefono: form.telefono.value };
		try { sessionStorage.setItem(DRAFT, JSON.stringify(d)); } catch (e) { /* storage non disponibile */ }
	}
	function clearDraft() {
		try { sessionStorage.removeItem(DRAFT); } catch (e) { /* storage non disponibile */ }
	}

	function showErr(campo, msg) {
		const el = dlg.querySelector('[data-campo="' + campo + '"]');
		if (!el) return;
		el.textContent = msg || "";
		el.hidden = !msg;
	}
	function clearErrors() {
		dlg.querySelectorAll("[data-campo]").forEach(function (el) { el.hidden = true; el.textContent = ""; });
		const g = dlg.querySelector("[data-ticket-error]");
		if (g) { g.hidden = true; g.textContent = ""; }
	}
	function general(msg) {
		const g = dlg.querySelector("[data-ticket-error]");
		g.textContent = msg;
		g.hidden = false;
	}

	async function post(url, body, type) {
		const res = await fetch(url, { method: "POST", body: body, headers: type ? { "Content-Type": type } : {}, credentials: "same-origin" });
		return res.json();
	}

	async function upload(file) {
		if (files.length >= 3) { showErr("allegati", "Massimo 3 allegati."); return; }
		const li = document.createElement("li");
		li.textContent = (file.name || "screenshot.png") + " — 0%";
		dlg.querySelector("[data-ticket-files]").append(li);
		try {
			const start = await post("/ticket/allegati", new URLSearchParams({ nome: file.name || "screenshot.png" }));
			if (!start.ok) throw new Error(start.error);
			for (let n = 0, off = 0; off < file.size; n++, off += start.chunk) {
				const r = await post("/ticket/allegati/" + start.id + "/pezzo?n=" + n, file.slice(off, off + start.chunk), "application/octet-stream");
				if (!r.ok) throw new Error(r.error);
				li.textContent = (file.name || "screenshot.png") + " — " + Math.min(100, Math.round((off + start.chunk) * 100 / file.size)) + "%";
			}
			const fin = await post("/ticket/allegati/" + start.id + "/fine", new URLSearchParams());
			if (!fin.ok) throw new Error(fin.error);
			const entry = { id: fin.id, li: li };
			files.push(entry);
			li.textContent = fin.nome + " ";
			const x = document.createElement("button");
			x.type = "button";
			x.className = "ticket-remove";
			x.setAttribute("aria-label", "Togli " + fin.nome);
			x.textContent = "×";
			x.addEventListener("click", function () { files.splice(files.indexOf(entry), 1); li.remove(); });
			li.append(x);
		} catch (e) {
			li.remove();
			showErr("allegati", (e && e.message) || "Caricamento non riuscito.");
		}
	}

	function resetForm() {
		files.splice(0).forEach(function (f) { f.li.remove(); });
		form.reset();
		clearErrors();
		form.hidden = false;
		dlg.querySelector("[data-ticket-done]").hidden = true;
	}

	opener.addEventListener("click", function () {
		if (form) {
			const d = readDraft();
			if (d.oggetto) form.oggetto.value = d.oggetto;
			if (d.descrizione) form.descrizione.value = d.descrizione;
			if (d.telefono) form.telefono.value = d.telefono;
		}
		dlg.showModal();
	});
	dlg.addEventListener("click", function (e) {
		if (e.target.closest("[data-ticket-close]")) dlg.close();
	});
	if (!form) return;

	form.addEventListener("input", saveDraft);
	const input = dlg.querySelector("[data-ticket-input]");
	dlg.querySelector("[data-ticket-attach]").addEventListener("click", function () { input.click(); });
	input.addEventListener("change", function () {
		Array.from(input.files).forEach(upload);
		input.value = "";
	});
	dlg.addEventListener("paste", function (e) {
		const items = (e.clipboardData && e.clipboardData.files) || [];
		if (items.length === 0) return;
		e.preventDefault();
		Array.from(items).forEach(upload);
	});

	const MSG = {
		limite: "Hai aperto molti ticket nell'ultima ora: riprova più tardi",
		otrs: "Il sistema di assistenza non risponde: riprova",
		ad: "Non riesco a leggere i tuoi dati dalla rete del Comune: riprova tra poco",
		anonimo: "Il Cruscotto non ti riconosce più: ricarica la pagina",
		mail: "Nella rete del Comune manca la mail del tuo account",
		spento: "L'apertura dei ticket non è attiva",
	};

	form.addEventListener("submit", async function (e) {
		e.preventDefault();
		clearErrors();
		const btn = dlg.querySelector("[data-ticket-submit]");
		btn.disabled = true;
		const body = new URLSearchParams({ oggetto: form.oggetto.value, descrizione: form.descrizione.value, telefono: form.telefono.value });
		files.forEach(function (f) { body.append("allegato", f.id); });
		try {
			const r = await post("/ticket", body);
			if (r.ok) {
				clearDraft();
				dlg.querySelector("[data-ticket-number]").textContent = r.numero;
				files.splice(0).forEach(function (f) { f.li.remove(); });
				form.reset();
				form.hidden = true;
				dlg.querySelector("[data-ticket-done]").hidden = false;
				return;
			}
			if (r.campi) {
				Object.keys(r.campi).forEach(function (k) { showErr(k, r.campi[k]); });
				return;
			}
			let msg = (MSG[r.errore] || "Invio non riuscito: riprova");
			msg += r.casella ? " o scrivi a " + r.casella + "." : ".";
			general(msg);
		} catch (err) {
			general("Invio non riuscito: controlla la connessione e riprova.");
		} finally {
			btn.disabled = false;
		}
	});
	dlg.addEventListener("close", function () {
		if (!dlg.querySelector("[data-ticket-done]").hidden) resetForm();
	});
})();
```

Nota: il test conta `try {`: la funzione `readDraft`/`saveDraft`/`clearDraft` ha 3 `try {` per 3 usi di `sessionStorage` (`getItem`, `setItem`, `removeItem`) più la costante `DRAFT` non usa la parola. Le occorrenze di `sessionStorage` sono 3 → rapporto rispettato. Gli allegati caricati e poi tolti con «×» restano sul server fino alla scadenza di un'ora (nessuna route di cancellazione: YAGNI).

`plancia.css`, in fondo:

```css
/* Ticket: tile dedicata e dialog. */
.ticket-row { margin-bottom: 1rem; }
.tile-ticket { background: #e8f0fe; }
.tile-ticket .tile-main { border: 0; background: none; font: inherit; text-align: left; cursor: pointer; width: 100%; }
.tile-ticket .tile-icon { background: #1565c0; color: #fff; }
dialog.ticket { border: 0; border-radius: 16px; padding: 1.3rem 1.4rem; width: min(560px, 94vw); max-height: 92vh; overflow-y: auto; box-shadow: 0 30px 80px rgba(0, 0, 0, .35); color: var(--p-text); }
dialog.ticket::backdrop { background: rgba(16, 24, 40, .45); }
dialog.ticket h2 { margin: 0 0 .8rem; font-size: 1.15rem; text-transform: none; letter-spacing: normal; color: var(--p-text); }
dialog.ticket label { display: block; margin: .7rem 0 .2rem; font-weight: 600; }
dialog.ticket input:not([type=file]), dialog.ticket textarea { display: block; width: 100%; box-sizing: border-box; margin-top: .25rem; font: inherit; padding: .5rem .6rem; border: 1px solid #c5ccd6; border-radius: 8px; }
.ticket-who { display: flex; gap: .6rem; align-items: center; margin: 0 0 .4rem; color: var(--p-text-2); }
.ticket-err { color: #b3261e; display: block; margin-top: .2rem; }
.ticket-files { margin-top: .9rem; }
.ticket-files ul { list-style: none; padding: 0; margin: .4rem 0 0; }
.ticket-files li { display: flex; align-items: center; gap: .4rem; }
.ticket-remove { border: 0; background: none; font-size: 1.2rem; cursor: pointer; }
.ticket-actions { display: flex; justify-content: flex-end; gap: .6rem; margin-top: 1.1rem; }
.ticket-done { text-align: center; }
.ticket-done > .material-icons { font-size: 3rem; color: #2e7d32; }
```

Verificare che `--p-text`/`--p-text-2` esistano in `plancia.css` (sono usate da `dialog.notify-ask`).

- [ ] **Step 4: Test verdi** (container `-run 'TestDashboard|TestTicket|TestCSP|TestPlancia'`) e sintassi JS: `node -e "new Function(require('fs').readFileSync('web/static/js/ticket.js','utf8'))"` → nessun errore.
- [ ] **Step 5: Commit** — `git add web internal/web && git commit -m "Plancia: tile e dialog «Apri un ticket»"`.

---

### Task 7: Pagina admin `/admin/ticket`

**Files:**
- Create: `internal/web/admin_ticket.go`
- Create: `web/templates/admin_ticket.html`
- Modify: `web/templates/admin_base.html` (voce nel rail dopo «Assistenza»)
- Modify: `internal/web/server.go` (route)
- Test: `internal/web/admin_ticket_test.go`

**Interfaces:**
- Consumes: `db.ListTickets(200)` (Task 3), `s.cfg.OTRS`, `s.renderPage`.
- Produces: `func otrsAgentBase(wsURL string) string` (taglia da `/nph-genericinterface.pl` in poi; `""` se assente o mock); `type adminTicketView struct { Enabled, Mock bool; Queue, Base string; Tickets []database.TicketSent }`.

- [ ] **Step 1: Test che falliscono** — `internal/web/admin_ticket_test.go`:

```go
package web

import (
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

func TestOTRSAgentBase(t *testing.T) {
	for in, want := range map[string]string{
		"https://otrs.example.it/otrs/nph-genericinterface.pl/Webservice/Prova": "https://otrs.example.it/otrs/",
		"mock":                       "",
		"https://otrs.example.it/x":  "",
	} {
		if got := otrsAgentBase(in); got != want {
			t.Errorf("otrsAgentBase(%q) = %q, atteso %q", in, got, want)
		}
	}
}

func TestAdminTicketPage(t *testing.T) {
	s, db := newTestServerWith(t, nil, func(o *Options) {
		o.Tickets = &otrs.Mock{}
		o.Config.OTRS.URL = "https://otrs.example.it/otrs/nph-genericinterface.pl/Webservice/Prova"
		o.Config.OTRS.Queue = "Coda prova"
	})
	db.RecordTicket(database.TicketSent{Username: "mrossi", Name: "Mario Rossi", Email: "mrossi@example.it", Subject: "Stampante",
		TicketID: "42", TicketNumber: "2026100800000042", CustomerSet: false, Attachments: 2, CreatedAt: fixedNow})
	c := login(t, s)
	body := do(t, s, "GET", "/admin/ticket", nil, c, nil).Body.String()
	for _, want := range []string{"Coda prova", "Mario Rossi", "Stampante", "2026100800000042",
		"https://otrs.example.it/otrs/index.pl?Action=AgentTicketZoom;TicketID=42", "Cliente non impostato", `href="/admin/ticket" aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	if strings.Contains(body, "Password") {
		t.Error("credenziali nella pagina")
	}
	off, _ := newTestServer(t, nil)
	if b := do(t, off, "GET", "/admin/ticket", nil, login(t, off), nil).Body.String(); !strings.Contains(b, "Modulo spento") {
		t.Error("stato spento non mostrato")
	}
}
```

Attenzione: in `html/template` il link con `;` dentro `href` resta così (non è un carattere da escapare), ma verificare l'output reale e adeguare l'asserzione se `html/template` lo normalizza.

- [ ] **Step 2: Verificare che falliscano** (container `-run 'TestOTRSAgentBase|TestAdminTicket'`) → FAIL.

- [ ] **Step 3: Implementazione** — `internal/web/admin_ticket.go`:

```go
package web

import (
	"net/http"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type adminTicketView struct {
	Enabled, Mock bool
	Queue, Base   string // Base: URL dell'interfaccia agenti di OTRS ("" = niente link)
	Tickets       []database.TicketSent
}

// otrsAgentBase: da …/otrs/nph-genericinterface.pl/Webservice/X a …/otrs/.
func otrsAgentBase(wsURL string) string {
	i := strings.Index(wsURL, "nph-genericinterface.pl")
	if i < 0 || !strings.HasPrefix(wsURL, "https://") {
		return ""
	}
	return wsURL[:i]
}

func (s *Server) handleAdminTickets(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListTickets(200)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, r, "admin_ticket.html", "ticket", adminTicketView{
		Enabled: s.ticketsEnabled(), Mock: s.cfg.OTRS.Mock(), Queue: s.cfg.OTRS.Queue,
		Base: otrsAgentBase(s.cfg.OTRS.URL), Tickets: list,
	})
}
```

`web/templates/admin_ticket.html`:

```html
{{define "admin_ticket.html"}}{{template "admin_top" .}}
<h1>Ticket</h1>
{{with .Body}}
{{if .Enabled}}<p class="flash">Modulo attivo{{if .Mock}} (OTRS finto: solo sviluppo){{end}}{{with .Queue}} · coda <strong>{{.}}</strong>{{end}}.</p>
{{else}}<p class="flash error">Modulo spento: imposta OTRS_URL, OTRS_USER, OTRS_PASSWORD e OTRS_QUEUE per mostrare la tile «Apri un ticket» in plancia.</p>{{end}}
<table class="list">
	<thead><tr><th>Data</th><th>Utente</th><th>Oggetto</th><th>Ticket</th><th>Allegati</th></tr></thead>
	<tbody>
	{{$base := .Base}}{{range .Tickets}}<tr>
		<td>{{fmtDate .CreatedAt}}</td>
		<td>{{.Name}}<br><small class="muted">{{.Username}}</small></td>
		<td>{{.Subject}}</td>
		<td>{{if $base}}<a href="{{$base}}index.pl?Action=AgentTicketZoom;TicketID={{.TicketID}}" target="_blank" rel="noopener">{{.TicketNumber}}</a>{{else}}{{.TicketNumber}}{{end}}{{if not .CustomerSet}}<br><span class="tag warn">Cliente non impostato in OTRS</span>{{end}}</td>
		<td>{{.Attachments}}</td>
	</tr>{{else}}<tr><td colspan="5" class="muted">Nessun ticket aperto dalla plancia.</td></tr>{{end}}
	</tbody>
</table>
<p class="hint">Ultimi 200 ticket aperti dalla plancia. L'identità è dichiarata (riconoscimento NTLM): le risposte di OTRS vanno comunque alla mail dell'utente in AD. Nessun bottone di prova: ogni invio crea un ticket vero.</p>
{{end}}
{{template "admin_bottom" .}}{{end}}
```

`admin_base.html`, dopo la riga di «Assistenza»:

```html
		<a href="/admin/ticket"{{if eq .Section "ticket"}} aria-current="page"{{end}}><span class="material-icons" aria-hidden="true">confirmation_number</span>Ticket</a>
```

Route in `server.go`: `s.mux.HandleFunc("GET /admin/ticket", s.requireAdmin(s.handleAdminTickets))`.

- [ ] **Step 4: Test verdi** (container `-run 'TestOTRSAgentBase|TestAdminTicket|TestAdmin'`) → PASS.
- [ ] **Step 5: Commit** — `git add internal/web web/templates && git commit -m "Admin: pagina Ticket con i ticket aperti dalla plancia"`.

---

### Task 8: Verifica completa, prova nel browser, documentazione e versione

**Files:**
- Modify: `CLAUDE.md` (sezione Architettura: voce `internal/otrs` e modulo ticket; route pubbliche `/ticket`, `/ticket/allegati*`; tabella `tickets_sent` v11 nell'elenco di `internal/database`; elenco sotto-progetti nel paragrafo Progetto)
- Modify: `publiccode.yml` (`softwareVersion: 0.12.0`, `releaseDate` = data del rilascio)
- Create (temporaneo, non committato): `scratchpad/e2e/ticket.mjs`

**Interfaces:**
- Consumes: tutto quanto sopra.

- [ ] **Step 1: Suite completa in container** — `MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)":/src -v "$(pwd -W)/.devcache":/cache -e GOPATH=/cache/gopath -e GOCACHE=/cache/build -e CGO_ENABLED=0 -w /src golang:1.27-alpine go test ./...` → tutti `ok`. Poi `go vet ./...` e `go build ./...`.

- [ ] **Step 2: Avvio locale con mock** — server con `LDAP_HOST=mock NTLM_DOMAIN=MOCK OTRS_URL=mock OTRS_FALLBACK_EMAIL=assistenza@example.it SESSION_SECRET=<32+ caratteri noti> SECURE_COOKIES=false go run ./cmd/server` (porta libera, es. `PORT=18091`). Cookie utente generato con la sonda temporanea descritta in CLAUDE.md (`cmd/cookieprobe`, da cancellare subito dopo).

- [ ] **Step 3: Prova Playwright** — script `ticket.mjs` nello scratchpad che, con il cookie `cruscotto_utente` iniettato:
  1. apre `/`, verifica la tile `[data-ticket-open]` e la apre;
  2. invia vuoto → compaiono gli errori sotto «Oggetto» e «Descrizione»;
  3. compila oggetto e descrizione con accenti, allega un PNG generato (`page.setInputFiles` su `[data-ticket-input]` con un buffer PNG minimo), attende la voce nell'elenco con «×»;
  4. invia → vede `[data-ticket-done]` con un numero `202610080000000…`;
  5. screenshot del dialog compilato e della conferma (da mostrare all'utente per decidere la posizione della tile).
  Comando: `docker run --rm -v <scratchpad>/e2e:/e2e -e BASE=http://host.docker.internal:18091 -e COOKIE=<valore> mcr.microsoft.com/playwright:v1.63.0-noble sh -c 'cd /tmp && npm i -s playwright@1.63.0 && node /e2e/ticket.mjs'`.

- [ ] **Step 4: Documentazione** — in `CLAUDE.md` aggiungere sotto Architettura:

```markdown
- `internal/otrs`: apertura dei ticket in OTRS (GenericInterface REST). `Create` = `TicketCreate` + `TicketUpdate` del cliente (con `CustomerUser` = mail, `TicketCreate` lo salva vuoto se la mail non è un customer user: il PostMaster invece scrive la mail). Risposta valida solo 200 + JSON senza `Error` (dietro revprx01 gli errori arrivano come pagina HTML); nessun redirect; credenziali mai nei log. `OTRS_URL=mock` → `otrs.Mock` (solo con LDAP mock). Host, web service, route, agente e coda **solo da env**, mai nel repo.
- Ticket in plancia: tile `ticket_tile` in testa agli applicativi e `dialog.ticket` (`ticket.js`), solo con `OTRS_URL`. Richiedente da AD (nome dal cookie, `mail` e `telephoneNumber` dal profilo: `profileFor` li chiede sempre), mai dal form. `POST /ticket` sempre 200 + JSON (`numero` | `campi` | `errore`: anonimo, ad, mail, limite, otrs, spento; `casella` = `OTRS_FALLBACK_EMAIL`). Allegati a pezzi su `/ticket/allegati*` (PNG/JPEG/WebP/PDF, 5 MB, 3 file, 10 MB), legati allo username e monouso, temporanei in `UPLOAD_DIR/.tmp/ticket`. Limite 5 ticket/ora (`tickets_sent`, v11). Admin `/admin/ticket` (sola lettura, link allo zoom in OTRS).
```

  e aggiornare l'elenco dei sotto-progetti nel paragrafo Progetto («…ricerca web (0.11), apertura ticket verso OTRS (0.12); fase 2 ticket: i miei ticket e risposte in plancia»), l'elenco delle route pubbliche (`/ticket`, `/ticket/allegati`, `/ticket/allegati/{id}/pezzo`, `/ticket/allegati/{id}/fine`) e la frase sulle tabelle con `tickets_sent` (v11). `publiccode.yml`: `softwareVersion: 0.12.0`.

- [ ] **Step 5: Commit** — `git add CLAUDE.md publiccode.yml && git commit -m "Ticket: documentazione e versione 0.12.0"`. Nessun push né PR senza richiesta dell'utente.
