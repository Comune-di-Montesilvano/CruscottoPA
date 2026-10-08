# Ticket fase 2 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** in plancia i propri ticket OTRS (aperti e chiusi da ≤ 7 giorni), la conversazione con allegati, la risposta (che riapre i chiusi) e la notifica quando un operatore risponde.

**Architecture:** `internal/otrs` legge da OTRS (`TicketSearch`/`TicketGet`), filtra le note interne e controlla la proprietà; `internal/web` aggiunge widget HTMX, pagina `/ticket/{id}`, risposta, download degli allegati e un controllo periodico che notifica via SSE e Web Push. In locale solo stato minimo (visto, ultimo articolo notificato, mail → username), migrazione v13.

**Tech Stack:** Go 1.27, `modernc.org/sqlite`, `html/template`, HTMX 2, JS vanilla, Web Push esistente (`notify.Pusher`).

**Spec:** `docs/superpowers/specs/2026-10-08-ticket-fase2-design.md` (fase 1: `docs/superpowers/specs/2026-10-08-ticket-otrs-design.md`)

## Global Constraints

- Flussi pubblici sempre **HTTP 200** (+ JSON per le API): il proxy sostituisce 4xx/5xx.
- Nessun dato dell'ente nel repo: host, web service, coda, agente solo da env. Nei test `example.it` e nomi inventati (`mrossi`, `PC-PROVA-001`).
- Nuova env var = `docker-compose.yml` + `.env.example` + `internal/config`.
- Migrazioni solo in coda: `migrateV13TicketsPhase2`.
- CSP: niente script/stili inline; JS in `web/static/js/`.
- Articoli visibili al cliente: `ArticleType` ∈ {`email-external`, `phone`, `fax`, `webrequest`, `note-external`}. Tutto il resto non esce da `internal/otrs`.
- Stati aperti: `new`, `open`, `pending reminder`, `pending auto`; chiusi interrogati solo se chiusi da ≤ 7 giorni.
- Limiti: max 20 ticket per utente in `Mine`, 50 per giro in `Changed`; 10 risposte/ora per utente; un invio alla volta per utente (apertura o risposta); cache 60 s.
- Tempi: letture 15 s; risposta 25 s (update) dentro i 40 s dell'handler.
- Date OTRS: `2006-01-02 15:04:05` nell'ora locale configurata (`TZ`, default Europe/Rome).
- Test `internal/web` solo in container: `MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)":/src -v "$(pwd -W)/.devcache":/cache -e GOPATH=/cache/gopath -e GOCACHE=/cache/build -e CGO_ENABLED=0 -w /src golang:1.27-alpine go test -count=1 ./internal/web/ -run <Nome>`.

## Review Focus

- Ticket con `CustomerUserID` scritto con maiuscole diverse dalla mail AD: deve risultare dell'utente (test in Task 2).
- OTRS che restituisce un solo ID come stringa invece che come lista, o numeri al posto di stringhe: il parser li accetta (test in Task 2).
- Ticket di un'altra coda con la mail dell'utente: mai mostrato né modificabile (test in Task 2).
- Riavvio del server: nessuna notifica per risposte già esistenti (test in Task 8).
- Risposta del cliente stesso o nota interna dell'operatore: nessuna notifica (test in Task 8).

---

### Task 1: Configurazione delle route di lettura

**Files:**
- Modify: `internal/config/config.go`, `internal/config/config_test.go`, `docker-compose.yml`, `.env.example`

**Interfaces:**
- Produces: `config.OTRS.RouteSearch` (default `/TicketSearch`), `config.OTRS.RouteGet` (default `/Ticket/:TicketID`).

- [ ] **Step 1: Test che falliscono** — in `config_test.go` aggiungere `"OTRS_ROUTE_SEARCH", "OTRS_ROUTE_GET"` ad `allVars`; in `TestLoadOTRSDisabledByDefault` aggiungere il controllo dei default; in `TestLoadOTRSValidation` aggiungere i casi:

```go
	if cfg.OTRS.RouteSearch != "/TicketSearch" || cfg.OTRS.RouteGet != "/Ticket/:TicketID" {
		t.Fatalf("route di lettura: %+v", cfg.OTRS)
	}
```

```go
		{"OTRS_ROUTE_SEARCH", "TicketSearch", "OTRS_ROUTE_SEARCH"},
		{"OTRS_ROUTE_GET", "/Ticket", ":TicketID"},
		{"OTRS_ROUTE_GET", "Ticket/:TicketID", "OTRS_ROUTE_GET"},
```

- [ ] **Step 2: Rosso** — `go test ./internal/config/` → FAIL (`RouteSearch` non esiste).

- [ ] **Step 3: Implementazione** — in `config.OTRS` i campi

```go
	RouteSearch   string // route POST di TicketSearch
	RouteGet      string // route GET di TicketGet, con :TicketID
```

in `Load`:

```go
			RouteSearch:   getEnv("OTRS_ROUTE_SEARCH", "/TicketSearch"),
			RouteGet:      getEnv("OTRS_ROUTE_GET", "/Ticket/:TicketID"),
```

in `validate`, la lista delle route diventa `{{"OTRS_ROUTE_CREATE", o.RouteCreate}, {"OTRS_ROUTE_UPDATE", o.RouteUpdate}, {"OTRS_ROUTE_SEARCH", o.RouteSearch}, {"OTRS_ROUTE_GET", o.RouteGet}}` e dopo il ciclo:

```go
	if !strings.Contains(o.RouteGet, ":TicketID") {
		return errors.New("OTRS_ROUTE_GET deve contenere :TicketID")
	}
```

`docker-compose.yml` dopo `OTRS_ROUTE_UPDATE`:

```yaml
      - OTRS_ROUTE_SEARCH=${OTRS_ROUTE_SEARCH:-/TicketSearch}
      - OTRS_ROUTE_GET=${OTRS_ROUTE_GET:-/Ticket/:TicketID}
```

`.env.example` dopo `OTRS_ROUTE_UPDATE=/TicketUpdate`:

```
# Lettura dei propri ticket in plancia (POST per la ricerca, GET con :TicketID)
OTRS_ROUTE_SEARCH=/TicketSearch
OTRS_ROUTE_GET=/Ticket/:TicketID
```

e nel commento dell'agente: `# Agente dedicato con permessi create, ro e rw sulla coda`.

- [ ] **Step 4: Verde** — `go test ./internal/config/` → ok.
- [ ] **Step 5: Commit** — `git commit -m "Config: route OTRS per leggere i ticket"`.

---

### Task 2: Lettura dei ticket in `internal/otrs`

**Files:**
- Create: `internal/otrs/read.go`
- Modify: `internal/otrs/otrs.go` (`call` con limite di byte, `Loc`, interfaccia `Client`, `New`)
- Modify: `internal/otrs/mock.go` (metodi di lettura)
- Modify: `cmd/server/main.go` (`otrs.New(cfg.OTRS, cfg.Location)`)
- Test: `internal/otrs/read_test.go`

**Interfaces:**
- Consumes: `config.OTRS.RouteSearch`, `RouteGet`, `Queue` (Task 1).
- Produces:
  - `type Summary struct { TicketID, TicketNumber, Title, State, StateType string; Created, Changed, LastAgentArticle time.Time; Closed bool }`
  - `type Ticket struct { Summary; CustomerUserID string; Articles []Article }`
  - `type Article struct { ArticleID string; FromAgent bool; From, Subject, Body string; Created time.Time; Attachments []AttachmentInfo }`
  - `type AttachmentInfo struct { FileID, Filename, ContentType string; Size int64 }`
  - `var ErrNotYours = errors.New("ticket non disponibile")`
  - `Client` interface: `Create`, `Mine(ctx, email string) ([]Summary, error)`, `Get(ctx, email, ticketID string) (Ticket, error)`, `Changed(ctx, since time.Time) ([]Ticket, error)`, `Attachment(ctx, email, ticketID, articleID, fileID string) (Attachment, error)` (e `Reply` dal Task 3).
  - `func New(c config.OTRS, loc *time.Location) Client`; `HTTPClient.Loc *time.Location` (nil = `time.Local`).
  - `Mock.Tickets map[string]*Ticket` (per TicketID), `Mock.Queue string` non usato; `Mock.Err` vale anche per le letture.

- [ ] **Step 1: Test che falliscono** — `internal/otrs/read_test.go`:

