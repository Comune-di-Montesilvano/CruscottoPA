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
		"gbianchi":  {Username: "gbianchi", Attrs: map[string][]string{"mail": {"gbianchi@example.it"}}},
	},
}

func ticketTestServer(t *testing.T, m otrs.Client) (*Server, *http.Cookie) {
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
	if sent.Name != "Mario Rossi" || sent.Email != "mrossi@example.it" || sent.Phone != "0851234" || !strings.HasPrefix(sent.Body, "Non stampa più.\nÈ urgente\n\n— Informazioni sul PC —") {
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
