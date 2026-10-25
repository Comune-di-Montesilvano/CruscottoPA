package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
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

// markViewerReads: alla plancia gli avvisi brevi non urgenti (letti per intero
// nel carosello) destinati a chi guarda risultano letti; poi segna lo stato di
// lettura di tutti. Gli urgenti si leggono solo con «Ho letto».
func (s *Server) markViewerReads(username string, alerts []database.Alert) []database.Alert {
	if username == "" {
		return alerts
	}
	read, err := s.db.ReadAlertIDs(username)
	if err != nil {
		slog.Warn("letture", "err", err)
		return alerts
	}
	var short []int64
	for _, a := range alerts {
		if !read[a.ID] && a.Level != database.LevelUrgent && !a.NotForViewer && !needsMore(a.Body) {
			short = append(short, a.ID)
		}
	}
	// Solo il giorno (mezzanotte di Roma): l'ora della lettura in plancia
	// sarebbe l'ora di arrivo del dipendente.
	now := s.now().In(s.loc())
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc())
	if err := s.db.MarkReadMany(short, username, database.ReadPlancia, day); err != nil {
		slog.Warn("lettura avvisi in plancia", "err", err)
	} else {
		for _, id := range short {
			read[id] = true
		}
	}
	return setReadState(alerts, read)
}

// withReadState segna gli avvisi già letti da username (nessuna scrittura).
func (s *Server) withReadState(username string, alerts []database.Alert) []database.Alert {
	if username == "" {
		return alerts
	}
	read, err := s.db.ReadAlertIDs(username)
	if err != nil {
		slog.Warn("letture", "err", err)
		return alerts
	}
	return setReadState(alerts, read)
}

// setReadState: CanRead = utente riconosciuto (il server registra il suo «Ho letto»).
func setReadState(alerts []database.Alert, read map[int64]bool) []database.Alert {
	for i := range alerts {
		alerts[i].CanRead = true
		alerts[i].ReadByViewer = read[alerts[i].ID]
	}
	return alerts
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
			if err := s.db.DeleteUserDays(p.Username); err != nil {
				slog.Warn("pulizia presenza", "err", err)
			}
			if err := s.db.DeleteUserLayout(p.Username); err != nil {
				slog.Warn("pulizia presenza", "err", err)
			}
		case err != nil:
			return // AD non raggiungibile: si riprova al prossimo giro
		}
	}
}

// clicksRetention: i clic sulle tile servono solo per l'ultima settimana.
const clicksRetention = 90 * 24 * time.Hour

// cleanupStats: giorni di accesso e dati dei ticket oltre STATS_RETENTION_MONTHS,
// clic oltre 90 giorni. Il contatore dei traguardi resta.
func (s *Server) cleanupStats() {
	now := s.now()
	days := s.dayOf(now.AddDate(0, -s.cfg.StatsRetentionMonths, 0))
	if err := s.db.CleanupStats(days, s.dayOf(now.Add(-clicksRetention))); err != nil {
		slog.Warn("pulizia statistiche", "err", err)
	}
}

// StartPresenceCleanup: pulizia all'avvio e poi una volta al giorno.
func (s *Server) StartPresenceCleanup(ctx context.Context) {
	go func() {
		t := time.NewTicker(presenceCleanupEvery)
		defer t.Stop()
		for {
			s.CleanupPresence()
			if err := s.db.CleanupTicketState(s.now()); err != nil {
				slog.Warn("pulizia stato ticket", "err", err)
			}
			s.cleanupStats()
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