```go
package otrs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

var rome, _ = time.LoadLocation("Europe/Rome")

// otrsRead simula TicketSearch e TicketGet. search riceve il corpo e risponde
// con gli ID; tickets è restituito da TicketGet per ID.
func otrsRead(t *testing.T, search func(body map[string]any) string, tickets map[string]string) (*HTTPClient, *[]map[string]any) {
	t.Helper()
	var searches []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/ws/TicketSearch":
			searches = append(searches, body)
			io.WriteString(w, search(body))
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/ws/Ticket/"):
			if body["UserLogin"] != "agente" || body["Password"] != "segreta" {
				io.WriteString(w, `{"Error":{"ErrorCode":"TicketGet.AuthFail"}}`)
				return
			}
			id := strings.TrimPrefix(r.URL.Path, "/ws/Ticket/")
			if body["Attachments"] != float64(1) {
				// senza allegati: nessun campo Attachment negli articoli
				io.WriteString(w, strings.ReplaceAll(tickets[id], `"Attachment":`, `"_Attachment":`))
				return
			}
			io.WriteString(w, tickets[id])
		default:
			http.Error(w, "<html>cortesia</html>", 500)
		}
	}))
	t.Cleanup(srv.Close)
	c := &HTTPClient{Config: config.OTRS{URL: srv.URL + "/ws", RouteSearch: "/TicketSearch", RouteGet: "/Ticket/:TicketID",
		RouteUpdate: "/TicketUpdate", User: "agente", Password: "segreta", Queue: "Coda prova"}, HTTP: srv.Client(), Loc: rome}
	return c, &searches
}

// ticketJSON: ticket OTRS 5 come lo restituisce TicketGet (numeri come stringhe).
func ticketJSON(id, customer, queue, stateType string) string {
	return `{"Ticket":[{"TicketID":"` + id + `","TicketNumber":"20261008000000` + id + `","Title":"Stampante ` + id + `",
"State":"` + stateType + `","StateType":"` + stateType + `","Queue":"` + queue + `","CustomerUserID":"` + customer + `",
"Created":"2026-10-08 10:00:00","Changed":"2026-10-08 12:00:00","Article":[
{"ArticleID":"100","ArticleType":"webrequest","SenderType":"customer","From":"\"Mario Rossi\" <mrossi@example.it>","Subject":"Stampante","Body":"Non stampa.","Created":"2026-10-08 10:00:00"},
{"ArticleID":"101","ArticleType":"note-internal","SenderType":"agent","From":"Operatore","Subject":"nota","Body":"SEGRETO interno","Created":"2026-10-08 11:00:00"},
{"ArticleID":"102","ArticleType":"email-external","SenderType":"agent","From":"Assistenza <ced@example.it>","Subject":"Re","Body":"Provi a riavviare.","Created":"2026-10-08 11:30:00",
 "Attachment":[{"Filename":"guida.pdf","ContentType":"application/pdf","FilesizeRaw":"9","Content":"JVBERi0xLjQ="}]},
{"ArticleID":103,"ArticleType":"note-report","SenderType":"system","From":"Sistema","Subject":"x","Body":"report","Created":"2026-10-08 11:40:00"}
]}]}`
}

func TestMine(t *testing.T) {
	c, searches := otrsRead(t, func(b map[string]any) string {
		if _, closed := b["TicketCloseTimeNewerDate"]; closed {
			return `{"TicketID":"7"}` // un solo ID come stringa: deve funzionare
		}
		return `{"TicketID":["5","6"]}`
	}, map[string]string{
		"5": ticketJSON("5", "MRossi@Example.it", "Coda prova", "open"),
		"6": ticketJSON("6", "mrossi@example.it", "Altra coda", "new"),
		"7": ticketJSON("7", "mrossi@example.it", "Coda prova", "closed"),
	})
	got, err := c.Mine(context.Background(), "mrossi@example.it")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].TicketID != "5" || got[1].TicketID != "7" || !got[1].Closed || got[0].Closed {
		t.Fatalf("ticket: %+v", got)
	}
	if want := time.Date(2026, 10, 8, 11, 30, 0, 0, rome); !got[0].LastAgentArticle.Equal(want) {
		t.Fatalf("ultima risposta dell'operatore: %v (la nota interna non conta)", got[0].LastAgentArticle)
	}
	if len(*searches) != 2 {
		t.Fatalf("ricerche: %v", *searches)
	}
	open := (*searches)[0]
	if open["CustomerUserLogin"] != "mrossi@example.it" || open["Queues"].([]any)[0] != "Coda prova" ||
		len(open["StateType"].([]any)) != 4 || open["Limit"] != float64(20) {
		t.Fatalf("ricerca aperti: %v", open)
	}
	closed := (*searches)[1]
	if closed["StateType"].([]any)[0] != "closed" || closed["TicketCloseTimeNewerDate"] == "" {
		t.Fatalf("ricerca chiusi: %v", closed)
	}
}

func TestGetFiltersAndOwnership(t *testing.T) {
	c, _ := otrsRead(t, func(map[string]any) string { return `{}` }, map[string]string{
		"5": ticketJSON("5", "mrossi@example.it", "Coda prova", "open"),
		"6": ticketJSON("6", "altro@example.it", "Coda prova", "open"),
		"8": ticketJSON("8", "mrossi@example.it", "Altra coda", "open"),
	})
	tk, err := c.Get(context.Background(), "MROSSI@example.it", "5")
	if err != nil {
		t.Fatal(err)
	}
	if len(tk.Articles) != 2 || tk.Articles[0].FromAgent || !tk.Articles[1].FromAgent {
		t.Fatalf("articoli: %+v", tk.Articles)
	}
	for _, a := range tk.Articles {
		if strings.Contains(a.Body, "SEGRETO") || strings.Contains(a.Body, "report") {
			t.Fatal("nota interna o di sistema esposta")
		}
	}
	att := tk.Articles[1].Attachments
	if len(att) != 1 || att[0].FileID != "1" || att[0].Filename != "guida.pdf" || att[0].Size != 9 {
		t.Fatalf("allegati: %+v", att)
	}
	for _, id := range []string{"6", "8", "999"} {
		if _, err := c.Get(context.Background(), "mrossi@example.it", id); !errors.Is(err, ErrNotYours) {
			t.Errorf("ticket %s: atteso ErrNotYours, ottenuto %v", id, err)
		}
	}
}

func TestAttachment(t *testing.T) {
	c, _ := otrsRead(t, func(map[string]any) string { return `{}` }, map[string]string{
		"5": ticketJSON("5", "mrossi@example.it", "Coda prova", "open"),
		"6": ticketJSON("6", "altro@example.it", "Coda prova", "open"),
	})
	a, err := c.Attachment(context.Background(), "mrossi@example.it", "5", "102", "1")
	if err != nil || string(a.Content) != "%PDF-1.4" || a.Filename != "guida.pdf" || a.ContentType != "application/pdf" {
		t.Fatalf("allegato: %+v %v", a, err)
	}
	for _, tc := range [][4]string{{"6", "102", "1"}, {"5", "101", "1"}, {"5", "102", "2"}} {
		if _, err := c.Attachment(context.Background(), "mrossi@example.it", tc[0], tc[1], tc[2]); !errors.Is(err, ErrNotYours) {
			t.Errorf("%v: atteso ErrNotYours, ottenuto %v", tc, err)
		}
	}
}

func TestChanged(t *testing.T) {
	since := time.Date(2026, 10, 8, 9, 0, 0, 0, rome)
	c, searches := otrsRead(t, func(map[string]any) string { return `{"TicketID":["5"]}` }, map[string]string{
		"5": ticketJSON("5", "mrossi@example.it", "Coda prova", "open"),
	})
	got, err := c.Changed(context.Background(), since)
	if err != nil || len(got) != 1 || got[0].CustomerUserID != "mrossi@example.it" || len(got[0].Articles) != 2 {
		t.Fatalf("changed: %+v %v", got, err)
	}
	s := (*searches)[0]
	if s["TicketChangeTimeNewerDate"] != "2026-10-08 09:00:00" || s["Limit"] != float64(50) || s["Queues"].([]any)[0] != "Coda prova" {
		t.Fatalf("ricerca: %v", s)
	}
}

func TestReadErrors(t *testing.T) {
	c, _ := otrsRead(t, func(map[string]any) string { return `{"Error":{"ErrorCode":"TicketSearch.AuthFail"}}` }, nil)
	if _, err := c.Mine(context.Background(), "mrossi@example.it"); !errors.Is(err, ErrOTRS) {
		t.Fatalf("errore OTRS: %v", err)
	}
	c.Config.RouteSearch = "/nonesiste"
	if _, err := c.Mine(context.Background(), "mrossi@example.it"); !errors.Is(err, ErrOTRS) {
		t.Fatalf("pagina HTML: %v", err)
	}
}

func TestMockRead(t *testing.T) {
	m := &Mock{Tickets: map[string]*Ticket{
		"5": {Summary: Summary{TicketID: "5", TicketNumber: "N5"}, CustomerUserID: "mrossi@example.it"},
		"6": {Summary: Summary{TicketID: "6", TicketNumber: "N6"}, CustomerUserID: "altro@example.it"},
	}}
	mine, _ := m.Mine(context.Background(), "MROSSI@example.it")
	if len(mine) != 1 || mine[0].TicketID != "5" {
		t.Fatalf("mock Mine: %+v", mine)
	}
	if _, err := m.Get(context.Background(), "mrossi@example.it", "6"); !errors.Is(err, ErrNotYours) {
		t.Fatal("mock Get: proprietà non controllata")
	}
}
```

- [ ] **Step 2: Rosso** — `go test ./internal/otrs/` → FAIL (metodi inesistenti).

- [ ] **Step 3: Implementazione**

In `otrs.go`:
- `call` prende un limite: `func (c *HTTPClient) call(ctx context.Context, method, route string, body map[string]any, out any, maxBytes int64) error` con `io.LimitReader(resp.Body, maxBytes)` (le chiamate di `Create` passano `1<<20`).
- campo `Loc *time.Location // ora locale di OTRS (nil = time.Local)` in `HTTPClient` e metodo `func (c *HTTPClient) loc() *time.Location { if c.Loc != nil { return c.Loc }; return time.Local }`.
- costante `ReadTimeout = 15 * time.Second`.
- `New`:

```go
func New(c config.OTRS, loc *time.Location) Client {
	if c.Mock() {
		return NewMock()
	}
	h := NewHTTPClient(c)
	h.Loc = loc
	return h
}
```

- `Client`:

```go
type Client interface {
	Create(ctx context.Context, t NewTicket) (Created, error)
	Mine(ctx context.Context, email string) ([]Summary, error)
	Get(ctx context.Context, email, ticketID string) (Ticket, error)
	Changed(ctx context.Context, since time.Time) ([]Ticket, error)
	Attachment(ctx context.Context, email, ticketID, articleID, fileID string) (Attachment, error)
	Reply(ctx context.Context, email, ticketID string, r NewReply) error
}
```

