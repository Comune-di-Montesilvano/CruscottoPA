package web

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

// mockMany: 7 ticket aperti (l'11 con una risposta non vista) e uno chiuso.
func mockMany() *otrs.Mock {
	m := otrs.NewMock()
	for i := 11; i <= 17; i++ {
		id := fmt.Sprint(i)
		m.Tickets[id] = &otrs.Ticket{Summary: otrs.Summary{TicketID: id, TicketNumber: "N" + id, Title: "Problema " + id,
			StateType: "open", Changed: fixedNow.Add(time.Duration(i) * time.Minute)}, CustomerUserID: "mrossi@example.it"}
	}
	m.Tickets["11"].LastAgentArticle = fixedNow.Add(-time.Hour)
	m.Tickets["20"] = &otrs.Ticket{Summary: otrs.Summary{TicketID: "20", TicketNumber: "N20", Title: "Chiuso da poco",
		StateType: "closed", Closed: true, Changed: fixedNow}, CustomerUserID: "mrossi@example.it"}
	return m
}

func TestTicketWidgetCompact(t *testing.T) {
	s, c := ticketTestServer(t, mockMany())
	body := do(t, s, "GET", "/partials/ticket", nil, c, nil).Body.String()
	if n := strings.Count(body, `class="ticket-link`); n != 5 {
		t.Fatalf("righe nel widget: %d, attese 5\n%s", n, body)
	}
	if i, j := strings.Index(body, "/ticket/11"), strings.Index(body, "/ticket/17"); i < 0 || i > j {
		t.Error("il ticket con una risposta non vista deve venire per primo")
	}
	if !strings.Contains(body, `href="/ticket"`) || !strings.Contains(body, "Tutti i miei ticket (8)") {
		t.Error("manca il link all'elenco con il totale")
	}
	if strings.Contains(body, "Chiuso da poco") {
		t.Error("i chiusi stanno nella pagina, non nel widget")
	}
}

func TestTicketListPage(t *testing.T) {
	s, c := ticketTestServer(t, mockMany())
	rec := do(t, s, "GET", "/ticket", nil, c, nil)
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	for i := 11; i <= 17; i++ {
		if !strings.Contains(body, fmt.Sprintf(`href="/ticket/%d"`, i)) {
			t.Errorf("manca il ticket %d", i)
		}
	}
	for _, want := range []string{"Chiusi negli ultimi 7 giorni", "Chiuso da poco", "data-ticket-open", `class="ticket"`} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	anon := do(t, s, "GET", "/ticket", nil, viewerCookie(t, s, identity.User{Anonymous: true}), nil)
	if anon.Code != 200 || !strings.Contains(anon.Body.String(), "non disponibile") {
		t.Errorf("anonimo: %d", anon.Code)
	}
}

func TestTicketAgo(t *testing.T) {
	now := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC) // 12:00 a Roma
	rome, _ := time.LoadLocation("Europe/Rome")
	for in, want := range map[time.Time]string{
		time.Date(2026, 10, 9, 7, 30, 0, 0, time.UTC):  "oggi alle 09:30",
		time.Date(2026, 10, 8, 16, 0, 0, 0, time.UTC):  "ieri",
		time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC):   "3 giorni fa",
		time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC):   "12/09/2026",
		time.Date(2026, 10, 8, 23, 30, 0, 0, time.UTC): "oggi alle 01:30",
	} {
		if got := ago(in, now, rome); got != want {
			t.Errorf("ago(%v) = %q, atteso %q", in, got, want)
		}
	}
}

func TestSplitPCBlock(t *testing.T) {
	m := splitPC("Non stampa.\n\n— Informazioni sul PC —\nPC: PC-PROVA-001\nBrowser: Edge 141\n\nTelefono / interno: 731")
	if m.Text != "Non stampa.\n\nTelefono / interno: 731" || m.PC != "PC: PC-PROVA-001\nBrowser: Edge 141" {
		t.Fatalf("diviso: %+v", m)
	}
	if m := splitPC("solo testo"); m.Text != "solo testo" || m.PC != "" {
		t.Fatalf("senza blocco: %+v", m)
	}
}

