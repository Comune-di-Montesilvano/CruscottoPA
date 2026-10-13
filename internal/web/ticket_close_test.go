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

func postClose(t *testing.T, s *Server, c *http.Cookie, id string, form url.Values) map[string]any {
	t.Helper()
	rec := do(t, s, "POST", "/ticket/"+id+"/chiudi", form, c, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("JSON: %s", rec.Body)
	}
	return out
}

func TestTicketClose(t *testing.T) {
	m := mockConversation()
	s, c := ticketTestServer(t, m)
	do(t, s, "GET", "/partials/ticket", nil, c, nil) // riempie la cache
	if out := postClose(t, s, c, "5", url.Values{"descrizione": {"Risolto, grazie."}}); out["ok"] != true {
		t.Fatalf("chiusura: %v", out)
	}
	tk := m.Tickets["5"]
	if !tk.Closed || len(m.Closes) != 1 {
		t.Fatalf("non chiuso: %+v", tk)
	}
	last := tk.Articles[len(tk.Articles)-1]
	if last.Body != "Ticket chiuso dall'utente tramite CruscottoPA.\n\nRisolto, grazie." || last.From != "Mario Rossi" {
		t.Fatalf("messaggio: %+v", last)
	}
	list, _ := s.db.ListTickets(1)
	if list[0].Kind != database.TicketClose || list[0].TicketID != "5" {
		t.Fatalf("registro: %+v", list[0])
	}
	if body := do(t, s, "GET", "/partials/ticket", nil, c, nil).Body.String(); strings.Contains(body, "/ticket/5\"") {
		t.Error("cache non svuotata dopo la chiusura (il 5 è ancora tra gli aperti)")
	}
}

func TestTicketCloseWithoutText(t *testing.T) {
	m := mockConversation()
	s, c := ticketTestServer(t, m)
	if out := postClose(t, s, c, "5", nil); out["ok"] != true {
		t.Fatalf("chiusura senza testo: %v", out)
	}
	if !m.Tickets["5"].Closed {
		t.Fatal("non chiuso")
	}
}

func TestTicketCloseRejects(t *testing.T) {
	m := mockConversation()
	s, c := ticketTestServer(t, m)
	if out := postClose(t, s, c, "9", nil); out["errore"] != "non_tuo" {
		t.Errorf("ticket altrui: %v", out)
	}
	if out := postClose(t, s, nil, "5", nil); out["errore"] != "anonimo" {
		t.Errorf("anonimo: %v", out)
	}
	if out := postClose(t, s, c, "5", url.Values{"descrizione": {strings.Repeat("a", maxBody+1)}}); out["campi"] == nil {
		t.Errorf("testo troppo lungo: %v", out)
	}
	if m.Tickets["5"].Closed {
		t.Fatal("chiuso nonostante gli errori")
	}
	m.Err = otrs.ErrOTRS
	if out := postClose(t, s, c, "5", nil); out["errore"] != "otrs" {
		t.Errorf("OTRS giù: %v", out)
	}
}

func TestTicketPageCloseButton(t *testing.T) {
	s, c := ticketTestServer(t, mockConversation())
	if body := do(t, s, "GET", "/ticket/5", nil, c, nil).Body.String(); !strings.Contains(body, `data-ticket-close="5"`) {
		t.Error("ticket aperto: manca «Chiudi il ticket»")
	}
	if body := do(t, s, "GET", "/ticket/7", nil, c, nil).Body.String(); strings.Contains(body, "data-ticket-close") {
		t.Error("ticket chiuso: «Chiudi il ticket» non deve comparire")
	}
}

func TestCloseScript(t *testing.T) {
	js := readFile(t, "../../web/static/js/ticket.js")
	for _, want := range []string{"data-ticket-close", "/chiudi"} {
		if !strings.Contains(js, want) {
			t.Errorf("ticket.js: manca %q", want)
		}
	}
}
