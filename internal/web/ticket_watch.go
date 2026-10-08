package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/markdown"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/notify"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

const (
	ticketWatchEvery = 2 * time.Minute
	ticketWatchSlack = time.Minute // tolleranza sugli orologi
)

type ticketWatcher struct {
	since  time.Time
	primed bool
}

// StartTicketWatch: ogni 2 minuti cerca risposte degli operatori e le notifica.
func (s *Server) StartTicketWatch(ctx context.Context) {
	if !s.ticketsEnabled() {
		return
	}
	go func() {
		w := &ticketWatcher{}
		t := time.NewTicker(ticketWatchEvery)
		defer t.Stop()
		for {
			if err := s.ticketWatchOnce(ctx, w); err != nil {
				slog.Warn("ticket: controllo risposte", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func articleNum(id string) int64 {
	n, _ := strconv.ParseInt(id, 10, 64)
	return n
}

// ticketWatchOnce: un giro. Il primo registra lo stato senza notificare.
// Con OTRS giù since non avanza: il giro dopo rilegge lo stesso intervallo.
func (s *Server) ticketWatchOnce(ctx context.Context, w *ticketWatcher) error {
	start := s.now()
	since := w.since
	if !w.primed {
		since = start.AddDate(0, 0, -7)
	}
	cctx, cancel := context.WithTimeout(ctx, 4*otrs.ReadTimeout)
	defer cancel()
	tickets, err := s.tickets.Changed(cctx, since)
	if err != nil {
		return err
	}
	for _, t := range tickets {
		last, _, err := s.db.TicketNotifyState(t.TicketID)
		if err != nil {
			return err
		}
		var newest int64
		var fresh []otrs.Article
		for _, a := range t.Articles {
			n := articleNum(a.ArticleID)
			if n > newest {
				newest = n
			}
			if a.FromAgent && n > last {
				fresh = append(fresh, a)
			}
		}
		if newest <= last {
			continue
		}
		// Prima lo stato, poi l'invio: una risposta si notifica una volta sola.
		if err := s.db.SetTicketNotifyState(t.TicketID, newest, start); err != nil {
			return err
		}
		if !w.primed || len(fresh) == 0 {
			continue
		}
		user, err := s.db.TicketUserFor(t.CustomerUserID)
		if err != nil || user == "" {
			continue
		}
		s.ticketCache.forget(t.CustomerUserID)
		s.notifyTicketReply(ctx, user, t, fresh[len(fresh)-1])
	}
	w.primed = true
	w.since = start.Add(-ticketWatchSlack)
	return nil
}

func (s *Server) notifyTicketReply(ctx context.Context, user string, t otrs.Ticket, a otrs.Article) {
	id, _ := strconv.ParseInt(t.TicketID, 10, 64)
	ev := notify.Event{ID: id, Kind: "ticket", Title: "Risposta al ticket " + t.TicketNumber,
		Body: markdown.Plain(a.Body, 120), URL: "/ticket/" + t.TicketID}
	s.hub.Broadcast(ev, func(u string) bool { return u == user })
	if s.pusher == nil {
		return
	}
	subs, err := s.db.ListPushSubscriptionsFor(user)
	if err != nil {
		slog.Warn("ticket: iscrizioni push", "err", err)
		return
	}
	payload, _ := json.Marshal(map[string]string{"title": ev.Title, "body": ev.Body, "url": ev.URL, "tag": "ticket-" + t.TicketID})
	for _, sub := range subs {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		gone, err := s.pusher.Send(pctx, sub, payload, false)
		cancel()
		switch {
		case gone:
			s.db.DeletePushSubscription(sub.Endpoint)
		case err != nil:
			slog.Warn("ticket: invio push", "err", err)
		}
	}
}
