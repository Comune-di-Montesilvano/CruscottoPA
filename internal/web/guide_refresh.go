package web

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/guidesrc"
)

// refreshGuide scarica la guida GitHub; se fallisce resta l'ultima copia buona
// e l'errore si vede nell'admin.
func (s *Server) refreshGuide(ctx context.Context, g database.Guide) error {
	src, err := guidesrc.ParseGitHubURL(g.SourceURL)
	if err == nil {
		var body string
		if body, err = s.fetchGuide(ctx, src.Raw); err == nil {
			return ignoreChanged(s.db.SetGuideFetched(g.ID, g.SourceURL, body, s.now()))
		}
	}
	slog.Warn("guida GitHub non aggiornata", "guida", g.ID, "err", err)
	if serr := ignoreChanged(s.db.SetGuideFetchError(g.ID, g.SourceURL, err.Error())); serr != nil {
		return serr
	}
	return err
}

// RefreshGuides aggiorna le guide GitHub più vecchie dell'intervallo, una
// alla volta. Una guida in errore si ritenta al giro successivo (10 minuti).
func (s *Server) RefreshGuides(ctx context.Context) {
	if s.cfg.GuideRefreshHours <= 0 {
		return
	}
	todo, err := s.db.GuidesToRefresh(s.now().Add(-time.Duration(s.cfg.GuideRefreshHours) * time.Hour))
	if err != nil {
		slog.Error("guide GitHub: elenco da aggiornare", "err", err)
		return
	}
	for _, g := range todo {
		if ctx.Err() != nil {
			return
		}
		s.refreshGuide(ctx, g) // l'errore resta sulla guida
	}
}

// StartGuideRefresh avvia l'aggiornamento periodico (niente se GUIDE_REFRESH_HOURS=0).
func (s *Server) StartGuideRefresh(ctx context.Context) {
	if s.cfg.GuideRefreshHours <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			s.RefreshGuides(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// ignoreChanged: la guida è stata modificata (o eliminata) durante il
// download, il risultato non serve più.
func ignoreChanged(err error) error {
	if errors.Is(err, database.ErrNotFound) {
		return nil
	}
	return err
}
