package web

import (
	"log/slog"
	"net/http"
	"strconv"
)

// Presenza e letture dalla plancia. Rispondono sempre 200: il reverse proxy
// sostituirebbe i 4xx. Contano solo gli utenti riconosciuti (identità
// dichiarata: dati indicativi, non una prova).

// viewerName: utente riconosciuto dal cookie; "" se assente o anonimo.
func (s *Server) viewerName(r *http.Request) (username, name string) {
	u, ok := s.viewer(r)
	if !ok || u.Anonymous || u.Username == "" {
		return "", ""
	}
	return u.Username, u.Name
}

func (s *Server) handlePresence(w http.ResponseWriter, r *http.Request) {
	if user, _ := s.viewerName(r); user != "" {
		if err := s.db.SetPresenceClient(user, r.FormValue("app") == "1", r.FormValue("permesso"), s.now()); err != nil {
			slog.Warn("presenza", "err", err)
		}
	}
	w.WriteHeader(http.StatusOK)
}

// markRead registra la lettura se l'avviso esiste ed è visibile all'utente.
func (s *Server) markRead(username string, alertID int64, how string) {
	if _, err := s.db.GetAlert(alertID); err != nil {
		return
	}
	if visible, _ := s.alertVisibleTo(username, alertID); !visible {
		return
	}
	if err := s.db.MarkRead(alertID, username, how, s.now()); err != nil {
		slog.Warn("lettura avviso", "err", err)
	}
}

func (s *Server) handleAlertRead(w http.ResponseWriter, r *http.Request) {
	if user, _ := s.viewerName(r); user != "" {
		if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil {
			s.markRead(user, id, r.FormValue("come"))
		}
	}
	w.WriteHeader(http.StatusOK)
}
