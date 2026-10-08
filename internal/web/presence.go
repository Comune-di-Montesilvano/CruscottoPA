package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
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
	a, err := s.db.GetAlert(alertID)
	if err != nil {
		return
	}
	now := s.now()
	if a.StartsAt.After(now) || (a.EndsAt != nil && !now.Before(*a.EndsAt)) {
		return // programmato o scaduto: non è in plancia
	}
	if done, err := s.db.HasRead(alertID, username); err != nil || done {
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

// presenceCleanupEvery: controllo degli utenti non più attivi nel dominio.
const presenceCleanupEvery = 24 * time.Hour

// CleanupPresence toglie dalla presenza gli utenti che AD non conosce più
// (disattivati o cancellati). Con AD non raggiungibile non cancella nulla.
func (s *Server) CleanupPresence() {
	if s.directory == nil {
		return
	}
	ps, err := s.db.ListPresence()
	if err != nil {
		slog.Warn("pulizia presenza", "err", err)
		return
	}
	for _, p := range ps {
		_, err := s.directory.Lookup(p.Username)
		switch {
		case errors.Is(err, identity.ErrUnknownUser):
			if err := s.db.DeletePresence(p.Username); err != nil {
				slog.Warn("pulizia presenza", "err", err)
			}
		case err != nil:
			return // AD non raggiungibile: si riprova al prossimo giro
		}
	}
}

// StartPresenceCleanup: pulizia all'avvio e poi una volta al giorno.
func (s *Server) StartPresenceCleanup(ctx context.Context) {
	go func() {
		t := time.NewTicker(presenceCleanupEvery)
		defer t.Stop()
		for {
			s.CleanupPresence()
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
