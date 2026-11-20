package web

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/notify"
)

func TestEventsStream(t *testing.T) {
	sseHeartbeat = 50 * time.Millisecond
	defer func() { sseHeartbeat = 25 * time.Second }()
	s, _ := newTestServer(t, nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/eventi", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("header: %v", resp.Header)
	}
	r := bufio.NewReader(resp.Body)
	if line, _ := r.ReadString('\n'); !strings.HasPrefix(line, ":") {
		t.Fatalf("primo heartbeat atteso, ottenuto %q", line)
	}
	s.hub.Broadcast(notify.Event{ID: 9, Title: "Sciopero", Level: "urgent"}, func(string) bool { return true })
	deadline := time.After(2 * time.Second)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, `"Sciopero"`) {
			break
		}
		select {
		case <-deadline:
			t.Fatal("evento non ricevuto")
		default:
		}
	}
}

func TestEventsCloseUnblocks(t *testing.T) {
	s, _ := newTestServer(t, nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/eventi")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	done := make(chan struct{})
	go func() { bufio.NewReader(resp.Body).ReadString(0); close(done) }()
	s.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close deve chiudere i flussi SSE (shutdown rapido)")
	}
}

func TestAlertVisibleTo(t *testing.T) {
	s, db := newTestServer(t, nil)
	db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	trib, _ := db.CreateAudienceGroup("Tributi")
	db.AddAudienceRule(database.AudienceRule{GroupID: trib, Kind: audience.KindAttr, Attr: "physicalDeliveryOfficeName", Value: "TRIBUTI"})
	pub, _ := db.CreateAlert(database.Alert{Title: "per tutti", Level: database.LevelNews, StartsAt: fixedNow})
	ris, _ := db.CreateAlert(database.Alert{Title: "solo tributi", Level: database.LevelNews, StartsAt: fixedNow})
	db.SetContentAudience(database.ContentAlert, ris, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{trib}})
	vis := func(u string, id int64) bool { v, _ := s.alertVisibleTo(u, id); return v }
	if !vis("", pub) || vis("", ris) {
		t.Fatal("anonimo: solo avvisi pubblici")
	}
	if !vis("mrossi", ris) || vis("senzanome", ris) {
		t.Fatal("riservato: visibile al membro, non agli altri")
	}
	if _, unsure := s.alertVisibleTo("senzanome", ris); unsure {
		t.Fatal("utente sconosciuto ad AD: risposta certa (non è AD giù)")
	}
}

func TestAlertVisibleToADDown(t *testing.T) {
	s, db := newTestServerWith(t, nil, func(o *Options) { o.Directory = fakeDirectory{err: errors.New("giù")} })
	trib, _ := db.CreateAudienceGroup("Tributi")
	db.AddAudienceRule(database.AudienceRule{GroupID: trib, Kind: audience.KindUser, Value: "mrossi"})
	pub, _ := db.CreateAlert(database.Alert{Title: "per tutti", Level: database.LevelNews, StartsAt: fixedNow})
	ris, _ := db.CreateAlert(database.Alert{Title: "solo tributi", Level: database.LevelNews, StartsAt: fixedNow})
	db.SetContentAudience(database.ContentAlert, ris, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{trib}})
	if v, unsure := s.alertVisibleTo("mrossi", pub); !v || unsure {
		t.Fatal("avviso pubblico: visibile anche con AD giù")
	}
	if v, unsure := s.alertVisibleTo("mrossi", ris); v || !unsure {
		t.Fatalf("avviso riservato con AD giù: atteso incerto, ottenuto %v %v", v, unsure)
	}
	if _, unsure := s.alertVisibleTo("", ris); unsure {
		t.Fatal("anonimo: risposta certa anche con AD giù")
	}
}

// Oltre il limite di connessioni: 200 con "retry" (un 503 lo riscriverebbe il
// proxy ed EventSource smetterebbe di riprovare).
func TestEventsTooManyAsksRetry(t *testing.T) {
	s, _ := newTestServer(t, nil)
	s.hub = notify.NewHub(0)
	rec := do(t, s, "GET", "/eventi", nil, nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "retry: 60000") {
		t.Fatalf("troppe connessioni: %d %q", rec.Code, rec.Body)
	}
}

// Close aspetta il dispatcher: dopo, nessuna query sul DB che si sta chiudendo.
func TestCloseWaitsForDispatcher(t *testing.T) {
	s, _ := newTestServer(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	s.StartNotifications(ctx)
	cancel()
	s.Close()
	select {
	case <-s.notifyDone:
	default:
		t.Fatal("Close è tornato con il dispatcher ancora attivo")
	}
}

// Il ping è un evento vero (non un commento SSE): il browser lo vede e capisce
// quando il flusso è morto senza errori (sospensione, cambio di rete).
func TestEventsPingIsEvent(t *testing.T) {
	sseHeartbeat = 50 * time.Millisecond
	defer func() { sseHeartbeat = 25 * time.Second }()
	s, _ := newTestServer(t, nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/eventi", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	r := bufio.NewReader(resp.Body)
	pings := 0
	for pings < 2 {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("ping ricevuti: %d (%v)", pings, err)
		}
		if line == "event: ping\n" {
			pings++
		}
	}
}

// notifiche.js si ricollega da solo: lo standard chiude per sempre l'EventSource
// su una risposta non SSE (la pagina di cortesia del proxy durante un riavvio),
// e senza ping per un po' il flusso va considerato morto. Al ritorno ricarica
// avvisi e ticket, per gli eventi persi nel frattempo.
func TestEventsScriptReconnects(t *testing.T) {
	js := readFile(t, "../../web/static/js/notifiche.js")
	for _, want := range []string{
		`es.readyState === EventSource.CLOSED`,
		`addEventListener("ping"`,
		`STALE_MS`,
		`refreshAll()`,
		`kind: "refresh"`,
		`"online"`,
		`"visibilitychange"`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("notifiche.js: manca %q", want)
		}
	}
}
