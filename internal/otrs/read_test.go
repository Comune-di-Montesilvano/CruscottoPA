package otrs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

var rome, _ = time.LoadLocation("Europe/Rome")

// noAttachments: senza Attachments=1 OTRS non manda il campo Attachment.
var noAttachments = regexp.MustCompile(`,\s*"Attachment":\[[^\]]*\]`)

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
			if _, ok := tickets[id]; !ok { // come OTRS 5 per un ticket inesistente
				io.WriteString(w, `{"Error":{"ErrorCode":"TicketGet.AccessDenied","ErrorMessage":"TicketGet: User does not have access to the ticket!"}}`)
				return
			}
			if body["Attachments"] != float64(1) {
				// senza allegati: nessun campo Attachment negli articoli
				io.WriteString(w, noAttachments.ReplaceAllString(tickets[id], ""))
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

// Un ticket che OTRS non riesce a leggere non blocca gli altri.
func TestChangedSkipsFailingTicket(t *testing.T) {
	c, _ := otrsRead(t, func(map[string]any) string { return `{"TicketID":["5","6"]}` }, map[string]string{
		"5": ticketJSON("5", "mrossi@example.it", "Coda prova", "open"),
		"6": `<html>errore</html>`,
	})
	got, err := c.Changed(context.Background(), time.Now())
	if !errors.Is(err, ErrOTRS) || len(got) != 1 || got[0].TicketID != "5" {
		t.Fatalf("parziale: %+v %v", got, err)
	}
}

// Ticket uniti o rimossi: mai mostrati né modificabili.
func TestMergedNotYours(t *testing.T) {
	c, _ := otrsRead(t, func(map[string]any) string { return `{"TicketID":["5"]}` }, map[string]string{
		"5": ticketJSON("5", "mrossi@example.it", "Coda prova", "merged"),
	})
	if _, err := c.Get(context.Background(), "mrossi@example.it", "5"); !errors.Is(err, ErrNotYours) {
		t.Fatalf("merged: %v", err)
	}
	if got, err := c.Mine(context.Background(), "mrossi@example.it"); err != nil || len(got) != 0 {
		t.Fatalf("merged in Mine: %+v %v", got, err)
	}
}

// Mail dell'operatore a un terzo (es. fornitore): visibile come in OTRS,
// ma marcata per non essere notificata come risposta.
func TestArticleToThirdParty(t *testing.T) {
	c := &HTTPClient{Loc: rome}
	tk := c.convert(rawTicket{CustomerUserID: "mrossi@example.it", Article: []rawArticle{
		{ArticleID: "1", ArticleType: "email-external", SenderType: "agent", To: "Fornitore <supporto@fornitore.example>"},
		{ArticleID: "2", ArticleType: "email-external", SenderType: "agent", To: "\"Mario Rossi\" <MRossi@example.it>"},
		{ArticleID: "3", ArticleType: "phone", SenderType: "agent"},
	}})
	if !tk.Articles[0].ToThirdParty || tk.Articles[1].ToThirdParty || tk.Articles[2].ToThirdParty {
		t.Fatalf("destinatari: %+v", tk.Articles)
	}
}

// Allegati troppo grandi per una risposta: il ticket si legge senza.
func TestGetTooLargeDropsAttachments(t *testing.T) {
	old := maxGetBytes
	maxGetBytes = 4000
	defer func() { maxGetBytes = old }()
	big := strings.Replace(ticketJSON("5", "mrossi@example.it", "Coda prova", "open"), "JVBERi0xLjQ=", strings.Repeat("A", 6000), 1)
	c, _ := otrsRead(t, func(map[string]any) string { return `{}` }, map[string]string{"5": big})
	tk, err := c.Get(context.Background(), "mrossi@example.it", "5")
	if err != nil || !tk.AttachmentsOmitted || len(tk.Articles) != 2 {
		t.Fatalf("degrado: %+v %v", tk, err)
	}
}

// Route di TicketGet senza :TicketID: l'ID viaggia nel corpo JSON.
func TestGetRouteWithoutParam(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "GET" || r.URL.Path != "/ws/TicketGet" || body["TicketID"] != "5" {
			http.Error(w, "<html>cortesia</html>", 500)
			return
		}
		io.WriteString(w, ticketJSON("5", "mrossi@example.it", "Coda prova", "open"))
	}))
	defer srv.Close()
	c := &HTTPClient{Config: config.OTRS{URL: srv.URL + "/ws", RouteGet: "/TicketGet", User: "a", Password: "b", Queue: "Coda prova"},
		HTTP: srv.Client(), Loc: rome}
	if tk, err := c.Get(context.Background(), "mrossi@example.it", "5"); err != nil || tk.TicketID != "5" {
		t.Fatalf("TicketGet senza parametro nella route: %+v %v", tk, err)
	}
}

// OTRS_URL=mock: ticket di esempio al primo accesso di ogni utente (sviluppo).
func TestDemoMockSeeds(t *testing.T) {
	m := NewDemoMock()
	got, err := m.Mine(context.Background(), "mrossi@example.it")
	if err != nil || len(got) < 3 {
		t.Fatalf("esempi: %+v %v", got, err)
	}
	states := map[string]bool{}
	agent := false
	for _, s := range got {
		states[s.StateType] = true
		tk, _ := m.Get(context.Background(), "mrossi@example.it", s.TicketID)
		for _, a := range tk.Articles {
			agent = agent || a.FromAgent
		}
	}
	if !states["new"] || !states["open"] || !states["closed"] || !agent {
		t.Fatalf("esempi poco vari: %v agent=%v", states, agent)
	}
	again, _ := m.Mine(context.Background(), "mrossi@example.it")
	if len(again) != len(got) {
		t.Fatal("esempi ricreati a ogni lettura")
	}
	if plain, _ := NewMock().Mine(context.Background(), "mrossi@example.it"); len(plain) != 0 {
		t.Fatal("NewMock senza esempi")
	}
}
