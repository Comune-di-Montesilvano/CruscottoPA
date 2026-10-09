package otrs

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Mock: OTRS_URL=mock (sviluppo) e test. Registra i ticket in memoria.
type Mock struct {
	mu         sync.Mutex
	Sent       []NewTicket
	Err        error              // se impostato, Create fallisce
	FailUpdate bool               // simula il cliente non impostato
	Tickets    map[string]*Ticket // per TicketID; nil = nessuna lettura
	Replies    []MockReply        // risposte ricevute
	demo       map[string]bool    // NewDemoMock: utenti con i ticket di esempio
	next       int                // numerazione dei ticket di esempio
}

// NewMock: OTRS finto per lo sviluppo; un ticket aperto compare nei propri ticket.
func NewMock() *Mock { return &Mock{Tickets: map[string]*Ticket{}} }

func (m *Mock) Create(_ context.Context, t NewTicket) (Created, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return Created{}, m.Err
	}
	m.Sent = append(m.Sent, t)
	n := len(m.Sent)
	c := Created{TicketID: fmt.Sprint(n), TicketNumber: fmt.Sprintf("20261008000000%02d", n), CustomerSet: !m.FailUpdate}
	if m.Tickets != nil {
		now := time.Now()
		m.Tickets[c.TicketID] = &Ticket{Summary: Summary{TicketID: c.TicketID, TicketNumber: c.TicketNumber, Title: t.Subject,
			State: "new", StateType: "new", Created: now, Changed: now}, CustomerUserID: t.Email,
			Articles: []Article{{ArticleID: c.TicketID + "00", From: t.Name, Subject: t.Subject, Body: t.Body, Created: now}}}
	}
	return c, nil
}

func (m *Mock) Mine(_ context.Context, email string) ([]Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	m.seedDemo(email)
	out := []Summary{}
	for _, t := range m.Tickets {
		if strings.EqualFold(t.CustomerUserID, email) {
			out = append(out, t.Summary)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TicketID < out[j].TicketID })
	return out, nil
}

func (m *Mock) Get(_ context.Context, email, id string) (Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return Ticket{}, m.Err
	}
	t, ok := m.Tickets[id]
	if !ok || !strings.EqualFold(t.CustomerUserID, email) {
		return Ticket{}, ErrNotYours
	}
	return *t, nil
}

func (m *Mock) Changed(_ context.Context, since time.Time) ([]Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	out := []Ticket{}
	for _, t := range m.Tickets {
		if !t.Changed.Before(since) {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (m *Mock) Attachment(ctx context.Context, email, id, articleID, fileID string) (Attachment, error) {
	t, err := m.Get(ctx, email, id)
	if err != nil {
		return Attachment{}, err
	}
	for _, a := range t.Articles {
		for _, at := range a.Attachments {
			if a.ArticleID == articleID && at.FileID == fileID {
				return Attachment{Filename: at.Filename, ContentType: at.ContentType, Content: []byte("contenuto di " + at.Filename)}, nil
			}
		}
	}
	return Attachment{}, ErrNotYours
}

type MockReply struct {
	TicketID string
	Reply    NewReply
	Reopened bool
}

func (m *Mock) Reply(_ context.Context, email, id string, r NewReply) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	t, ok := m.Tickets[id]
	if !ok || !strings.EqualFold(t.CustomerUserID, email) {
		return ErrNotYours
	}
	reopened := t.Closed
	if reopened {
		t.Closed, t.State, t.StateType = false, "open", "open"
	}
	now := time.Now()
	t.Changed = now
	t.Articles = append(t.Articles, Article{ArticleID: fmt.Sprintf("%s%02d", id, len(t.Articles)+1), From: r.Name, Body: r.Body, Created: now})
	m.Replies = append(m.Replies, MockReply{TicketID: id, Reply: r, Reopened: reopened})
	return nil
}