(`Reply` e `NewReply` arrivano nel Task 3: fino ad allora tenere `Reply` fuori dall'interfaccia, oppure aggiungere subito il tipo e uno stub che restituisce `ErrOTRS`; scelta consigliata: aggiungerlo nel Task 3.)

`internal/otrs/read.go`:

```go
package otrs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrNotYours: ticket di un altro utente, di un'altra coda o inesistente.
// Il messaggio è uguale per tutti i casi: non dice se il ticket esiste.
var ErrNotYours = errors.New("ticket non disponibile")

const (
	ReadTimeout = 15 * time.Second
	timeLayout  = "2006-01-02 15:04:05"
	maxMine     = 20
	maxChanged  = 50
	closedDays  = 7
	maxGetBytes = 40 << 20 // TicketGet con il contenuto degli allegati
)

var openStates = []string{"new", "open", "pending reminder", "pending auto"}

// visibleTypes: articoli che il cliente vede anche in OTRS. Note interne,
// *-internal, note-report e articoli di sistema non escono da qui.
var visibleTypes = map[string]bool{"email-external": true, "phone": true, "fax": true, "webrequest": true, "note-external": true}

type Summary struct {
	TicketID, TicketNumber, Title, State, StateType string
	Created, Changed, LastAgentArticle              time.Time
	Closed                                          bool
}

type Ticket struct {
	Summary
	CustomerUserID string
	Articles       []Article
}

type Article struct {
	ArticleID           string
	FromAgent           bool
	From, Subject, Body string
	Created             time.Time
	Attachments         []AttachmentInfo
}

type AttachmentInfo struct {
	FileID, Filename, ContentType string
	Size                          int64
}

// flexString accetta "5" e 5: OTRS 5 manda quasi tutto come stringa, non sempre.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*f = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = flexString(n.String())
	return nil
}

// flexList accetta ["5","6"] e "5" (una sola voce).
type flexList []string

func (l *flexList) UnmarshalJSON(b []byte) error {
	var many []flexString
	if err := json.Unmarshal(b, &many); err == nil {
		for _, s := range many {
			*l = append(*l, string(s))
		}
		return nil
	}
	var one flexString
	if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	if one != "" {
		*l = flexList{string(one)}
	}
	return nil
}

type rawAttachment struct {
	Filename, ContentType, Content flexString
	FilesizeRaw                    flexString
}

type rawArticle struct {
	ArticleID, ArticleType, SenderType, From, Subject, Body, Created flexString
	Attachment                                                       []rawAttachment
}

type rawTicket struct {
	TicketID, TicketNumber, Title, State, StateType, Queue, CustomerUserID, Created, Changed flexString
	Article                                                                                 []rawArticle
}

func (c *HTTPClient) search(ctx context.Context, q map[string]any) ([]string, error) {
	q["Queues"] = []string{c.Config.Queue}
	var out struct{ TicketID flexList }
	cctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()
	if err := c.call(cctx, http.MethodPost, c.Config.RouteSearch, q, &out, 1<<20); err != nil {
		return nil, err
	}
	return out.TicketID, nil
}

// getRaw: TicketGet con tutti gli articoli; con allegati solo se servono.
func (c *HTTPClient) getRaw(ctx context.Context, id string, attachments bool) (rawTicket, error) {
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return rawTicket{}, ErrNotYours
	}
	body := map[string]any{"AllArticles": 1, "Attachments": 0}
	if attachments {
		body["Attachments"] = 1
	}
	var out struct{ Ticket []rawTicket }
	cctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()
	route := strings.Replace(c.Config.RouteGet, ":TicketID", id, 1)
	if err := c.call(cctx, http.MethodGet, route, body, &out, maxGetBytes); err != nil {
		if strings.Contains(err.Error(), "TicketGet.") { // es. TicketGet.AccessDenied o ticket inesistente
			return rawTicket{}, ErrNotYours
		}
		return rawTicket{}, err
	}
	if len(out.Ticket) == 0 {
		return rawTicket{}, ErrNotYours
	}
	return out.Ticket[0], nil
}

func (c *HTTPClient) parseTime(s flexString) time.Time {
	t, _ := time.ParseInLocation(timeLayout, string(s), c.loc())
	return t
}

// convert: ticket con i soli articoli visibili al cliente.
func (c *HTTPClient) convert(r rawTicket) Ticket {
	t := Ticket{Summary: Summary{TicketID: string(r.TicketID), TicketNumber: string(r.TicketNumber), Title: string(r.Title),
		State: string(r.State), StateType: string(r.StateType), Created: c.parseTime(r.Created), Changed: c.parseTime(r.Changed),
		Closed: string(r.StateType) == "closed"}, CustomerUserID: string(r.CustomerUserID)}
	for _, a := range r.Article {
		if !visibleTypes[string(a.ArticleType)] || string(a.SenderType) == "system" {
			continue
		}
		art := Article{ArticleID: string(a.ArticleID), FromAgent: string(a.SenderType) == "agent", From: string(a.From),
			Subject: string(a.Subject), Body: string(a.Body), Created: c.parseTime(a.Created)}
		for i, at := range a.Attachment {
			size, _ := strconv.ParseInt(string(at.FilesizeRaw), 10, 64)
			art.Attachments = append(art.Attachments, AttachmentInfo{FileID: strconv.Itoa(i + 1),
				Filename: safeFilename(string(at.Filename)), ContentType: string(at.ContentType), Size: size})
		}
		if art.FromAgent && art.Created.After(t.LastAgentArticle) {
			t.LastAgentArticle = art.Created
		}
		t.Articles = append(t.Articles, art)
	}
	return t
}

// owned: il ticket è nella coda configurata e il cliente è email.
func (c *HTTPClient) owned(r rawTicket, email string) bool {
	return string(r.Queue) == c.Config.Queue && email != "" && strings.EqualFold(strings.TrimSpace(string(r.CustomerUserID)), strings.TrimSpace(email))
}

func (c *HTTPClient) Mine(ctx context.Context, email string) ([]Summary, error) {
	open, err := c.search(ctx, map[string]any{"CustomerUserLogin": email, "StateType": openStates,
		"SortBy": "Changed", "OrderBy": "Down", "Limit": maxMine})
	if err != nil {
		return nil, err
	}
	since := time.Now().In(c.loc()).AddDate(0, 0, -closedDays).Format(timeLayout)
	closed, err := c.search(ctx, map[string]any{"CustomerUserLogin": email, "StateType": []string{"closed"},
		"TicketCloseTimeNewerDate": since, "SortBy": "Changed", "OrderBy": "Down", "Limit": maxMine})
	if err != nil {
		return nil, err
	}
	out := []Summary{}
	for _, id := range append(open, closed...) {
		if len(out) == maxMine {
			break
		}
		r, err := c.getRaw(ctx, id, false)
		if errors.Is(err, ErrNotYours) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !c.owned(r, email) {
			continue
		}
		out = append(out, c.convert(r).Summary)
	}
	// Aperti prima dei chiusi, poi per ultima modifica.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Closed != out[j].Closed {
			return !out[i].Closed
		}
		return out[i].Changed.After(out[j].Changed)
	})
	return out, nil
}

func (c *HTTPClient) Get(ctx context.Context, email, id string) (Ticket, error) {
	r, err := c.getRaw(ctx, id, true)
	if err != nil {
		return Ticket{}, err
	}
	if !c.owned(r, email) {
		return Ticket{}, ErrNotYours
	}
	return c.convert(r), nil
}

func (c *HTTPClient) Changed(ctx context.Context, since time.Time) ([]Ticket, error) {
	ids, err := c.search(ctx, map[string]any{"TicketChangeTimeNewerDate": since.In(c.loc()).Format(timeLayout),
		"SortBy": "Changed", "OrderBy": "Down", "Limit": maxChanged})
	if err != nil {
		return nil, err
	}
	out := []Ticket{}
	for _, id := range ids {
		r, err := c.getRaw(ctx, id, false)
		if errors.Is(err, ErrNotYours) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if string(r.Queue) != c.Config.Queue {
			continue
		}
		out = append(out, c.convert(r))
	}
	return out, nil
}

func (c *HTTPClient) Attachment(ctx context.Context, email, id, articleID, fileID string) (Attachment, error) {
	r, err := c.getRaw(ctx, id, true)
	if err != nil {
		return Attachment{}, err
	}
	if !c.owned(r, email) {
		return Attachment{}, ErrNotYours
	}
	for _, a := range r.Article {
		if string(a.ArticleID) != articleID || !visibleTypes[string(a.ArticleType)] || string(a.SenderType) == "system" {
			continue
		}
		n, err := strconv.Atoi(fileID)
		if err != nil || n < 1 || n > len(a.Attachment) {
			return Attachment{}, ErrNotYours
		}
		at := a.Attachment[n-1]
		data, err := base64.StdEncoding.DecodeString(string(at.Content))
		if err != nil {
			return Attachment{}, fmt.Errorf("%w: allegato non valido", ErrOTRS)
		}
		return Attachment{Filename: safeFilename(string(at.Filename)), ContentType: string(at.ContentType), Content: data}, nil
	}
	return Attachment{}, ErrNotYours
}
```

Nota su `getRaw`: `call` restituisce errori come `"OTRS non disponibile: TicketGet.AccessDenied"`; per un ticket inesistente OTRS 5 risponde `{"Error":{"ErrorCode":"TicketGet.AccessDenied"...}}` o `TicketGet.InvalidParameter`: entrambi diventano `ErrNotYours`. Gli errori di rete e le pagine HTML restano `ErrOTRS`.

`mock.go`, aggiungere:

```go
// NewMock: OTRS finto per lo sviluppo, con un ticket di esempio per chi ne apre uno.
func NewMock() *Mock { return &Mock{Tickets: map[string]*Ticket{}} }
```

campi `Tickets map[string]*Ticket` e `Replies []MockReply` (Task 3), e i metodi (con `m.mu` e `m.Err`):

```go
func (m *Mock) Mine(_ context.Context, email string) ([]Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	out := []Summary{}
	for _, t := range m.Tickets {
		if strings.EqualFold(t.CustomerUserID, email) {
			out = append(out, t.Summary)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TicketID < out[j].TicketID })
	return out, nil
}

func (m *Mock) Get(_ context.Context, email, id string) (Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return Ticket{}, m.Err
	}
	t, ok := m.Tickets[id]
	if !ok || !strings.EqualFold(t.CustomerUserID, email) {
		return Ticket{}, ErrNotYours
	}
	return *t, nil
}

func (m *Mock) Changed(_ context.Context, since time.Time) ([]Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	out := []Ticket{}
	for _, t := range m.Tickets {
		if !t.Changed.Before(since) {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (m *Mock) Attachment(_ context.Context, email, id, articleID, fileID string) (Attachment, error) {
	t, err := m.Get(context.Background(), email, id)
	if err != nil {
		return Attachment{}, err
	}
	for _, a := range t.Articles {
		for _, at := range a.Attachments {
			if a.ArticleID == articleID && at.FileID == fileID {
				return Attachment{Filename: at.Filename, ContentType: at.ContentType, Content: []byte("contenuto di " + at.Filename)}, nil
			}
		}
	}
	return Attachment{}, ErrNotYours
}
```

In `Mock.Create`, oltre a registrare l'invio, creare il ticket in `m.Tickets` (se la mappa non è nil) con un articolo cliente, così in sviluppo il widget mostra subito il ticket aperto:

```go
	if m.Tickets != nil {
		now := time.Now()
		m.Tickets[c.TicketID] = &Ticket{Summary: Summary{TicketID: c.TicketID, TicketNumber: c.TicketNumber, Title: t.Subject,
			State: "new", StateType: "new", Created: now, Changed: now}, CustomerUserID: t.Email,
			Articles: []Article{{ArticleID: c.TicketID + "00", From: t.Name, Subject: t.Subject, Body: t.Body, Created: now}}}
	}
```

(`c` è il `Created` calcolato prima del return; riorganizzare `Create` di conseguenza.) Import `sort`, `strings`, `time`.

`cmd/server/main.go`: `tickets = otrs.New(cfg.OTRS, cfg.Location)`.

- [ ] **Step 4: Verde** — `go test ./internal/otrs/` → ok; `go build ./... && go vet ./...`.
- [ ] **Step 5: Commit** — `git commit -m "otrs: lettura dei propri ticket senza note interne"`.

---

### Task 3: Risposta in `internal/otrs`

**Files:**
- Modify: `internal/otrs/read.go` (o `reply.go`), `internal/otrs/otrs.go` (interfaccia), `internal/otrs/mock.go`
- Test: `internal/otrs/reply_test.go`

**Interfaces:**
- Consumes: `getRaw`, `owned` (Task 2), `UpdateTimeout`, `call`.
- Produces: `type NewReply struct { Name, Email, Body string; Attachments []Attachment }`; `Reply(ctx, email, ticketID string, r NewReply) error` nell'interfaccia; `Mock.Replies []MockReply` con `type MockReply struct { TicketID string; Reply NewReply; Reopened bool }`.

- [ ] **Step 1: Test che falliscono** — `internal/otrs/reply_test.go`:

```go
package otrs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

func otrsReply(t *testing.T, stateType string) (*HTTPClient, *map[string]any) {
	t.Helper()
	var update map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/ws/Ticket/"):
			id := strings.TrimPrefix(r.URL.Path, "/ws/Ticket/")
			owner := "mrossi@example.it"
			if id == "6" {
				owner = "altro@example.it"
			}
			io.WriteString(w, ticketJSON(id, owner, "Coda prova", stateType))
		case r.Method == "PATCH" && r.URL.Path == "/ws/TicketUpdate":
			update = body
			io.WriteString(w, `{"TicketID":"5","ArticleID":"200"}`)
		default:
			http.Error(w, "x", 500)
		}
	}))
	t.Cleanup(srv.Close)
	return &HTTPClient{Config: config.OTRS{URL: srv.URL + "/ws", RouteGet: "/Ticket/:TicketID", RouteUpdate: "/TicketUpdate",
		User: "agente", Password: "segreta", Queue: "Coda prova"}, HTTP: srv.Client(), Loc: rome}, &update
}

func TestReplyOpenTicket(t *testing.T) {
	c, update := otrsReply(t, "open")
	err := c.Reply(context.Background(), "mrossi@example.it", "5", NewReply{Name: "Mario Rossi", Email: "mrossi@example.it",
		Body: "Ancora non va.", Attachments: []Attachment{{Filename: "a.png", ContentType: "image/png", Content: []byte("x")}}})
	if err != nil {
		t.Fatal(err)
	}
	u := *update
	art := u["Article"].(map[string]any)
	if u["TicketID"] != "5" || art["Body"] != "Ancora non va." || art["SenderType"] != "customer" || art["ArticleType"] != "webrequest" ||
		art["From"] != `"Mario Rossi" <mrossi@example.it>` || len(u["Attachment"].([]any)) != 1 {
		t.Fatalf("update: %v", u)
	}
	if _, reopened := u["Ticket"]; reopened {
		t.Fatal("ticket aperto: nessun cambio di stato")
	}
}

func TestReplyReopensClosed(t *testing.T) {
	c, update := otrsReply(t, "closed")
	if err := c.Reply(context.Background(), "mrossi@example.it", "5", NewReply{Name: "M", Email: "mrossi@example.it", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if (*update)["Ticket"].(map[string]any)["State"] != "open" {
		t.Fatalf("riapertura: %v", *update)
	}
}

func TestReplyNotYours(t *testing.T) {
	c, update := otrsReply(t, "open")
	if err := c.Reply(context.Background(), "mrossi@example.it", "6", NewReply{Body: "x"}); !errors.Is(err, ErrNotYours) {
		t.Fatalf("ticket altrui: %v", err)
	}
	if *update != nil {
		t.Fatal("update inviato per un ticket altrui")
	}
}

func TestMockReply(t *testing.T) {
	m := NewMock()
	m.Tickets["5"] = &Ticket{Summary: Summary{TicketID: "5", StateType: "closed", Closed: true}, CustomerUserID: "mrossi@example.it"}
	if err := m.Reply(context.Background(), "mrossi@example.it", "5", NewReply{Body: "x"}); err != nil || len(m.Replies) != 1 || !m.Replies[0].Reopened {
		t.Fatalf("mock: %v %+v", err, m.Replies)
	}
	if m.Tickets["5"].Closed || len(m.Tickets["5"].Articles) != 1 {
		t.Fatalf("mock: ticket non aggiornato %+v", m.Tickets["5"])
	}
}
```

- [ ] **Step 2: Rosso** — `go test ./internal/otrs/ -run Reply` → FAIL.

- [ ] **Step 3: Implementazione** — in `read.go`:

```go
type NewReply struct {
	Name, Email, Body string
	Attachments       []Attachment
}

// Reply aggiunge un messaggio del cliente; un ticket chiuso torna "open".
func (c *HTTPClient) Reply(ctx context.Context, email, id string, rep NewReply) error {
	r, err := c.getRaw(ctx, id, false)
	if err != nil {
		return err
	}
	if !c.owned(r, email) {
		return ErrNotYours
	}
	upd := map[string]any{"TicketID": string(r.TicketID), "Article": map[string]any{
		"Subject": "Re: " + string(r.Title), "Body": rep.Body, "ContentType": "text/plain; charset=utf8",
		"ArticleType": "webrequest", "SenderType": "customer",
		"From": (&mail.Address{Name: rep.Name, Address: rep.Email}).String(),
	}}
	if string(r.StateType) == "closed" {
		upd["Ticket"] = map[string]any{"State": "open"}
	}
	if len(rep.Attachments) > 0 {
		atts := make([]map[string]any, len(rep.Attachments))
		for i, a := range rep.Attachments {
			atts[i] = map[string]any{"Filename": safeFilename(a.Filename), "ContentType": a.ContentType,
				"Content": base64.StdEncoding.EncodeToString(a.Content)}
		}
		upd["Attachment"] = atts
	}
	var ignored map[string]any
	cctx, cancel := context.WithTimeout(ctx, orDefault(c.CreateTimeout, CreateTimeout))
	defer cancel()
	return c.call(cctx, http.MethodPatch, c.Config.RouteUpdate, upd, &ignored, 1<<20)
}
```

(import `net/mail`). Aggiungere `Reply` all'interfaccia `Client`. In `mock.go`:

```go
type MockReply struct {
	TicketID string
	Reply    NewReply
	Reopened bool
}

func (m *Mock) Reply(_ context.Context, email, id string, r NewReply) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	t, ok := m.Tickets[id]
	if !ok || !strings.EqualFold(t.CustomerUserID, email) {
		return ErrNotYours
	}
	reopened := t.Closed
	if reopened {
		t.Closed, t.State, t.StateType = false, "open", "open"
	}
	now := time.Now()
	t.Changed = now
	t.Articles = append(t.Articles, Article{ArticleID: fmt.Sprintf("%s%02d", id, len(t.Articles)+1), From: r.Name, Body: r.Body, Created: now})
	m.Replies = append(m.Replies, MockReply{TicketID: id, Reply: r, Reopened: reopened})
	return nil
}
```

- [ ] **Step 4: Verde** — `go test ./internal/otrs/` → ok; `go build ./...`.
- [ ] **Step 5: Commit** — `git commit -m "otrs: risposta del cliente con riapertura dei chiusi"`.

---

### Task 4: Stato locale (migrazione v13)

**Files:**
- Modify: `internal/database/migrations.go`, `internal/database/tickets.go`
- Test: `internal/database/tickets_test.go`

**Interfaces:**
- Produces:
  - `TicketSent.Kind string` (`database.TicketOpen = "apertura"`, `database.TicketReply = "risposta"`); `RecordTicket` scrive `kind` (vuoto → `apertura`).
  - `CountTicketsSince(username, kind string, since time.Time) (int, error)` (firma cambiata: aggiornare i chiamanti).
  - `MarkTicketSeen(username, ticketID string, now time.Time) error`; `TicketSeen(username string) (map[string]time.Time, error)`.
  - `TicketNotifyState(ticketID string) (lastArticleID int64, ok bool, err error)`; `SetTicketNotifyState(ticketID string, lastArticleID int64, now time.Time) error`.
  - `UpsertTicketUser(email, username string, now time.Time) error`; `TicketUserFor(email string) (string, error)` (`""` se assente).
  - `CleanupTicketState(now time.Time) error` (righe più vecchie di 30 giorni nelle tre tabelle).

- [ ] **Step 1: Test che falliscono** — in `tickets_test.go`:

```go
func TestTicketsKindAndCount(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	db.RecordTicket(TicketSent{Username: "mrossi", Name: "M", Email: "m@example.it", Subject: "s", TicketID: "1", TicketNumber: "N1", CreatedAt: now})
	db.RecordTicket(TicketSent{Username: "mrossi", Name: "M", Email: "m@example.it", Subject: "s", TicketID: "1", TicketNumber: "N1", Kind: TicketReply, CreatedAt: now})
	open, _ := db.CountTicketsSince("mrossi", TicketOpen, now.Add(-time.Hour))
	replies, _ := db.CountTicketsSince("mrossi", TicketReply, now.Add(-time.Hour))
	if open != 1 || replies != 1 {
		t.Fatalf("conteggi: %d %d", open, replies)
	}
	list, _ := db.ListTickets(5)
	if list[0].Kind == "" || list[1].Kind == "" {
		t.Fatalf("kind: %+v", list)
	}
}

func TestTicketSeen(t *testing.T) {
	db := newTestDB(t)
	at := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	db.MarkTicketSeen("MRossi", "5", at)
	db.MarkTicketSeen("mrossi", "5", at.Add(time.Hour))
	seen, err := db.TicketSeen("mrossi")
	if err != nil || len(seen) != 1 || !seen["5"].Equal(at.Add(time.Hour)) {
		t.Fatalf("visti: %v %v", seen, err)
	}
}

func TestTicketNotifyStateAndUsers(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	if _, ok, _ := db.TicketNotifyState("5"); ok {
		t.Fatal("stato inesistente")
	}
	db.SetTicketNotifyState("5", 102, now)
	if last, ok, err := db.TicketNotifyState("5"); !ok || last != 102 || err != nil {
		t.Fatalf("stato: %d %v %v", last, ok, err)
	}
	db.UpsertTicketUser("MRossi@Example.it", "mrossi", now)
	if u, _ := db.TicketUserFor("mrossi@example.it"); u != "mrossi" {
		t.Fatalf("mail → utente: %q", u)
	}
	if u, _ := db.TicketUserFor("nessuno@example.it"); u != "" {
		t.Fatalf("mail sconosciuta: %q", u)
	}
	if err := db.CleanupTicketState(now.Add(31 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := db.TicketNotifyState("5"); ok {
		t.Fatal("pulizia: stato vecchio rimasto")
	}
	if u, _ := db.TicketUserFor("mrossi@example.it"); u != "" {
		t.Fatal("pulizia: utente vecchio rimasto")
	}
}
```

e nel test esistente `TestTicketsRecordCountList` la chiamata diventa `db.CountTicketsSince("mrossi", TicketOpen, now.Add(-time.Hour))`.

- [ ] **Step 2: Rosso** — `go test ./internal/database/` → FAIL (build).

- [ ] **Step 3: Implementazione** — migrazione in coda:

```go
// migrateV13TicketsPhase2: risposte nel registro, ticket visti, ultimo
// articolo notificato e associazione mail → utente per le notifiche.
func migrateV13TicketsPhase2(tx *sql.Tx) error {
	_, err := tx.Exec(`
ALTER TABLE tickets_sent ADD COLUMN kind TEXT NOT NULL DEFAULT 'apertura';
CREATE TABLE ticket_seen (
	username  TEXT NOT NULL,
	ticket_id TEXT NOT NULL,
	seen_at   TEXT NOT NULL,
	PRIMARY KEY (username, ticket_id)
);
CREATE TABLE ticket_notify_state (
	ticket_id       TEXT PRIMARY KEY,
	last_article_id INTEGER NOT NULL,
	updated_at      TEXT NOT NULL
);
CREATE TABLE ticket_users (
	email      TEXT PRIMARY KEY,
	username   TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
`)
	return err
}
```

`tickets.go`:

```go
const (
	TicketOpen  = "apertura"
	TicketReply = "risposta"
	ticketStateRetention = 30 * 24 * time.Hour
)
```

campo `Kind string` in `TicketSent`; `RecordTicket` aggiunge la colonna `kind` (valore `t.Kind`, o `TicketOpen` se vuoto); `ListTickets` legge `kind`; `CountTicketsSince` aggiunge `AND kind = ?`. Poi:

```go
func (db *DB) MarkTicketSeen(username, ticketID string, now time.Time) error {
	_, err := db.Exec(`INSERT INTO ticket_seen (username, ticket_id, seen_at) VALUES (?, ?, ?)
ON CONFLICT(username, ticket_id) DO UPDATE SET seen_at = excluded.seen_at`,
		strings.ToLower(strings.TrimSpace(username)), ticketID, formatTime(now))
	return err
}

func (db *DB) TicketSeen(username string) (map[string]time.Time, error) {
	rows, err := db.Query(`SELECT ticket_id, seen_at FROM ticket_seen WHERE username = ?`, strings.ToLower(strings.TrimSpace(username)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var id, at string
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		if out[id], err = parseTime(at); err != nil {
			return nil, err
		}
	}
	return out, rows.Err()
}

func (db *DB) TicketNotifyState(ticketID string) (int64, bool, error) {
	var last int64
	err := db.QueryRow(`SELECT last_article_id FROM ticket_notify_state WHERE ticket_id = ?`, ticketID).Scan(&last)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return last, err == nil, err
}

func (db *DB) SetTicketNotifyState(ticketID string, last int64, now time.Time) error {
	_, err := db.Exec(`INSERT INTO ticket_notify_state (ticket_id, last_article_id, updated_at) VALUES (?, ?, ?)
ON CONFLICT(ticket_id) DO UPDATE SET last_article_id = excluded.last_article_id, updated_at = excluded.updated_at`,
		ticketID, last, formatTime(now))
	return err
}

func (db *DB) UpsertTicketUser(email, username string, now time.Time) error {
	_, err := db.Exec(`INSERT INTO ticket_users (email, username, updated_at) VALUES (?, ?, ?)
ON CONFLICT(email) DO UPDATE SET username = excluded.username, updated_at = excluded.updated_at`,
		strings.ToLower(strings.TrimSpace(email)), strings.ToLower(strings.TrimSpace(username)), formatTime(now))
	return err
}

func (db *DB) TicketUserFor(email string) (string, error) {
	var u string
	err := db.QueryRow(`SELECT username FROM ticket_users WHERE email = ?`, strings.ToLower(strings.TrimSpace(email))).Scan(&u)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return u, err
}

// CleanupTicketState: via lo stato locale fermo da più di 30 giorni.
func (db *DB) CleanupTicketState(now time.Time) error {
	old := formatTime(now.Add(-ticketStateRetention))
	for _, q := range []string{
		`DELETE FROM ticket_seen WHERE seen_at < ?`,
		`DELETE FROM ticket_notify_state WHERE updated_at < ?`,
		`DELETE FROM ticket_users WHERE updated_at < ?`,
	} {
		if _, err := db.Exec(q, old); err != nil {
			return err
		}
	}
	return nil
}
```

(import `database/sql`, `errors`). Aggiornare i chiamanti di `CountTicketsSince` in `internal/web/ticket.go`: `s.db.CountTicketsSince(user, database.TicketOpen, …)`.

- [ ] **Step 4: Verde** — `go test ./internal/database/` → ok; `go build ./...`.
- [ ] **Step 5: Commit** — `git commit -m "database: stato locale dei ticket (v13)"`.

---

### Task 5: Widget «I miei ticket»

**Files:**
- Create: `internal/web/ticket_mine.go` (cache, widget, stati in italiano)
- Modify: `internal/web/server.go` (campo `ticketCache *ticketCache`, route `GET /partials/ticket`)
- Modify: `web/templates/partials_ticket.html` (template `widget_ticket`), `web/templates/dashboard.html` (contenitore del widget), `web/static/css/plancia.css`
- Test: `internal/web/ticket_mine_test.go`

**Interfaces:**
- Consumes: `otrs.Client.Mine`, `otrs.Summary`; `s.ticketRequester(r)` (fase 1); `db.TicketSeen`, `db.UpsertTicketUser`.
- Produces:
  - `type ticketCache struct` con `func (c *ticketCache) mine(key string, load func() ([]otrs.Summary, error)) ([]otrs.Summary, error)`, `func (c *ticketCache) ticket(key string, load func() (otrs.Ticket, error)) (otrs.Ticket, error)`, `func (c *ticketCache) forget(email string)`; TTL 60 s; errori non messi in cache.
  - `func stateLabel(stateType string) string` (funzione di template `ticketState`).
  - `type ticketRow struct { otrs.Summary; Unread bool }`; `type ticketWidget struct { Open, Closed []ticketRow; Down bool }`.

- [ ] **Step 1: Test che falliscono** — `internal/web/ticket_mine_test.go`:

```go
package web

import (
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

func mockWithTickets() *otrs.Mock {
	m := otrs.NewMock()
	m.Tickets["5"] = &otrs.Ticket{Summary: otrs.Summary{TicketID: "5", TicketNumber: "2026100800000005", Title: "Stampante ferma",
		StateType: "open", Changed: fixedNow, LastAgentArticle: fixedNow.Add(-time.Hour)}, CustomerUserID: "mrossi@example.it"}
	m.Tickets["7"] = &otrs.Ticket{Summary: otrs.Summary{TicketID: "7", TicketNumber: "2026100800000007", Title: "Password",
		StateType: "closed", Closed: true, Changed: fixedNow}, CustomerUserID: "mrossi@example.it"}
	m.Tickets["9"] = &otrs.Ticket{Summary: otrs.Summary{TicketID: "9", TicketNumber: "2026100800000009", Title: "Di un altro",
		StateType: "open"}, CustomerUserID: "altro@example.it"}
	return m
}

func TestTicketWidget(t *testing.T) {
	s, c := ticketTestServer(t, mockWithTickets())
	page := do(t, s, "GET", "/", nil, c, nil).Body.String()
	if !strings.Contains(page, `hx-get="/partials/ticket"`) {
		t.Fatal("contenitore del widget assente")
	}
	body := do(t, s, "GET", "/partials/ticket", nil, c, nil).Body.String()
	for _, want := range []string{"Stampante ferma", "/ticket/5", "In lavorazione", "Chiusi di recente", "Password", "ticket-unread"} {
		if !strings.Contains(body, want) {
			t.Errorf("widget: manca %q", want)
		}
	}
	if strings.Contains(body, "Di un altro") {
		t.Error("widget: ticket di un altro utente")
	}
	if u, _ := s.db.TicketUserFor("mrossi@example.it"); u != "mrossi" {
		t.Errorf("mail → utente non registrata: %q", u)
	}
	// Visto dopo l'ultima risposta: niente pallino.
	s.db.MarkTicketSeen("mrossi", "5", fixedNow)
	s.ticketCache.forget("mrossi@example.it")
	if body := do(t, s, "GET", "/partials/ticket", nil, c, nil).Body.String(); strings.Contains(body, "ticket-unread") {
		t.Error("pallino anche dopo la lettura")
	}
}

func TestTicketWidgetHiddenOrDown(t *testing.T) {
	m := mockWithTickets()
	s, _ := ticketTestServer(t, m)
	if body := do(t, s, "GET", "/partials/ticket", nil, nil, nil).Body.String(); strings.Contains(body, "/ticket/") {
		t.Error("anonimo: widget con ticket")
	}
	sm := viewerCookie(t, s, identity.User{Username: "senzamail", Name: "S"})
	if body := do(t, s, "GET", "/partials/ticket", nil, sm, nil).Body.String(); strings.Contains(body, "/ticket/") {
		t.Error("senza mail: widget con ticket")
	}
	m.Err = otrs.ErrOTRS
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	rec := do(t, s, "GET", "/partials/ticket", nil, c, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "non raggiungibile") {
		t.Fatalf("OTRS giù: %d %s", rec.Code, rec.Body)
	}
}

func TestTicketWidgetCache(t *testing.T) {
	m := mockWithTickets()
	s, c := ticketTestServer(t, m)
	do(t, s, "GET", "/partials/ticket", nil, c, nil)
	m.Err = otrs.ErrOTRS // in cache per 60 s: OTRS non viene richiamato
	if body := do(t, s, "GET", "/partials/ticket", nil, c, nil).Body.String(); !strings.Contains(body, "Stampante ferma") {
		t.Fatal("cache non usata")
	}
}
```

`ticketTestServer` (fase 1, `ticket_test.go`) va cambiato in modo che accetti un `otrs.Client` invece di `*otrs.Mock`:

```go
func ticketTestServer(t *testing.T, m otrs.Client) (*Server, *http.Cookie)
```

(i chiamanti esistenti passano già un `*otrs.Mock`).

- [ ] **Step 2: Rosso** (container `-run TestTicketWidget`) → FAIL.

- [ ] **Step 3: Implementazione** — `internal/web/ticket_mine.go`:

```go
package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

const ticketCacheTTL = 60 * time.Second

type cachedMine struct {
	list    []otrs.Summary
	expires time.Time
}

type cachedTicket struct {
	t       otrs.Ticket
	expires time.Time
}

// ticketCache: letture da OTRS in memoria per 60 s. Gli errori non si
// tengono: al prossimo caricamento si riprova.
type ticketCache struct {
	mu      sync.Mutex
	mineBy  map[string]cachedMine   // email minuscola
	tickets map[string]cachedTicket // email minuscola + "/" + id
	now     func() time.Time
}

func newTicketCache(now func() time.Time) *ticketCache {
	return &ticketCache{mineBy: map[string]cachedMine{}, tickets: map[string]cachedTicket{}, now: now}
}

func (c *ticketCache) mine(email string, load func() ([]otrs.Summary, error)) ([]otrs.Summary, error) {
	key := strings.ToLower(email)
	c.mu.Lock()
	e, ok := c.mineBy[key]
	c.mu.Unlock()
	if ok && c.now().Before(e.expires) {
		return e.list, nil
	}
	list, err := load()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.mineBy[key] = cachedMine{list: list, expires: c.now().Add(ticketCacheTTL)}
	c.mu.Unlock()
	return list, nil
}

func (c *ticketCache) ticket(email, id string, load func() (otrs.Ticket, error)) (otrs.Ticket, error) {
	key := strings.ToLower(email) + "/" + id
	c.mu.Lock()
	e, ok := c.tickets[key]
	c.mu.Unlock()
	if ok && c.now().Before(e.expires) {
		return e.t, nil
	}
	t, err := load()
	if err != nil {
		return otrs.Ticket{}, err
	}
	c.mu.Lock()
	c.tickets[key] = cachedTicket{t: t, expires: c.now().Add(ticketCacheTTL)}
	c.mu.Unlock()
	return t, nil
}

// forget: dopo una risposta (o una notifica) si rilegge da OTRS.
func (c *ticketCache) forget(email string) {
	key := strings.ToLower(email)
	c.mu.Lock()
	delete(c.mineBy, key)
	for k := range c.tickets {
		if strings.HasPrefix(k, key+"/") {
			delete(c.tickets, k)
		}
	}
	c.mu.Unlock()
}

func stateLabel(stateType string) string {
	switch {
	case stateType == "new":
		return "Nuovo"
	case stateType == "open":
		return "In lavorazione"
	case strings.HasPrefix(stateType, "pending"):
		return "In attesa"
	case stateType == "closed":
		return "Chiuso"
	}
	return stateType
}

type ticketRow struct {
	otrs.Summary
	Unread bool
}

type ticketWidget struct {
	Open, Closed []ticketRow
	Down         bool
}

// handleTicketWidget: widget caricato a parte (HTMX). Senza modulo, utente
// anonimo o senza mail: niente (200 vuoto).
func (s *Server) handleTicketWidget(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.ticketsEnabled() {
		return
	}
	req, problem := s.ticketRequester(r)
	if problem != "" {
		return
	}
	user := s.ticketUser(r)
	if err := s.db.UpsertTicketUser(req.Email, user, s.now()); err != nil {
		slog.Warn("ticket: mail → utente", "err", err)
	}
	list, err := s.ticketCache.mine(req.Email, func() ([]otrs.Summary, error) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*otrs.ReadTimeout)
		defer cancel()
		return s.tickets.Mine(ctx, req.Email)
	})
	if err != nil {
		if !errors.Is(err, otrs.ErrOTRS) {
			slog.Warn("ticket: elenco", "err", err)
		}
		s.render(w, http.StatusOK, "widget_ticket", ticketWidget{Down: true})
		return
	}
	seen, err := s.db.TicketSeen(user)
	if err != nil {
		slog.Warn("ticket: visti", "err", err)
	}
	var v ticketWidget
	for _, t := range list {
		row := ticketRow{Summary: t}
		if !t.LastAgentArticle.IsZero() {
			at, ok := seen[t.TicketID]
			row.Unread = !ok || t.LastAgentArticle.After(at)
		}
		if t.Closed {
			v.Closed = append(v.Closed, row)
		} else {
			v.Open = append(v.Open, row)
		}
	}
	s.render(w, http.StatusOK, "widget_ticket", v)
}
```

(import `context`). In `server.go`: campo `ticketCache *ticketCache`, in `New` `s.ticketCache = newTicketCache(o.Now)` (vicino a `s.profiles`), route `s.mux.HandleFunc("GET /partials/ticket", s.handleTicketWidget)`; in `render.go` `funcs()` aggiungere `"ticketState": stateLabel,`.

`partials_ticket.html`, in fondo:

```html
{{define "widget_ticket"}}
<h2 class="widget-title">I miei ticket</h2>
{{if .Down}}<p class="muted">Assistenza non raggiungibile, riprova più tardi.</p>
{{else}}
{{if .Open}}<ul class="ticket-list">{{range .Open}}{{template "ticket_row" .}}{{end}}</ul>
{{else}}<p class="muted">Nessun ticket aperto. <button type="button" class="linklike" data-ticket-open>Apri un ticket</button></p>{{end}}
{{with .Closed}}<details class="ticket-closed"><summary>Chiusi di recente ({{len .}})</summary><ul class="ticket-list">{{range .}}{{template "ticket_row" .}}{{end}}</ul></details>{{end}}
{{end}}
{{end}}

{{define "ticket_row"}}<li><a href="/ticket/{{.TicketID}}" class="ticket-link">
	{{if .Unread}}<span class="ticket-unread" title="Nuova risposta"></span>{{end}}
	<span class="ticket-title">{{.Title}}</span>
	<small>{{ticketState .StateType}} · {{shortDay .Changed}} · n. {{.TicketNumber}}</small>
</a></li>{{end}}
```

In `ticket.js` l'apertura del dialog deve funzionare con **qualsiasi** `[data-ticket-open]`, anche aggiunto dopo da HTMX: sostituire il listener sul singolo `opener` con una delega

```js
	document.addEventListener("click", function (e) {
		if (!e.target.closest("[data-ticket-open]")) return;
		// stesso corpo del vecchio listener di opener
	});
```

(lasciare il controllo iniziale `if (!dlg) return;` e togliere `opener` dalle condizioni di uscita).

`dashboard.html`, in `<aside class="widgets">` prima di `widget_guide`:

```html
			{{if and .Ticket.Enabled (eq .Ticket.Problem "")}}<section class="widget" id="widget-ticket" aria-label="I miei ticket" hx-get="/partials/ticket" hx-trigger="load, every 120s, ticket from:body" hx-swap="innerHTML"><h2 class="widget-title">I miei ticket</h2></section>{{end}}
```

`plancia.css`:

```css
/* Widget «I miei ticket». */
.ticket-list { list-style: none; margin: .4rem 0 0; padding: 0; }
.ticket-link { display: grid; grid-template-columns: auto 1fr; column-gap: .45rem; padding: .45rem .3rem; border-radius: 8px; color: inherit; text-decoration: none; }
.ticket-link:hover { background: var(--p-page); }
.ticket-link .ticket-title { grid-column: 2; font-weight: 600; }
.ticket-link small { grid-column: 2; color: var(--p-text-3); }
.ticket-unread { grid-row: 1; width: .55rem; height: .55rem; margin-top: .4rem; border-radius: 50%; background: var(--p-blue-2); }
.ticket-closed summary { cursor: pointer; color: var(--p-text-3); margin-top: .5rem; font-size: .85rem; }
.linklike { border: 0; background: none; padding: 0; color: var(--p-blue-2); font: inherit; cursor: pointer; text-decoration: underline; }
```

- [ ] **Step 4: Verde** (container `-run 'TestTicketWidget|TestTicket|TestDashboard'`) → ok; sintassi `ticket.js`.
- [ ] **Step 5: Commit** — `git commit -m "Plancia: widget «I miei ticket»"`.

---

### Task 6: Pagina del ticket e allegati

**Files:**
- Create: `internal/web/ticket_page.go`, `web/templates/ticket.html`
- Modify: `internal/web/server.go` (route), `web/static/css/plancia.css`
- Test: `internal/web/ticket_page_view_test.go`

**Interfaces:**
- Consumes: `otrs.Client.Get`, `otrs.Client.Attachment`, `otrs.ErrNotYours`; `s.ticketCache.ticket`; `s.ticketRequester`; `db.MarkTicketSeen`.
- Produces: route `GET /ticket/{id}` (template `ticket.html`, dati `ticketPageView{ Ticket otrs.Ticket; OK bool; Requester ticketRequester; Version string; Fallback string }`), `GET /ticket/{id}/allegati/{art}/{file}`.

- [ ] **Step 1: Test che falliscono** — `internal/web/ticket_page_view_test.go`:

```go
package web

import (
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

func mockConversation() *otrs.Mock {
	m := mockWithTickets()
	m.Tickets["5"].Articles = []otrs.Article{
		{ArticleID: "100", From: "Mario Rossi", Body: "Non stampa.\n<script>alert(1)</script>", Created: fixedNow.Add(-2 * time.Hour)},
		{ArticleID: "102", FromAgent: true, From: "Assistenza", Body: "Provi a riavviare.", Created: fixedNow.Add(-time.Hour),
			Attachments: []otrs.AttachmentInfo{{FileID: "1", Filename: "guida.pdf", ContentType: "application/pdf", Size: 9}}},
	}
	return m
}

func TestTicketPage(t *testing.T) {
	s, c := ticketTestServer(t, mockConversation())
	rec := do(t, s, "GET", "/ticket/5", nil, c, nil)
	body := rec.Body.String()
	for _, want := range []string{"Stampante ferma", "2026100800000005", "In lavorazione", "Provi a riavviare.",
		"/ticket/5/allegati/102/1", "guida.pdf", "data-ticket-reply", "&lt;script&gt;"} {
		if !strings.Contains(body, want) {
			t.Errorf("pagina: manca %q", want)
		}
	}
	if strings.Contains(body, "<script>alert") {
		t.Error("HTML degli articoli interpretato")
	}
	seen, _ := s.db.TicketSeen("mrossi")
	if _, ok := seen["5"]; !ok {
		t.Error("apertura della pagina non registrata come vista")
	}
}

func TestTicketPageNotAvailable(t *testing.T) {
	m := mockConversation()
	s, c := ticketTestServer(t, m)
	for _, path := range []string{"/ticket/9", "/ticket/999", "/ticket/abc"} {
		rec := do(t, s, "GET", path, nil, c, nil)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Ticket non disponibile") || strings.Contains(rec.Body.String(), "Di un altro") {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
	if rec := do(t, s, "GET", "/ticket/5", nil, nil, nil); !strings.Contains(rec.Body.String(), "Ticket non disponibile") {
		t.Error("anonimo: ticket mostrato")
	}
	m.Err = otrs.ErrOTRS
	s.ticketCache.forget("mrossi@example.it")
	if rec := do(t, s, "GET", "/ticket/5", nil, c, nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Ticket non disponibile") {
		t.Errorf("OTRS giù: %d", rec.Code)
	}
}

func TestTicketAttachmentDownload(t *testing.T) {
	s, c := ticketTestServer(t, mockConversation())
	rec := do(t, s, "GET", "/ticket/5/allegati/102/1", nil, c, nil)
	if rec.Code != 200 || rec.Body.String() != "contenuto di guida.pdf" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "object-src 'self'") {
		t.Fatalf("PDF: %d %q %v", rec.Code, rec.Body, rec.Header())
	}
	other := viewerCookie(t, s, identity.User{Username: "gbianchi", Name: "Giulia Bianchi"})
	if rec := do(t, s, "GET", "/ticket/5/allegati/102/1", nil, other, nil); rec.Code == 200 && rec.Body.String() == "contenuto di guida.pdf" {
		t.Fatal("allegato servito a un altro utente")
	}
}

func TestAttachmentHeaders(t *testing.T) {
	for ct, want := range map[string]string{
		"image/png":       "sandbox",
		"application/pdf": "object-src 'self'",
		"text/html":       "attachment",
	} {
		h := attachmentHeaders(ct, "x")
		if !strings.Contains(h.Get("Content-Security-Policy")+h.Get("Content-Disposition"), want) {
			t.Errorf("%s: %v", ct, h)
		}
		if ct == "text/html" && h.Get("Content-Type") != "application/octet-stream" {
			t.Errorf("text/html servito come %q", h.Get("Content-Type"))
		}
	}
}
```

`ticketDirectory` (fase 1) non conosce `gbianchi`: aggiungere a `ticketDirectory.profiles` `"gbianchi": {Username: "gbianchi", Attrs: map[string][]string{"mail": {"gbianchi@example.it"}}}`.

- [ ] **Step 2: Rosso** (container `-run 'TestTicketPage|TestTicketAttachment|TestAttachmentHeaders'`) → FAIL.

- [ ] **Step 3: Implementazione** — `internal/web/ticket_page.go`:

```go
package web

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

type ticketPageView struct {
	Ticket    otrs.Ticket
	OK        bool
	Requester ticketRequester
	Version   string
	Fallback  string
}

func (s *Server) handleTicketPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	v := ticketPageView{Version: s.version, Fallback: s.cfg.OTRS.FallbackEmail}
	req, problem := s.ticketRequester(r)
	if !s.ticketsEnabled() || problem != "" {
		s.render(w, http.StatusOK, "ticket.html", v)
		return
	}
	id := r.PathValue("id")
	t, err := s.ticketCache.ticket(req.Email, id, func() (otrs.Ticket, error) {
		ctx, cancel := context.WithTimeout(r.Context(), otrs.ReadTimeout)
		defer cancel()
		return s.tickets.Get(ctx, req.Email, id)
	})
	if err != nil {
		if !errors.Is(err, otrs.ErrNotYours) && !errors.Is(err, otrs.ErrOTRS) {
			slog.Warn("ticket: lettura", "err", err)
		}
		s.render(w, http.StatusOK, "ticket.html", v)
		return
	}
	if err := s.db.MarkTicketSeen(s.ticketUser(r), id, s.now()); err != nil {
		slog.Warn("ticket: visto", "err", err)
	}
	v.Ticket, v.OK, v.Requester = t, true, req
	s.render(w, http.StatusOK, "ticket.html", v)
}

// attachmentHeaders: immagini in sandbox, PDF come le guide, il resto solo
// come download (mai interpretato dal browser).
func attachmentHeaders(contentType, filename string) http.Header {
	h := http.Header{}
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-store")
	ct, _, _ := mime.ParseMediaType(contentType)
	switch {
	case ct == "image/png" || ct == "image/jpeg" || ct == "image/webp" || ct == "image/gif":
		h.Set("Content-Type", ct)
		h.Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	case ct == "application/pdf":
		h.Set("Content-Type", ct)
		h.Set("Content-Security-Policy", "default-src 'none'; object-src 'self'")
	default:
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	}
	return h
}

func (s *Server) handleTicketAttachment(w http.ResponseWriter, r *http.Request) {
	req, problem := s.ticketRequester(r)
	if !s.ticketsEnabled() || problem != "" {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), otrs.ReadTimeout)
	defer cancel()
	a, err := s.tickets.Attachment(ctx, req.Email, r.PathValue("id"), r.PathValue("art"), r.PathValue("file"))
	if err != nil {
		// 200 con un messaggio: un 404 lo sostituirebbe il proxy.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("Allegato non disponibile."))
		return
	}
	for k, vs := range attachmentHeaders(a.ContentType, strings.ReplaceAll(a.Filename, `"`, "")) {
		w.Header()[k] = vs
	}
	w.Write(a.Content)
}
```

Route in `server.go`:

```go
	s.mux.HandleFunc("GET /ticket/{id}", s.handleTicketPage)
	s.mux.HandleFunc("GET /ticket/{id}/allegati/{art}/{file}", s.handleTicketAttachment)
