package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
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

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
