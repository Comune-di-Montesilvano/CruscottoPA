package web

import (
	"net/http"
	"strconv"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type avvisoView struct {
	Alert   *database.Alert
	Version string
	Admin   bool
}

// handleAvviso: avviso attivo e visibile (stesse regole di /avvisi). Altrimenti
// "non disponibile" con 200: il proxy riscriverebbe un 404.
func (s *Server) handleAvviso(w http.ResponseWriter, r *http.Request) {
	view := avvisoView{Version: s.version, Admin: s.viewerIsAdmin(r)}
	if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil {
		alerts, err := s.db.ListActiveAlerts(s.now())
		if err != nil {
			s.serverError(w, err)
			return
		}
		f, err := s.contentFilterFor(r)
		if err != nil {
			s.serverError(w, err)
			return
		}
		for _, a := range f.alertList(alerts) {
			if a.ID == id {
				view.Alert = &a
				break
			}
		}
	}
	s.render(w, http.StatusOK, "avviso.html", view)
}
