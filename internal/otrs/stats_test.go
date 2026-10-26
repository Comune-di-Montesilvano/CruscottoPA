package otrs

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestTicketIDsSince(t *testing.T) {
	c, searches := otrsRead(t, func(body map[string]any) string {
		if body["StateType"] != nil {
			return `{"TicketID":["7"]}`
		}
		return `{"TicketID":["5","6"]}`
	}, nil)
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, rome)
	created, closed, err := c.TicketIDsSince(context.Background(), from)
	if err != nil || !reflect.DeepEqual(created, []string{"5", "6"}) || !reflect.DeepEqual(closed, []string{"7"}) {
		t.Fatalf("id: %v %v %v", created, closed, err)
	}
	s := *searches
	if len(s) != 2 || s[0]["TicketCreateTimeNewerDate"] != "2026-10-01 00:00:00" || s[0]["Limit"] != float64(10000) {
		t.Fatalf("ricerca creati: %v", s)
	}
	if q, _ := s[0]["Queues"].([]any); len(q) != 1 || q[0] != "Coda prova" {
		t.Fatalf("coda: %v", s[0]["Queues"])
	}
	if st, _ := s[1]["StateType"].([]any); len(st) != 1 || st[0] != "closed" || s[1]["TicketCloseTimeNewerDate"] != "2026-10-01 00:00:00" || s[1]["Limit"] != float64(10000) {
		t.Fatalf("ricerca chiusi: %v", s[1])
	}
}

func statsTicket(queue, articles string) string {
	return `{"Ticket":[{"TicketID":"5","TicketNumber":"1","Title":"x","State":"open","StateType":"open","Queue":"` + queue + `",
"CustomerUserID":"mrossi@example.it","Created":"2026-10-08 10:00:00","Changed":"2026-10-08 12:00:00","Article":[` + articles + `]}]}`
}

func TestFirstReplySkipsOpeningArticle(t *testing.T) {
	c, _ := otrsRead(t, nil, map[string]string{"5": statsTicket("Coda prova", `
{"ArticleID":"1","ArticleType":"phone","SenderType":"agent","From":"Op","To":"mrossi@example.it","Body":"aperto al telefono","Created":"2026-10-08 10:00:00"},
{"ArticleID":"2","ArticleType":"webrequest","SenderType":"customer","From":"Mario","Body":"grazie","Created":"2026-10-08 10:30:00"},
{"ArticleID":"3","ArticleType":"email-external","SenderType":"agent","From":"Op","To":"mrossi@example.it","Body":"fatto","Created":"2026-10-08 12:00:00"}`)})
	created, reply, err := c.FirstReply(context.Background(), "5")
	if err != nil || !created.Equal(time.Date(2026, 10, 8, 10, 0, 0, 0, rome)) {
		t.Fatalf("creato: %v %v", created, err)
	}
	if reply == nil || !reply.Equal(time.Date(2026, 10, 8, 12, 0, 0, 0, rome)) {
		t.Fatalf("risposta: %v", reply)
	}
}

func TestFirstReplyIgnoresInternalAndThirdParty(t *testing.T) {
	c, _ := otrsRead(t, nil, map[string]string{"5": statsTicket("Coda prova", `
{"ArticleID":"1","ArticleType":"webrequest","SenderType":"customer","From":"Mario","Body":"aiuto","Created":"2026-10-08 10:00:00"},
{"ArticleID":"2","ArticleType":"note-internal","SenderType":"agent","From":"Op","Body":"interna","Created":"2026-10-08 10:10:00"},
{"ArticleID":"3","ArticleType":"email-external","SenderType":"agent","From":"Op","To":"fornitore@example.it","Body":"al fornitore","Created":"2026-10-08 10:20:00"}`)})
	_, reply, err := c.FirstReply(context.Background(), "5")
	if err != nil || reply != nil {
		t.Fatalf("nessuna risposta valida: %v %v", reply, err)
	}
}

func TestFirstReplyOtherQueue(t *testing.T) {
	c, _ := otrsRead(t, nil, map[string]string{"5": statsTicket("Altra", `{"ArticleID":"1","ArticleType":"webrequest","SenderType":"customer","Body":"x","Created":"2026-10-08 10:00:00"}`)})
	if _, _, err := c.FirstReply(context.Background(), "5"); !errors.Is(err, ErrNotYours) {
		t.Fatalf("altra coda: %v", err)
	}
}

func TestMockStats(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	at := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }
	m := NewMock()
	m.Tickets["1"] = &Ticket{Summary: Summary{TicketID: "1", Created: at(2, 8), Changed: at(3, 8), Closed: true},
		Articles: []Article{{FromAgent: true, Created: at(2, 8)}, {Created: at(2, 9)}, {FromAgent: true, ToThirdParty: true, Created: at(2, 10)}, {FromAgent: true, Created: at(2, 11)}}}
	m.Tickets["2"] = &Ticket{Summary: Summary{TicketID: "2", Created: time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC), Changed: at(4, 8), Closed: true}}
	m.Tickets["3"] = &Ticket{Summary: Summary{TicketID: "3", Created: time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC), Changed: at(4, 8)}}
	created, closed, _ := m.TicketIDsSince(context.Background(), from)
	if !reflect.DeepEqual(created, []string{"1"}) || !reflect.DeepEqual(closed, []string{"1", "2"}) {
		t.Fatalf("mock id: %v %v", created, closed)
	}
	c, reply, err := m.FirstReply(context.Background(), "1")
	if err != nil || !c.Equal(at(2, 8)) || reply == nil || !reply.Equal(at(2, 11)) {
		t.Fatalf("mock risposta: %v %v %v", c, reply, err)
	}
	if _, _, err := m.FirstReply(context.Background(), "9"); !errors.Is(err, ErrNotYours) {
		t.Fatalf("mock inesistente: %v", err)
	}
}
