package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

func mario(t *testing.T, s *Server) string {
	t.Helper()
	return do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"}), nil).Body.String()
}

func TestMonthWidget(t *testing.T) {
	s, db := newTestServer(t, nil)
	// Ultimi 30 giorni dal 6 ottobre: dal 7 settembre compreso.
	for _, d := range []string{"2026-09-06", "2026-09-07", "2026-09-30", "2026-10-01", "2026-10-02"} {
		db.RecordDay("mrossi", d)
	}
	body := mario(t, s) // + oggi, 6 ottobre
	if !strings.Contains(body, "I tuoi ultimi 30 giorni") || !strings.Contains(body, `class="month-days">5<`) || !strings.Contains(body, "giorni di cruscotto") {
		t.Fatalf("widget:\n%s", widgetSnippet(body))
	}
}

func TestMonthWidgetAnonymous(t *testing.T) {
	s, _ := newTestServer(t, nil)
	for _, c := range []*http.Cookie{nil, viewerCookie(t, s, identity.User{Anonymous: true})} {
		if body := do(t, s, "GET", "/", nil, c, nil).Body.String(); strings.Contains(body, "widget-mese") {
			t.Fatal("widget per chi non è riconosciuto")
		}
	}
}

func TestBadges(t *testing.T) {
	s, db := newTestServer(t, nil)
	body := mario(t, s)
	if strings.Contains(body, `data-badge="avvisi"`) || strings.Contains(body, `data-badge="ticket"`) {
		t.Fatal("spille avvisi/ticket senza avvisi recenti o senza OTRS")
	}
	if !strings.Contains(body, `class="badge" data-badge="notifiche"`) || !strings.Contains(body, `class="badge" data-badge="app"`) {
		t.Fatal("spille da prendere assenti")
	}
	db.SetPresenceClient("mrossi", true, "granted", fixedNow)
	id := newAlert(t, db, "Lungo", longBody, database.LevelNews)
	body = mario(t, s)
	for _, want := range []string{`class="badge earned" data-badge="notifiche"`, `class="badge earned" data-badge="app"`, `class="badge" data-badge="avvisi"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("manca %s", want)
		}
	}
	db.MarkRead(id, "mrossi", database.ReadConfirm, fixedNow)
	if body = mario(t, s); !strings.Contains(body, `class="badge earned" data-badge="avvisi"`) {
		t.Fatal("avvisi letti: spilla non presa")
	}
}

func TestMonthTickets(t *testing.T) {
	s, c := ticketTestServer(t, otrs.NewMock())
	oct := fixedNow.Add(-24 * time.Hour)
	for _, r := range []database.TicketSent{
		{Username: "mrossi", TicketID: "1", Kind: database.TicketOpen, CreatedAt: oct},
		{Username: "mrossi", TicketID: "2", Kind: database.TicketOpen, CreatedAt: oct},
		{Username: "mrossi", TicketID: "1", Kind: database.TicketReply, CreatedAt: oct},
	} {
		s.db.RecordTicket(r)
	}
	s.db.RecordTicket(database.TicketSent{Username: "mrossi", TicketID: "3", Kind: database.TicketOpen, CreatedAt: fixedNow.AddDate(0, 0, -31)}) // fuori dal periodo
	s.db.SetClosed([]string{"1", "3"})
	body := do(t, s, "GET", "/", nil, c, nil).Body.String()
	if !strings.Contains(body, "2 richieste inviate, 1 risolta</p>") || !strings.Contains(body, `class="badge earned" data-badge="ticket"`) {
		t.Fatalf("richieste:\n%s", widgetSnippet(body))
	}
}

func TestMonthNoTickets(t *testing.T) {
	s, c := ticketTestServer(t, otrs.NewMock())
	body := do(t, s, "GET", "/", nil, c, nil).Body.String()
	if !strings.Contains(body, "Hai bisogno di aiuto?") || !strings.Contains(body, `class="badge" data-badge="ticket"`) {
		t.Fatalf("senza richieste:\n%s", widgetSnippet(body))
	}
}

func TestAssistLine(t *testing.T) {
	s, c := ticketTestServer(t, otrs.NewMock())
	d := 2 * time.Hour
	s.db.SaveTicketStats(database.TicketStats{Requests: 64, Resolved: 58, MedianFirst: &d, UpdatedAt: fixedNow})
	body := do(t, s, "GET", "/", nil, c, nil).Body.String()
	if !strings.Contains(body, "Per tutti: 64 richieste all'assistenza, 58 risolte · prima risposta di solito entro 2 ore") {
		t.Fatalf("riga assistenza:\n%s", widgetSnippet(body))
	}
}

func TestRoughDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second: "1 minuto", 45 * time.Minute: "45 minuti", time.Hour: "1 ora",
		90 * time.Minute: "2 ore", 47 * time.Hour: "47 ore", 72 * time.Hour: "3 giorni",
	} {
		if got := roughDuration(d); got != want {
			t.Errorf("%v: %q, atteso %q", d, got, want)
		}
	}
}

func TestStatsDisabledNoWidget(t *testing.T) {
	s, _ := newTestServerWith(t, nil, func(o *Options) { o.Config.StatsEnabled = false })
	if strings.Contains(mario(t, s), "widget-mese") {
		t.Fatal("widget con STATS_ENABLED=false")
	}
}

func widgetSnippet(body string) string {
	if i := strings.Index(body, "widget-mese"); i >= 0 {
		return body[max(0, i-40):min(len(body), i+1500)]
	}
	return "(widget assente)"
}

// Spilla degli avvisi: conta anche un avviso iniziato il mese scorso ma negli ultimi 30 giorni.
func TestBadgeAlertsLast30Days(t *testing.T) {
	s, db := newTestServer(t, nil)
	if _, err := db.CreateAlert(database.Alert{Title: "Settembre", Body: longBody, Level: database.LevelNews,
		StartsAt: time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC), CreatedAt: fixedNow}); err != nil {
		t.Fatal(err)
	}
	if body := mario(t, s); !strings.Contains(body, `class="badge" data-badge="avvisi"`) {
		t.Fatal("avviso degli ultimi 30 giorni non contato")
	}
}