func TestTicketPageTracker(t *testing.T) {
	for state, step := range map[string]string{"new": "ricevuto", "open": "lavorazione", "pending reminder": "lavorazione", "closed": "risolto"} {
		m := mockConversation()
		m.Tickets["5"].StateType = state
		m.Tickets["5"].Closed = state == "closed"
		s, c := ticketTestServer(t, m)
		body := do(t, s, "GET", "/ticket/5", nil, c, nil).Body.String()
		if !strings.Contains(body, `data-step="`+step+`" aria-current="step"`) {
			t.Errorf("%s: tappa corrente %q mancante", state, step)
		}
	}
}

// Tappa centrale: «In attesa» per i pending, altrimenti «In lavorazione» (anche a ticket chiuso).
func TestTicketTrackerMiddleLabel(t *testing.T) {
	for state, want := range map[string]string{"closed": "In lavorazione", "pending reminder": "In attesa", "new": "In lavorazione"} {
		m := mockConversation()
		m.Tickets["5"].StateType = state
		s, c := ticketTestServer(t, m)
		body := do(t, s, "GET", "/ticket/5", nil, c, nil).Body.String()
		if !strings.Contains(body, `<span class="tracker-dot"></span>`+want+`</li>`) {
			t.Errorf("%s: tappa centrale attesa %q", state, want)
		}
	}
}

// Un solo bottone «indietro», uguale in tutte le pagine secondarie.
func TestBackButtons(t *testing.T) {
	m := mockConversation()
	s, c := ticketTestServer(t, m)
	s.db.CreateAlert(database.Alert{Title: "Avviso", Body: "x", Level: database.LevelNews, StartsAt: fixedNow.Add(-time.Hour)})
	alerts, _ := s.db.ListActiveAlerts(fixedNow)
	for path, want := range map[string]string{
		"/avvisi":                             `<a class="back-btn" href="/"><span class="material-icons" aria-hidden="true">arrow_back</span>Torna alla plancia</a>`,
		"/ticket":                             `<a class="back-btn" href="/"><span class="material-icons" aria-hidden="true">arrow_back</span>Torna alla plancia</a>`,
		"/ticket/5":                           `<a class="back-btn" href="/ticket"><span class="material-icons" aria-hidden="true">arrow_back</span>I miei ticket</a>`,
		"/avvisi/" + fmt.Sprint(alerts[0].ID): `<a class="back-btn" href="/avvisi"><span class="material-icons" aria-hidden="true">arrow_back</span>Tutti gli avvisi</a>`,
	} {
		if body := do(t, s, "GET", path, nil, c, nil).Body.String(); !strings.Contains(body, want) {
			t.Errorf("%s: bottone indietro mancante", path)
		}
	}
	css, _ := os.ReadFile("../../web/static/css/plancia.css")
	if !strings.Contains(string(css), ".back-btn {") || !strings.Contains(string(css), "min-height: 40px") {
		t.Error("stile del bottone indietro mancante")
	}
}

// «Apri un ticket» sta nel widget Assistenza in cima alla colonna destra, non in una tile.
func TestAssistWidgetPlacement(t *testing.T) {
	s, c := ticketTestServer(t, mockMany())
	page := do(t, s, "GET", "/", nil, c, nil).Body.String()
	if strings.Contains(page, "tile-ticket") {
		t.Error("la tile «Apri un ticket» non deve più esserci")
	}
	assist := strings.Index(page, `class="widget assist"`)
	cal := strings.Index(page, `id="widget-calendario"`)
	if assist < 0 || cal < 0 || assist > cal {
		t.Fatal("widget Assistenza assente o non in cima alla colonna")
	}
	block := page[assist:cal]
	for _, want := range []string{"Assistenza", `class="assist-open" data-ticket-open`, `hx-get="/partials/ticket"`} {
		if !strings.Contains(block, want) {
			t.Errorf("widget Assistenza: manca %q", want)
		}
	}
	anon := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Anonymous: true}), nil).Body.String()
	if !strings.Contains(anon, `class="assist-open" data-ticket-open`) || strings.Contains(anon, `hx-get="/partials/ticket"`) {
		t.Error("anonimo: bottone sì, elenco no")
	}
}