```

`web/templates/ticket.html`:

```html
{{define "ticket.html"}}<!doctype html>
<html lang="it">
<head>
	{{template "plancia_head"}}
	<title>{{if .OK}}{{.Ticket.Title}} · {{end}}Ticket · CruscottoPA{{template "title_ente"}}</title>
</head>
<body class="plancia ticket-page">
	<main class="ticket-main">
		<p><a href="/">← Plancia</a></p>
		{{if .OK}}
		<header class="ticket-head">
			<h1>{{.Ticket.Title}}</h1>
			<p class="muted">Ticket n. {{.Ticket.TicketNumber}} · {{ticketState .Ticket.StateType}} · aperto il {{fmtDate .Ticket.Created}}</p>
		</header>
		<ol class="ticket-thread">
			{{$id := .Ticket.TicketID}}{{range .Ticket.Articles}}<li class="ticket-msg{{if .FromAgent}} from-agent{{end}}">
				<div class="ticket-msg-head"><strong>{{if .FromAgent}}Assistenza{{else}}Tu{{end}}</strong> <small>{{fmtDate .Created}}</small></div>
				<div class="ticket-msg-body">{{.Body}}</div>
				{{$art := .ArticleID}}{{with .Attachments}}<ul class="ticket-msg-files">{{range .}}<li><a href="/ticket/{{$id}}/allegati/{{$art}}/{{.FileID}}" target="_blank" rel="noopener"><span class="material-icons" aria-hidden="true">attach_file</span>{{.Filename}}</a> <small>{{humanSize .Size}}</small></li>{{end}}</ul>{{end}}
			</li>{{end}}
		</ol>
		<form class="ticket-reply" data-ticket-reply="{{.Ticket.TicketID}}" novalidate>
			<h2>Rispondi</h2>
			{{if .Ticket.Closed}}<p class="muted">Il ticket è chiuso: rispondendo verrà riaperto.</p>{{end}}
			<label>Messaggio<textarea name="descrizione" rows="5" maxlength="10000" required></textarea></label>
			<small class="ticket-err" data-campo="descrizione" hidden></small>
			<div class="ticket-files">
				<button type="button" data-ticket-attach><span class="material-icons" aria-hidden="true">attach_file</span>Allega</button>
				<input type="file" accept="image/png,image/jpeg,image/webp,application/pdf" multiple hidden data-ticket-input>
				<ul data-ticket-files></ul>
				<small class="ticket-err" data-campo="allegati" hidden></small>
			</div>
			<p class="ticket-err ticket-general" data-ticket-error hidden></p>
			<div class="ticket-actions"><button type="submit" class="primary" data-ticket-submit>Invia risposta</button></div>
		</form>
		{{else}}
		<h1>Ticket non disponibile</h1>
		<p>Il ticket non esiste, non è tuo oppure il sistema di assistenza non risponde.{{with .Fallback}} Puoi scrivere a <a href="mailto:{{.}}">{{.}}</a>.{{end}}</p>
		{{end}}
	</main>
