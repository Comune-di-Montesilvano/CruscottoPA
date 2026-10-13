package otrs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

func TestCloseOpenTicket(t *testing.T) {
	c, update := otrsReply(t, "open")
	err := c.Close(context.Background(), "mrossi@example.it", "5", NewClose{Name: "Mario Rossi", Email: "mrossi@example.it",
		Body: "Risolto riavviando."})
	if err != nil {
		t.Fatal(err)
	}
	u := *update
	if u["TicketID"] != "5" || u["Ticket"].(map[string]any)["State"] != CloseState {
		t.Fatalf("stato: %v", u)
	}
	art := u["Article"].(map[string]any)
	if art["Subject"] != "Chiuso dall'utente: Stampante 5" || art["SenderType"] != "customer" || art["ArticleType"] != "webrequest" ||
		art["HistoryType"] != "FollowUp" || art["From"] != `"Mario Rossi" <mrossi@example.it>` {
		t.Fatalf("articolo: %v", art)
	}
	if art["Body"] != "Ticket chiuso dall'utente tramite CruscottoPA.\n\nRisolto riavviando." {
		t.Fatalf("testo: %q", art["Body"])
	}
}

func TestCloseWithoutText(t *testing.T) {
	c, update := otrsReply(t, "open")
	if err := c.Close(context.Background(), "mrossi@example.it", "5", NewClose{Name: "M", Email: "mrossi@example.it"}); err != nil {
		t.Fatal(err)
	}
	if body := (*update)["Article"].(map[string]any)["Body"]; body != "Ticket chiuso dall'utente tramite CruscottoPA." {
		t.Fatalf("testo: %q", body)
	}
}

func TestCloseAlreadyClosed(t *testing.T) {
	c, update := otrsReply(t, "closed")
	if err := c.Close(context.Background(), "mrossi@example.it", "5", NewClose{Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if *update != nil {
		t.Fatal("update inviato per un ticket già chiuso")
	}
}

func TestCloseNotYours(t *testing.T) {
	c, update := otrsReply(t, "open")
	if err := c.Close(context.Background(), "mrossi@example.it", "6", NewClose{}); !errors.Is(err, ErrNotYours) {
		t.Fatalf("ticket altrui: %v", err)
	}
	if *update != nil {
		t.Fatal("update inviato per un ticket altrui")
	}
}

func TestMockClose(t *testing.T) {
	m := NewMock()
	m.Tickets["5"] = &Ticket{Summary: Summary{TicketID: "5", StateType: "open"}, CustomerUserID: "mrossi@example.it"}
	if err := m.Close(context.Background(), "altro@example.it", "5", NewClose{}); !errors.Is(err, ErrNotYours) {
		t.Fatalf("mock altrui: %v", err)
	}
	if err := m.Close(context.Background(), "mrossi@example.it", "5", NewClose{Body: "ok"}); err != nil {
		t.Fatal(err)
	}
	tk := m.Tickets["5"]
	if !tk.Closed || tk.StateType != "closed" || len(tk.Articles) != 1 || len(m.Closes) != 1 {
		t.Fatalf("mock: %+v %+v", tk, m.Closes)
	}
}

func TestCloseDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			io.WriteString(w, ticketJSON("5", "mrossi@example.it", "Coda prova", "open"))
			return
		}
		io.WriteString(w, `{"Error":{"ErrorCode":"TicketUpdate.AccessDenied","ErrorMessage":"TicketUpdate: Does not have permissions to update state!"}}`)
	}))
	t.Cleanup(srv.Close)
	c := &HTTPClient{Config: config.OTRS{URL: srv.URL + "/ws", RouteGet: "/Ticket/:TicketID", RouteUpdate: "/TicketUpdate",
		User: "agente", Password: "segreta", Queue: "Coda prova"}, HTTP: srv.Client(), Loc: rome}
	err := c.Close(context.Background(), "mrossi@example.it", "5", NewClose{})
	if !errors.Is(err, ErrDenied) || !errors.Is(err, ErrOTRS) {
		t.Fatalf("permesso negato: %v", err)
	}
}
