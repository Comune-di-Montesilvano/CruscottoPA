package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type dashboardView struct {
	database.Dashboard
	Greeting  string
	DateLabel string
	Clock     string
	Version   string
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	d, err := s.db.GetDashboard(s.now())
	if err != nil {
		s.serverError(w, err)
		return
	}
	now := s.now().In(s.loc())
	s.render(w, http.StatusOK, "dashboard.html", dashboardView{
		Dashboard: d,
		Greeting:  greeting(now.Hour()),
		DateLabel: italianDate(now),
		Clock:     now.Format("15:04"),
		Version:   s.version,
	})
}

func (s *Server) handleAlertsPartial(w http.ResponseWriter, r *http.Request) {
	alerts, err := s.db.ListActiveAlerts(s.now())
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, http.StatusOK, "alerts_strip", alerts)
}

// greeting: stesse soglie di dashboard.js (che lo aggiorna lato client).
func greeting(hour int) string {
	switch {
	case hour >= 6 && hour < 13:
		return "Buongiorno"
	case hour >= 13 && hour < 18:
		return "Buon pomeriggio"
	default:
		return "Buonasera"
	}
}

var (
	weekdays = [...]string{"domenica", "lunedì", "martedì", "mercoledì", "giovedì", "venerdì", "sabato"}
	months   = [...]string{"gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno",
		"luglio", "agosto", "settembre", "ottobre", "novembre", "dicembre"}
)

func italianDate(t time.Time) string {
	return fmt.Sprintf("%s %d %s %d", weekdays[t.Weekday()], t.Day(), months[t.Month()-1], t.Year())
}