</body>
</html>{{end}}
```

`plancia.css`:

```css
/* Pagina del ticket. */
.ticket-main { max-width: 760px; margin: 0 auto; padding: 1.5rem 16px 3rem; }
.ticket-head h1 { margin: .3rem 0; font-size: 1.4rem; text-transform: none; letter-spacing: normal; color: var(--p-text); }
.ticket-thread { list-style: none; padding: 0; margin: 1.2rem 0; display: flex; flex-direction: column; gap: .8rem; }
.ticket-msg { background: #fff; border: 1px solid var(--p-line); border-radius: 12px; padding: .8rem 1rem; max-width: 85%; align-self: flex-end; }
.ticket-msg.from-agent { align-self: flex-start; background: #e8f0fe; border-color: #c6d8fb; }
.ticket-msg-head small { color: var(--p-text-3); margin-left: .4rem; }
.ticket-msg-body { white-space: pre-wrap; margin-top: .35rem; overflow-wrap: anywhere; }
.ticket-msg-files { list-style: none; padding: 0; margin: .5rem 0 0; }
.ticket-msg-files a { display: inline-flex; align-items: center; gap: .25rem; }
.ticket-reply { background: #fff; border: 1px solid var(--p-line); border-radius: 12px; padding: 1rem; }
.ticket-reply h2 { margin: 0 0 .5rem; font-size: 1.05rem; text-transform: none; letter-spacing: normal; color: var(--p-text); }
.ticket-reply textarea { display: block; width: 100%; box-sizing: border-box; margin-top: .25rem; font: inherit; padding: .5rem .6rem; border: 1px solid #c5ccd6; border-radius: 8px; }
.ticket-reply button { border: 1px solid var(--p-line); background: #fff; border-radius: 10px; padding: .55rem 1rem; font: inherit; cursor: pointer; display: inline-flex; align-items: center; gap: .3rem; }
.ticket-reply .primary { background: var(--p-blue-2); border-color: var(--p-blue-2); color: #fff; font-weight: 600; }
```

- [ ] **Step 4: Verde** (container `-run 'TestTicketPage|TestTicketAttachment|TestAttachmentHeaders|TestTicket'`) → ok.
- [ ] **Step 5: Commit** — `git commit -m "Ticket: pagina con la conversazione e gli allegati"`.

---

### Task 7: Risposta dalla pagina

**Files:**
- Create: `internal/web/ticket_reply.go`
- Modify: `internal/web/server.go` (route), `web/static/js/ticket.js` (modulo di risposta), `web/templates/admin_ticket.html` (colonna Tipo)
- Test: `internal/web/ticket_reply_test.go`

**Interfaces:**
- Consumes: `otrs.Client.Reply`, `otrs.NewReply`, `otrs.ErrNotYours`, `otrs.ErrOTRS`; `s.ticketSending` (un invio alla volta, fase 1); `takeTicketFiles`/`releaseTicketFiles`/`consumeTicketFiles`; `pcBlock`; `db.CountTicketsSince(user, database.TicketReply, …)`; `db.RecordTicket` con `Kind`; `s.ticketCache.forget`.
- Produces: route `POST /ticket/{id}/risposta` → JSON `{"ok":true}` | `{"ok":false,"campi":…}` | `{"ok":false,"errore":"anonimo|ad|mail|limite|in_corso|otrs|non_tuo|spento","casella":…}`; costante `repliesPerHour = 10`.

- [ ] **Step 1: Test che falliscono** — `internal/web/ticket_reply_test.go`:

```go
package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

func postReply(t *testing.T, s *Server, c *http.Cookie, id string, form url.Values) map[string]any {
	t.Helper()
	rec := do(t, s, "POST", "/ticket/"+id+"/risposta", form, c, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("JSON: %s", rec.Body)
	}
	return out
}

func TestTicketReply(t *testing.T) {
	m := mockConversation()
	m.Tickets["7"].Articles = nil
	s, c := ticketTestServer(t, m)
	do(t, s, "GET", "/partials/ticket", nil, c, nil) // riempie la cache
	id := uploadTicketFile(t, s, c, "a.png", pngBytes)["id"].(string)
	f := url.Values{"descrizione": {"Ancora non va."}, "browser": {"Edge 141"}, "allegato": {id}}
	if out := postReply(t, s, c, "7", f); out["ok"] != true {
		t.Fatalf("risposta: %v", out)
	}
	r := m.Replies[0]
	if r.TicketID != "7" || !r.Reopened || r.Reply.Email != "mrossi@example.it" || r.Reply.Name != "Mario Rossi" ||
		!strings.HasPrefix(r.Reply.Body, "Ancora non va.\n\n— Informazioni sul PC —") || len(r.Reply.Attachments) != 1 {
		t.Fatalf("inviata: %+v", r)
	}
	list, _ := s.db.ListTickets(1)
	if list[0].Kind != database.TicketReply || list[0].TicketID != "7" {
		t.Fatalf("registro: %+v", list[0])
	}
	if body := do(t, s, "GET", "/partials/ticket", nil, c, nil).Body.String(); strings.Contains(body, "Chiusi di recente") {
		t.Error("cache non svuotata dopo la risposta (il 7 è riaperto)")
	}
}

func TestTicketReplyRejects(t *testing.T) {
	m := mockConversation()
	s, c := ticketTestServer(t, m)
	if out := postReply(t, s, c, "9", url.Values{"descrizione": {"x"}}); out["errore"] != "non_tuo" {
		t.Errorf("ticket altrui: %v", out)
	}
	if out := postReply(t, s, c, "5", url.Values{"descrizione": {"  "}}); out["campi"] == nil {
		t.Errorf("testo vuoto: %v", out)
	}
	if out := postReply(t, s, nil, "5", url.Values{"descrizione": {"x"}}); out["errore"] != "anonimo" {
		t.Errorf("anonimo: %v", out)
	}
	for i := 0; i < repliesPerHour; i++ {
		if out := postReply(t, s, c, "5", url.Values{"descrizione": {"x"}}); out["ok"] != true {
			t.Fatalf("risposta %d: %v", i+1, out)
		}
	}
	if out := postReply(t, s, c, "5", url.Values{"descrizione": {"x"}}); out["errore"] != "limite" {
		t.Errorf("oltre il limite: %v", out)
	}
	m.Err = otrs.ErrOTRS
	s2, c2 := ticketTestServer(t, m)
	if out := postReply(t, s2, c2, "5", url.Values{"descrizione": {"x"}}); out["errore"] != "otrs" {
		t.Errorf("OTRS giù: %v", out)
	}
}

func TestReplyScript(t *testing.T) {
	js := readFile(t, "../../web/static/js/ticket.js")
	for _, want := range []string{"data-ticket-reply", "/risposta"} {
		if !strings.Contains(js, want) {
			t.Errorf("ticket.js: manca %q", want)
		}
	}
}
```

con, in fondo al file:

```go
func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
```

(import `os`).

- [ ] **Step 2: Rosso** (container `-run 'TestTicketReply|TestReplyScript'`) → FAIL.

- [ ] **Step 3: Implementazione** — `internal/web/ticket_reply.go`:

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

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

const repliesPerHour = 10

func (s *Server) handleTicketReply(w http.ResponseWriter, r *http.Request) {
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
	body := strings.TrimSpace(strings.ReplaceAll(r.FormValue("descrizione"), "\r\n", "\n"))
	switch n := utf8.RuneCountInString(body); {
	case n == 0:
		reply(map[string]any{"campi": map[string]string{"descrizione": "Scrivi la risposta."}})
		return
	case n > maxBody:
		reply(map[string]any{"campi": map[string]string{"descrizione": "Massimo 10.000 caratteri."}})
		return
	}
	user := s.ticketUser(r)
	if _, busy := s.ticketSending.LoadOrStore(user, true); busy {
		reply(map[string]any{"errore": "in_corso"})
		return
	}
	defer s.ticketSending.Delete(user)
	n, err := s.db.CountTicketsSince(user, database.TicketReply, s.now().Add(-time.Hour))
	if err != nil {
		slog.Error("ticket: conteggio risposte", "err", err)
		reply(map[string]any{"errore": "otrs"})
		return
	}
	if n >= repliesPerHour {
		reply(map[string]any{"errore": "limite"})
		return
	}
	ids := r.Form["allegato"]
	atts, _, err := s.takeTicketFiles(user, ids)
	if err != nil {
		reply(map[string]any{"campi": map[string]string{"allegati": "Allegati non validi: ricaricali."}})
		return
	}
	id := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), ticketSendTimeout)
	defer cancel()
	err = s.tickets.Reply(ctx, req.Email, id, otrs.NewReply{Name: req.Name, Email: req.Email,
		Body: body + pcBlock(req.PC, r), Attachments: atts})
	if err != nil {
		s.releaseTicketFiles(ids)
		if errors.Is(err, otrs.ErrNotYours) {
			reply(map[string]any{"errore": "non_tuo"})
			return
		}
		reply(map[string]any{"errore": "otrs"})
		return
	}
	s.consumeTicketFiles(ids)
	s.ticketCache.forget(req.Email)
	if err := s.db.RecordTicket(database.TicketSent{Username: user, Name: req.Name, Email: req.Email, Subject: "Risposta",
		TicketID: id, TicketNumber: id, CustomerSet: true, Attachments: len(atts), PC: req.PC, Kind: database.TicketReply,
		CreatedAt: s.now()}); err != nil {
		slog.Error("ticket: registro risposta", "err", err)
	}
	reply(map[string]any{"ok": true})
}
```

Nota: `TicketNumber` nel registro: usare il numero dal ticket in cache se presente (`s.ticketCache` → `Get`), altrimenti l'id; scelta minima consentita: id (la pagina admin linka comunque per TicketID).

Route: `s.mux.HandleFunc("POST /ticket/{id}/risposta", s.handleTicketReply)`.

`ticket.js`: estrarre l'upload in una funzione riusabile per un contenitore (`setupFiles(root)` che restituisce l'array `files` e gestisce `[data-ticket-attach]`, `[data-ticket-input]`, incolla) e usarla sia nel dialog sia nel modulo `[data-ticket-reply]`. Il modulo di risposta:

```js
	const replyForm = document.querySelector("[data-ticket-reply]");
	if (replyForm) {
		const replyFiles = setupFiles(replyForm);
		replyForm.addEventListener("submit", async function (e) {
			e.preventDefault();
			const btn = replyForm.querySelector("[data-ticket-submit]");
			btn.disabled = true;
			const body = new URLSearchParams({ descrizione: replyForm.descrizione.value,
				browser: pcInfo.browser, sistema: pcInfo.sistema, schermo: pcInfo.schermo });
			replyFiles.forEach(function (f) { body.append("allegato", f.id); });
			try {
				const r = await post("/ticket/" + replyForm.dataset.ticketReply + "/risposta", body);
				if (r.ok) { location.reload(); return; }
				if (r.campi) { Object.keys(r.campi).forEach(function (k) { showErrIn(replyForm, k, r.campi[k]); }); return; }
				generalIn(replyForm, (MSG[r.errore] || "Invio non riuscito: riprova") + (r.casella ? " o scrivi a " + r.casella + "." : "."));
			} catch (err) {
				generalIn(replyForm, "Invio non riuscito: controlla la connessione e riprova.");
			} finally {
				btn.disabled = false;
			}
		});
	}
```

con `showErrIn(root, campo, msg)` e `generalIn(root, msg)` versioni di `showErr`/`general` che cercano dentro `root` (il dialog usa `showErrIn(dlg, …)`). `pcInfo`, `post` e `MSG` vanno spostati fuori dal ramo del dialog (in cima all'IIFE) e il ritorno anticipato `if (!dlg) return;` va tolto: ogni parte controlla il proprio elemento. `MSG` aggiunge `non_tuo: "Questo ticket non è disponibile"`.

`admin_ticket.html`: colonna «Tipo» dopo «Data» (`<th>Tipo</th>` e `<td>{{.Kind}}</td>`), colspan del vuoto a 6.

- [ ] **Step 4: Verde** (container, tutto il pacchetto `./internal/web/`) → ok; sintassi `ticket.js`.
- [ ] **Step 5: Commit** — `git commit -m "Ticket: risposta dalla plancia con riapertura"`.

---

### Task 8: Notifiche di risposta

**Files:**
- Create: `internal/web/ticket_watch.go`
- Modify: `internal/notify/hub.go` (`Event` con `Kind`, `Body`, `URL`), `internal/web/events.go` (nome dell'evento SSE), `web/static/js/notifiche.js` (evento `ticket`), `internal/web/presence.go` (pulizia), `cmd/server/main.go` (`srv.StartTicketWatch(ctx)`)
- Test: `internal/web/ticket_watch_test.go`

**Interfaces:**
- Consumes: `otrs.Client.Changed`, `otrs.Ticket`/`Article`; `db.TicketNotifyState`, `SetTicketNotifyState`, `TicketUserFor`, `ListPushSubscriptionsFor`, `DeletePushSubscription`, `CleanupTicketState`; `s.hub.Broadcast`; `s.pusher` (`notify.Pusher`, nil = spento).
- Produces: `func (s *Server) StartTicketWatch(ctx context.Context)`; `func (s *Server) ticketWatchOnce(ctx context.Context, w *ticketWatcher) error`; `type ticketWatcher struct { since time.Time; primed bool }`; `notify.Event` campi `Kind string \`json:"kind,omitempty"\``, `Body string \`json:"body,omitempty"\``, `URL string \`json:"url,omitempty"\``.

- [ ] **Step 1: Test che falliscono** — `internal/web/ticket_watch_test.go`:

```go
package web

import (
	"context"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/notify"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

type fakePusher struct{ sent []string }

func (f *fakePusher) Send(_ context.Context, sub database.PushSubscription, payload []byte, _ bool) (bool, error) {
	f.sent = append(f.sent, sub.Username+" "+string(payload))
	return false, nil
}

func watchServer(t *testing.T) (*Server, *otrs.Mock, *fakePusher, *notify.Client) {
	t.Helper()
	m := mockConversation()
	s, _ := ticketTestServer(t, m)
	fp := &fakePusher{}
	s.pusher = fp
	s.db.UpsertTicketUser("mrossi@example.it", "mrossi", fixedNow)
	if _, err := s.db.SavePushSubscription(database.PushSubscription{Endpoint: "https://fcm.googleapis.com/x", P256dh: "k", Auth: "a", Username: "mrossi", CreatedAt: fixedNow}, 100); err != nil {
		t.Fatal(err)
	}
	c, err := s.hub.Subscribe("mrossi")
	if err != nil {
		t.Fatal(err)
	}
	return s, m, fp, c
}

func TestTicketWatchFirstRunSilent(t *testing.T) {
	s, _, fp, c := watchServer(t)
	w := &ticketWatcher{}
	if err := s.ticketWatchOnce(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if len(fp.sent) != 0 || len(c.Events) != 0 {
		t.Fatalf("primo giro: notifiche inviate %v", fp.sent)
	}
	if last, ok, _ := s.db.TicketNotifyState("5"); !ok || last != 102 {
		t.Fatalf("stato dopo il primo giro: %d %v", last, ok)
	}
}

func TestTicketWatchNotifiesAgentReplyOnce(t *testing.T) {
	s, m, fp, c := watchServer(t)
	w := &ticketWatcher{}
	s.ticketWatchOnce(context.Background(), w)
	tk := m.Tickets["5"]
	tk.Changed = fixedNow.Add(time.Minute)
	tk.Articles = append(tk.Articles,
		otrs.Article{ArticleID: "110", FromAgent: true, Body: "Fatto: ora funziona.", Created: fixedNow.Add(time.Minute)},
		otrs.Article{ArticleID: "111", FromAgent: false, Body: "Grazie", Created: fixedNow.Add(time.Minute)})
	s.ticketWatchOnce(context.Background(), w)
	if len(fp.sent) != 1 {
		t.Fatalf("push: %v", fp.sent)
	}
	select {
	case e := <-c.Events:
		if e.Kind != "ticket" || e.URL != "/ticket/5" || e.Body != "Fatto: ora funziona." {
			t.Fatalf("evento: %+v", e)
		}
	default:
		t.Fatal("nessun evento SSE")
	}
	s.ticketWatchOnce(context.Background(), w) // stesso articolo: niente di nuovo
	if len(fp.sent) != 1 {
		t.Fatalf("notifica ripetuta: %v", fp.sent)
	}
}

func TestTicketWatchCustomerReplyOrUnknownUser(t *testing.T) {
	s, m, fp, _ := watchServer(t)
	w := &ticketWatcher{}
	s.ticketWatchOnce(context.Background(), w)
	m.Tickets["5"].Changed = fixedNow.Add(time.Minute)
	m.Tickets["5"].Articles = append(m.Tickets["5"].Articles, otrs.Article{ArticleID: "120", Body: "mia risposta"})
	m.Tickets["9"].Changed = fixedNow.Add(time.Minute) // cliente senza utente in plancia
	m.Tickets["9"].Articles = []otrs.Article{{ArticleID: "130", FromAgent: true, Body: "x"}}
	s.ticketWatchOnce(context.Background(), w)
	if len(fp.sent) != 0 {
		t.Fatalf("notifiche non dovute: %v", fp.sent)
	}
}

func TestTicketWatchOTRSDownKeepsSince(t *testing.T) {
	s, m, _, _ := watchServer(t)
	w := &ticketWatcher{}
	s.ticketWatchOnce(context.Background(), w)
	before := w.since
	m.Err = otrs.ErrOTRS
	if err := s.ticketWatchOnce(context.Background(), w); err == nil || !w.since.Equal(before) {
		t.Fatalf("OTRS giù: %v %v→%v", err, before, w.since)
	}
}
```

- [ ] **Step 2: Rosso** (container `-run TestTicketWatch`) → FAIL.

- [ ] **Step 3: Implementazione**

`internal/notify/hub.go`, `Event`:

```go
type Event struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Level string `json:"level"`
	Kind  string `json:"kind,omitempty"` // "" = avviso, "ticket" = risposta a un ticket
	Body  string `json:"body,omitempty"`
	URL   string `json:"url,omitempty"`
}
```

`internal/web/events.go`, nel ciclo:

```go
		case e := <-c.Events:
			b, _ := json.Marshal(e)
			name := "avviso"
			if e.Kind != "" {
				name = e.Kind
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
```

`internal/web/ticket_watch.go`:

```go
package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/markdown"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/notify"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

const (
	ticketWatchEvery = 2 * time.Minute
	ticketWatchSlack = time.Minute // tolleranza sugli orologi
)

type ticketWatcher struct {
	since  time.Time
	primed bool
}

// StartTicketWatch: ogni 2 minuti cerca risposte degli operatori e le notifica.
func (s *Server) StartTicketWatch(ctx context.Context) {
	if !s.ticketsEnabled() {
		return
	}
	go func() {
		w := &ticketWatcher{}
		t := time.NewTicker(ticketWatchEvery)
		defer t.Stop()
		for {
			if err := s.ticketWatchOnce(ctx, w); err != nil {
				slog.Warn("ticket: controllo risposte", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func articleNum(id string) int64 {
	n, _ := strconv.ParseInt(id, 10, 64)
	return n
}

// ticketWatchOnce: un giro. Il primo registra lo stato senza notificare.
// Con OTRS giù since non avanza: il giro dopo rilegge lo stesso intervallo.
func (s *Server) ticketWatchOnce(ctx context.Context, w *ticketWatcher) error {
	start := s.now()
	since := w.since
	if !w.primed {
		since = start.AddDate(0, 0, -7)
	}
	cctx, cancel := context.WithTimeout(ctx, 4*otrs.ReadTimeout)
	defer cancel()
	tickets, err := s.tickets.Changed(cctx, since)
	if err != nil {
		return err
	}
	for _, t := range tickets {
		last, _, err := s.db.TicketNotifyState(t.TicketID)
		if err != nil {
			return err
		}
		var newest int64
		var fresh []otrs.Article
		for _, a := range t.Articles {
			n := articleNum(a.ArticleID)
			if n > newest {
				newest = n
			}
			if a.FromAgent && n > last {
				fresh = append(fresh, a)
			}
		}
		if newest <= last {
			continue
		}
		// Prima lo stato, poi l'invio: una risposta si notifica una volta sola.
		if err := s.db.SetTicketNotifyState(t.TicketID, newest, start); err != nil {
			return err
		}
		if !w.primed || len(fresh) == 0 {
			continue
		}
		user, err := s.db.TicketUserFor(t.CustomerUserID)
		if err != nil || user == "" {
			continue
		}
		s.ticketCache.forget(t.CustomerUserID)
		s.notifyTicketReply(ctx, user, t, fresh[len(fresh)-1])
	}
	w.primed = true
	w.since = start.Add(-ticketWatchSlack)
	return nil
}

func (s *Server) notifyTicketReply(ctx context.Context, user string, t otrs.Ticket, a otrs.Article) {
	id, _ := strconv.ParseInt(t.TicketID, 10, 64)
	ev := notify.Event{ID: id, Kind: "ticket", Title: "Risposta al ticket " + t.TicketNumber,
		Body: markdown.Plain(a.Body, 120), URL: "/ticket/" + t.TicketID}
	s.hub.Broadcast(ev, func(u string) bool { return u == user })
	if s.pusher == nil {
		return
	}
	subs, err := s.db.ListPushSubscriptionsFor(user)
	if err != nil {
		slog.Warn("ticket: iscrizioni push", "err", err)
		return
	}
	payload, _ := json.Marshal(map[string]string{"title": ev.Title, "body": ev.Body, "url": ev.URL, "tag": "ticket-" + t.TicketID})
	for _, sub := range subs {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		gone, err := s.pusher.Send(pctx, sub, payload, false)
		cancel()
		switch {
		case gone:
			s.db.DeletePushSubscription(sub.Endpoint)
		case err != nil:
			slog.Warn("ticket: invio push", "err", err)
		}
	}
}
```

(`markdown.Plain` produce testo semplice troncato a 120: se il corpo contiene caratteri Markdown li tratta come testo; accettabile. In alternativa una funzione locale che tronca le rune.)

`presence.go`, nel ciclo di `StartPresenceCleanup` dopo `s.CleanupPresence()`:

```go
			if err := s.db.CleanupTicketState(s.now()); err != nil {
				slog.Warn("pulizia stato ticket", "err", err)
			}
```

`cmd/server/main.go` dopo `srv.StartPresenceCleanup(ctx)`: `srv.StartTicketWatch(ctx)`.

`notifiche.js`: dopo `onAvviso` aggiungere

```js
	function onTicket(t, leader) {
		document.body.dispatchEvent(new Event("ticket")); // aggiorna il widget (hx-trigger "ticket from:body")
		if (leader && supported && active() && document.visibilityState !== "visible") {
			const opts = { body: t.body || "", icon: "/static/img/icon-192.png", tag: "ticket-" + t.id, data: { url: t.url } };
			navigator.serviceWorker.getRegistration().then((reg) => {
				if (reg) reg.showNotification(t.title, opts); else new Notification(t.title, opts);
			});
		}
	}
```

nello `stream`:

```js
			es.addEventListener("ticket", (e) => {
				let t = {};
				try { t = JSON.parse(e.data); } catch (_) { return; }
				onTicket(t, true);
				if (bc) bc.postMessage(t);
			});
```

e `bc.onmessage = (e) => (e.data && e.data.kind === "ticket" ? onTicket(e.data, false) : onAvviso(e.data, false));`.

Test in coda a `ticket_watch_test.go` per il JS:

```go
func TestTicketEventsScript(t *testing.T) {
	js := readFile(t, "../../web/static/js/notifiche.js")
	for _, want := range []string{`addEventListener("ticket"`, `kind === "ticket"`, `"ticket-" + t.id`} {
		if !strings.Contains(js, want) {
			t.Errorf("notifiche.js: manca %q", want)
		}
	}
}
```

(import `strings`).

- [ ] **Step 4: Verde** (container, tutto `./...`) → ok; sintassi `notifiche.js`, `ticket.js`.
- [ ] **Step 5: Commit** — `git commit -m "Ticket: notifica quando un operatore risponde"`.

---

### Task 9: Verifica, prova nel browser, documentazione

**Files:**
- Modify: `CLAUDE.md`; `.superpowers/.../e2e/ticket2.mjs` (temporaneo, non versionato)

- [ ] **Step 1: Suite completa** in container `go test ./...` e, per i test del ticket, `-race` con `golang:1.27` e `CGO_ENABLED=1`; `go vet ./...`.
- [ ] **Step 2: Server mock** — `LDAP_HOST=mock NTLM_DOMAIN=MOCK OTRS_URL=mock OTRS_FALLBACK_EMAIL=assistenza@example.it SESSION_SECRET=<noto> SECURE_COOKIES=false`, cookie `cruscotto_utente` con la sonda temporanea (`cmd/cookieprobe`, poi cancellata). Nel mock `otrs.NewMock()` crea il ticket all'apertura.
- [ ] **Step 3: Playwright** — apre la plancia, apre un ticket dal dialog, vede il ticket nel widget, apre `/ticket/{id}`, invia una risposta con allegato, verifica la pagina ricaricata con il nuovo messaggio; screenshot di widget e pagina; nessun errore in console.
- [ ] **Step 4: `CLAUDE.md`** — nella voce «Ticket in plancia» aggiungere: widget `/partials/ticket` (HTMX, cache 60 s), pagina `/ticket/{id}`, allegati `/ticket/{id}/allegati/{art}/{file}`, risposta `POST /ticket/{id}/risposta` (10/ora, riapre i chiusi), controllo ogni 2 min (`StartTicketWatch`, primo giro muto, evento SSE `ticket`, push con tag `ticket-*`), filtro degli articoli e `ErrNotYours` in `internal/otrs`, identità dichiarata come rischio accettato; route pubbliche aggiornate; tabelle v13; nuove env `OTRS_ROUTE_SEARCH`, `OTRS_ROUTE_GET`.
- [ ] **Step 5: Commit e push** — `git commit -m "Ticket fase 2: documentazione"` e `git push` sul branch `ticket-otrs` (la PR #30 resta in bozza: nessun merge).
