package web

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

// Statistiche dell'assistenza per il widget degli ultimi 30 giorni: un giro ogni ora,
// risultato nel DB (la plancia non chiama OTRS per questi numeri).
const (
	ticketStatsEvery = time.Hour
	ticketStatsReads = 300 // letture di OTRS al massimo per giro
	statsWindowDays  = 30  // periodo del widget e delle statistiche, oggi compreso
)

// StartTicketStats: subito e poi ogni ora, solo con OTRS e STATS_ENABLED.
func (s *Server) StartTicketStats(ctx context.Context) {
	if !s.ticketsEnabled() || !s.cfg.StatsEnabled {
		return
	}
	go func() {
		t := time.NewTicker(ticketStatsEvery)
		defer t.Stop()
		for {
			if err := s.ticketStatsOnce(ctx); err != nil {
				slog.Warn("statistiche assistenza", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// windowStart: inizio degli ultimi 30 giorni (oggi compreso), a mezzanotte
// nel fuso dell'ente.
func (s *Server) windowStart(t time.Time) time.Time {
	l := t.In(s.loc())
	return time.Date(l.Year(), l.Month(), l.Day()-(statsWindowDays-1), 0, 0, 0, 0, s.loc())
}

// ticketStatsOnce aggiorna le statistiche degli ultimi 30 giorni. Un ticket
// illeggibile non blocca gli altri: si restituisce il primo errore.
func (s *Server) ticketStatsOnce(ctx context.Context) error {
	now := s.now()
	from := s.windowStart(now)
	createdIDs, closedIDs, err := s.tickets.TicketIDsSince(ctx, from)
	if err != nil {
		return err
	}
	var firstErr error
	budget := ticketStatsReads
	seen := map[string]bool{}
	read := func(id string) {
		budget--
		seen[id] = true
		created, reply, err := s.tickets.FirstReply(ctx, id)
		if errors.Is(err, otrs.ErrNotYours) {
			return
		}
		if err == nil {
			if err = s.db.UpsertTicketCreated(id, created); err == nil && reply != nil {
				err = s.db.SetFirstReply(id, *reply)
			}
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	missing, err := s.db.MissingTicketFacts(createdIDs)
	if err != nil {
		return err
	}
	for _, id := range missing {
		if budget == 0 {
			break
		}
		read(id)
	}
	if budget > 0 {
		pending, err := s.db.TicketsWithoutReply(from, budget+len(seen))
		if err != nil {
			return err
		}
		for _, f := range pending {
			if budget == 0 {
				break
			}
			if !seen[f.TicketID] {
				read(f.TicketID)
			}
		}
	}
	if err := s.db.SetClosed(closedIDs); err != nil {
		return err
	}
	ds, err := s.db.FirstReplyDurations(from, now.Add(time.Second))
	if err != nil {
		return err
	}
	st := database.TicketStats{Requests: len(createdIDs), Resolved: len(closedIDs), MedianFirst: median(ds), UpdatedAt: now}
	if err := s.db.SaveTicketStats(st); err != nil {
		return err
	}
	return firstErr
}

// median: con un numero pari di valori, la media dei due centrali (al secondo).
func median(ds []time.Duration) *time.Duration {
	if len(ds) == 0 {
		return nil
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	m := ds[len(ds)/2]
	if len(ds)%2 == 0 {
		m = (ds[len(ds)/2-1] + m) / 2
	}
	m = m.Truncate(time.Second)
	return &m
}
