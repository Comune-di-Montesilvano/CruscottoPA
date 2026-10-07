package web

import (
	"context"
	"log/slog"

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
			return s.db.SetGuideFetched(g.ID, body, s.now())
		}
	}
	slog.Warn("guida GitHub non aggiornata", "guida", g.ID, "err", err)
	if serr := s.db.SetGuideFetchError(g.ID, err.Error()); serr != nil {
		return serr
	}
	return err
}
