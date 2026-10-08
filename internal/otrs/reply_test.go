package otrs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

func otrsReply(t *testing.T, stateType string) (*HTTPClient, *map[string]any) {
	t.Helper()
	var update map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/ws/Ticket/"):
			id := strings.TrimPrefix(r.URL.Path, "/ws/Ticket/")
			owner := "mrossi@example.it"
			if id == "6" {
				owner = "altro@example.it"
			}
			io.WriteString(w, ticketJSON(id, owner, "Coda prova", stateType))
		case r.Method == "PATCH" && r.URL.Path == "/ws/TicketUpdate":
			update = body
			io.WriteString(w, `{"TicketID":"5","ArticleID":"200"}`)
		default:
			http.Error(w, "x", 500)
		}
	}))
	t.Cleanup(srv.Close)
	return &HTTPClient{Config: config.OTRS{URL: srv.URL + "/ws", RouteGet: "/Ticket/:TicketID", RouteUpdate: "/TicketUpdate",
		User: "agente", Password: "segreta", Queue: "Coda prova"}, HTTP: srv.Client(), Loc: rome}, &update
}

func TestReplyOpenTicket(t *testing.T) {
	c, update := otrsReply(t, "open")
	err := c.Reply(context.Background(), "mrossi@example.it", "5", NewReply{Name: "Mario Rossi", Email: "mrossi@example.it",
		Body: "Ancora non va.", Attachments: []Attachment{{Filename: "a.png", ContentType: "image/png", Content: []byte("x")}}})
	if err != nil {
		t.Fatal(err)
	}
	u := *update
	art := u["Article"].(map[string]any)
	if u["TicketID"] != "5" || art["Body"] != "Ancora non va." || art["SenderType"] != "customer" || art["ArticleType"] != "webrequest" ||
		art["From"] != `"Mario Rossi" <mrossi@example.it>` || len(u["Attachment"].([]any)) != 1 {
		t.Fatalf("update: %v", u)
	}
	if _, reopened := u["Ticket"]; reopened {
		t.Fatal("ticket aperto: nessun cambio di stato")
	}
}

func TestReplyReopensClosed(t *testing.T) {
	c, update := otrsReply(t, "closed")
	if err := c.Reply(context.Background(), "mrossi@example.it", "5", NewReply{Name: "M", Email: "mrossi@example.it", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if (*update)["Ticket"].(map[string]any)["State"] != "open" {
		t.Fatalf("riapertura: %v", *update)
	}
}

func TestReplyNotYours(t *testing.T) {
	c, update := otrsReply(t, "open")
	if err := c.Reply(context.Background(), "mrossi@example.it", "6", NewReply{Body: "x"}); !errors.Is(err, ErrNotYours) {
		t.Fatalf("ticket altrui: %v", err)
	}
	if *update != nil {
		t.Fatal("update inviato per un ticket altrui")
	}
}

func TestMockReply(t *testing.T) {
	m := NewMock()
	m.Tickets["5"] = &Ticket{Summary: Summary{TicketID: "5", StateType: "closed", Closed: true}, CustomerUserID: "mrossi@example.it"}
	if err := m.Reply(context.Background(), "mrossi@example.it", "5", NewReply{Body: "x"}); err != nil || len(m.Replies) != 1 || !m.Replies[0].Reopened {
		t.Fatalf("mock: %v %+v", err, m.Replies)
	}
	if m.Tickets["5"].Closed || len(m.Tickets["5"].Articles) != 1 {
		t.Fatalf("mock: ticket non aggiornato %+v", m.Tickets["5"])
	}
}
