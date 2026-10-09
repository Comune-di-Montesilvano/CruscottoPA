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

// ticketWatcher: since = inizio del giro precedente completo (meno la
// tolleranza); floor = primo giro tentato. Per un ticket senza stato si
// notificano solo le risposte scritte dopo max(since, floor): mai quelle
// vecchie (ticket fuori dal primo giro, stato pulito dopo 30 giorni, avvio).
type ticketWatcher struct {
	since time.Time
	floor time.Time
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

// ticketWatchOnce: un giro. Ogni chiamata a OTRS ha il suo timeout (15 s);
// un ticket illeggibile non ferma gli altri ma il giro non conta come
// completo: since non avanza e il giro dopo rilegge lo stesso intervallo
// (lo stato per ticket evita i doppioni).
func (s *Server) ticketWatchOnce(ctx context.Context, w *ticketWatcher) error {
	start := s.now()
	if w.floor.IsZero() {
		w.floor = start
	}
	since := w.since
	if since.IsZero() {
		since = start.AddDate(0, 0, -7)
	}
	// OTRS ragiona in ora locale senza fuso: se fra since e adesso cambia
	// l'ora legale, un'ora si ripete e va riletta (lo stato evita i doppioni).
	if _, a := since.In(s.loc()).Zone(); true {
		if _, b := start.In(s.loc()).Zone(); a != b {
			since = since.Add(-time.Hour)
		}
	}
	threshold := since
	if threshold.Before(w.floor) {
		threshold = w.floor
	}
	tickets, changedErr := s.tickets.Changed(ctx, since)
	if changedErr != nil && len(tickets) == 0 {
		return changedErr
	}
	for _, t := range tickets {
		s.ticketCache.forget(t.CustomerUserID) // il widget rilegge il ticket cambiato
		last, known, err := s.db.TicketNotifyState(t.TicketID)
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
			if !a.FromAgent || a.ToThirdParty || n <= last {
				continue
			}
			if !known && a.Created.Before(threshold) {
				continue // risposta vecchia di un ticket mai visto dal watcher
			}
			fresh = append(fresh, a)
		}
		if newest <= last {
			continue
		}
		// Prima lo stato, poi l'invio: una risposta si notifica una volta sola.
		if err := s.db.SetTicketNotifyState(t.TicketID, newest, start); err != nil {
			return err
		}
		if len(fresh) == 0 {
			continue
		}
		user, err := s.db.TicketUserFor(t.CustomerUserID)
		if err != nil || user == "" {
			continue
		}
		s.notifyTicketReply(ctx, user, t, fresh[len(fresh)-1])
	}
	if changedErr != nil {
		return changedErr
	}
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
