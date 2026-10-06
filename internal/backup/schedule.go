package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Prune applica la retention GFS ai soli backup automatici (manuali e
// pre-ripristino restano finché un admin non li elimina):
// ≤7 giorni tutti, ≤35 uno per settimana ISO, ≤365 uno per mese, oltre eliminati.
func (s *Service) Prune(now time.Time) ([]string, error) {
	list, err := s.List() // più recenti prima: in ogni bucket resta il primo visto
	if err != nil {
		return nil, err
	}
	const day = 24 * time.Hour
	seenWeek, seenMonth := map[string]bool{}, map[string]bool{}
	var deleted []string
	for _, b := range list {
		if b.Kind != KindAuto {
			continue
		}
		age := now.Sub(b.CreatedAt)
		var key string
		var seen map[string]bool
		switch {
		case age <= 7*day:
			continue
		case age <= 35*day:
			y, w := b.CreatedAt.ISOWeek()
			key, seen = fmt.Sprintf("%d-W%02d", y, w), seenWeek
		case age <= 365*day:
			key, seen = b.CreatedAt.Format("2006-01"), seenMonth
		}
		if seen != nil && !seen[key] {
			seen[key] = true
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, b.Name)); err != nil {
			return deleted, err
		}
		deleted = append(deleted, b.Name)
	}
	return deleted, nil
}

// Scheduler crea un backup automatico quando l'ultimo è più vecchio di interval
// (controllo subito e poi ogni minuto). interval <= 0 disattiva.
func (s *Service) Scheduler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	s.tick(interval)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(interval)
		}
	}
}

func (s *Service) tick(interval time.Duration) {
	list, err := s.List()
	if err != nil {
		slog.Error("backup: elenco", "err", err)
		return
	}
	for _, b := range list {
		if b.Kind == KindAuto {
			if s.o.Now().Sub(b.CreatedAt) < interval {
				return
			}
			break
		}
	}
	info, err := s.Create(KindAuto)
	if err != nil {
		if !errors.Is(err, ErrBusy) {
			slog.Error("backup automatico non riuscito", "err", err)
		}
		return
	}
	slog.Info("backup automatico creato", "file", info.Name, "bytes", info.Size)
	if deleted, err := s.Prune(s.o.Now()); err != nil {
		slog.Error("backup: conservazione", "err", err)
	} else if len(deleted) > 0 {
		slog.Info("backup: eliminati dalla conservazione", "file", deleted)
	}
}
