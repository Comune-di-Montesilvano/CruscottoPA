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
	// Correggendo un campo, il suo errore sparisce subito (non solo al prossimo invio).
	if !strings.Contains(src, "showErr(e.target.name, \"\")") {
		t.Error("ticket.js: l'errore del campo non si toglie mentre si scrive")
	}
}

// I bottoni del dialog hanno lo stile degli altri dialog della plancia.
func TestTicketCSS(t *testing.T) {
	css, err := os.ReadFile("../../web/static/css/plancia.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dialog.ticket button {", ".ticket-actions .primary {"} {
		if !strings.Contains(string(css), want) {
			t.Errorf("plancia.css: manca %q", want)
		}
	}
}
