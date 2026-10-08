package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

// ticketCacheTTL: più lungo del refresh del widget. La freschezza la danno
// forget dopo apertura e risposta e il watcher su ogni ticket cambiato.
const ticketCacheTTL = 5 * time.Minute

type cachedMine struct {
	list    []otrs.Summary
	expires time.Time
}

type cachedTicket struct {
	t       otrs.Ticket
	expires time.Time
}

// ticketCache: letture da OTRS in memoria per 60 s. Gli errori non si
// tengono: al prossimo caricamento si riprova.
type ticketCache struct {
	mu      sync.Mutex
	mineBy  map[string]cachedMine   // email minuscola
	tickets map[string]cachedTicket // email minuscola + "/" + id
	now     func() time.Time
}

func newTicketCache(now func() time.Time) *ticketCache {
	return &ticketCache{mineBy: map[string]cachedMine{}, tickets: map[string]cachedTicket{}, now: now}
}

func (c *ticketCache) mine(email string, load func() ([]otrs.Summary, error)) ([]otrs.Summary, error) {
	key := strings.ToLower(email)
	c.mu.Lock()
	e, ok := c.mineBy[key]
	c.mu.Unlock()
	if ok && c.now().Before(e.expires) {
		return e.list, nil
	}
	list, err := load()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.pruneLocked()
	c.mineBy[key] = cachedMine{list: list, expires: c.now().Add(ticketCacheTTL)}
	c.mu.Unlock()
	return list, nil
}

func (c *ticketCache) ticket(email, id string, load func() (otrs.Ticket, error)) (otrs.Ticket, error) {
	key := strings.ToLower(email) + "/" + id
	c.mu.Lock()
	e, ok := c.tickets[key]
	c.mu.Unlock()
	if ok && c.now().Before(e.expires) {
		return e.t, nil
	}
	t, err := load()
	if err != nil {
		return otrs.Ticket{}, err
	}
	c.mu.Lock()
	c.pruneLocked()
	c.tickets[key] = cachedTicket{t: t, expires: c.now().Add(ticketCacheTTL)}
	c.mu.Unlock()
	return t, nil
}

// pruneLocked (con il lock): via le voci scadute, così la cache non cresce.
func (c *ticketCache) pruneLocked() {
	now := c.now()
	for k, e := range c.mineBy {
		if !now.Before(e.expires) {
			delete(c.mineBy, k)
		}
	}
	for k, e := range c.tickets {
		if !now.Before(e.expires) {
			delete(c.tickets, k)
		}
	}
}

// forget: dopo una risposta (o una notifica) si rilegge da OTRS.
func (c *ticketCache) forget(email string) {
	key := strings.ToLower(email)
	c.mu.Lock()
	delete(c.mineBy, key)
	for k := range c.tickets {
		if strings.HasPrefix(k, key+"/") {
			delete(c.tickets, k)
		}
	}
	c.mu.Unlock()
}

func stateLabel(stateType string) string {
	switch {
	case stateType == "new":
		return "Nuovo"
	case stateType == "open":
		return "In lavorazione"
	case strings.HasPrefix(stateType, "pending"):
		return "In attesa"
	case stateType == "closed":
		return "Chiuso"
	}
	return stateType
}

type ticketRow struct {
	otrs.Summary
	Unread bool
}

type ticketWidget struct {
	Open, Closed []ticketRow
	Down         bool
}

// handleTicketWidget: widget caricato a parte (HTMX). Senza modulo, utente
// anonimo o senza mail: niente (200 vuoto).
func (s *Server) handleTicketWidget(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.ticketsEnabled() {
		return
	}
	req, problem := s.ticketRequester(r)
	if problem != "" {
		return
	}
	user := s.ticketUser(r)
	if err := s.db.UpsertTicketUser(req.Email, user, s.now()); err != nil {
		slog.Warn("ticket: mail → utente", "err", err)
	}
	list, err := s.ticketCache.mine(req.Email, func() ([]otrs.Summary, error) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*otrs.ReadTimeout)
		defer cancel()
		return s.tickets.Mine(ctx, req.Email)
	})
	if err != nil {
		if !errors.Is(err, otrs.ErrOTRS) {
			slog.Warn("ticket: elenco", "err", err)
		}
		s.render(w, http.StatusOK, "widget_ticket", ticketWidget{Down: true})
		return
	}
	seen, err := s.db.TicketSeen(user)
	if err != nil {
		slog.Warn("ticket: visti", "err", err)
	}
	var v ticketWidget
	for _, t := range list {
		row := ticketRow{Summary: t}
		if !t.LastAgentArticle.IsZero() {
			at, ok := seen[t.TicketID]
			row.Unread = !ok || t.LastAgentArticle.After(at)
		}
		if t.Closed {
			v.Closed = append(v.Closed, row)
		} else {
			v.Open = append(v.Open, row)
		}
	}
	s.render(w, http.StatusOK, "widget_ticket", v)
}
