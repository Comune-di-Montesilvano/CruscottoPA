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

// Dopo un'apertura il widget mostra subito il nuovo ticket (cache svuotata).
func TestTicketWidgetAfterOpen(t *testing.T) {
	s, c := ticketTestServer(t, otrs.NewMock())
	if body := do(t, s, "GET", "/partials/ticket", nil, c, nil).Body.String(); !strings.Contains(body, "Nessun ticket aperto") {
		t.Fatalf("widget iniziale: %s", body)
	}
	if out := postTicket(t, s, c, validTicket()); out["ok"] != true {
		t.Fatalf("apertura: %v", out)
	}
	rec := do(t, s, "GET", "/partials/ticket", nil, c, nil)
	if body := rec.Body.String(); !strings.Contains(body, "Stampante") {
		t.Fatalf("widget dopo l'apertura: %s", body)
	}
}
