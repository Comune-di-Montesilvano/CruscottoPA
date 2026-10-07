package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/calendar"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

type todayView struct {
	Day      int
	Weekday  string
	Week     int
	YearDay  int
	YearDays int
}

type calendarWidget struct {
	Month    calendar.MonthView
	Upcoming []calendar.Occurrence
}

type dashboardView struct {
	database.Dashboard
	Greeting  string
	DateLabel string
	Clock     string
	Version   string
	Today     todayView
	Calendar  calendarWidget
	User      identity.User // identità dichiarata: solo per il saluto
	Recognize bool          // senza cookie e con riconoscimento attivo: dashboard.js chiama /io
	Filter    *contentFilter
	Admin     bool // link al pannello admin nel footer
}

type avvisiView struct {
	Alerts  []database.Alert
	Version string
	Admin   bool
}

func (s *Server) calendarWidgetFor(year int, month time.Month) (calendarWidget, error) {
	evs, err := s.db.ListCalendarEvents()
	if err != nil {
		return calendarWidget{}, err
	}
	ce := toCalendarEvents(evs)
	today := s.today()
	return calendarWidget{
		Month:    calendar.Month(year, month, ce, today),
		Upcoming: calendar.Upcoming(today, ce, 5),
	}, nil
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	d, err := s.db.GetDashboard(s.now())
	if err != nil {
		s.serverError(w, err)
		return
	}
	f, err := s.contentFilterFor(r)
	if err != nil {
		s.serverError(w, err)
		return
	}
	f.dashboard(&d)
	now := s.now().In(s.loc())
	cw, err := s.calendarWidgetFor(now.Year(), now.Month())
	if err != nil {
		s.serverError(w, err)
		return
	}
	u, known := s.viewer(r)
	s.render(w, http.StatusOK, "dashboard.html", dashboardView{
		Dashboard: d,
		Greeting:  greeting(now.Hour()),
		DateLabel: italianDate(now),
		Clock:     now.Format("15:04"),
		Version:   s.version,
		Today:     todayInfo(now),
		Calendar:  cw,
		User:      u,
		Recognize: !known && s.canRecognize(r),
		Filter:    f,
		Admin:     s.viewerIsAdmin(r),
	})
}

func (s *Server) handleAlertsPartial(w http.ResponseWriter, r *http.Request) {
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
	s.render(w, http.StatusOK, "alerts_carousel", f.alertList(alerts))
}

// handleCalendarPartial: mese non valido → mese corrente (mai un errore).
func (s *Server) handleCalendarPartial(w http.ResponseWriter, r *http.Request) {
	y, m := calendar.ParseMonth(r.URL.Query().Get("mese"), s.today())
	cw, err := s.calendarWidgetFor(y, m)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, http.StatusOK, "widget_calendario", cw)
}

func (s *Server) handleAvvisi(w http.ResponseWriter, r *http.Request) {
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
	s.render(w, http.StatusOK, "avvisi.html", avvisiView{Alerts: f.alertList(alerts), Version: s.version, Admin: s.viewerIsAdmin(r)})
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

func todayInfo(t time.Time) todayView {
	_, week := t.ISOWeek()
	days := 365
	if y := t.Year(); y%4 == 0 && (y%100 != 0 || y%400 == 0) {
		days = 366
	}
	return todayView{Day: t.Day(), Weekday: weekdays[t.Weekday()], Week: week, YearDay: t.YearDay(), YearDays: days}
}
