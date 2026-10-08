package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/calendar"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

type todayView struct {
	Day      int
	Weekday  string
	Month    string // "ottobre 2026"
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
	Clock     string
	Version   string
	Today     todayView
	Calendar  calendarWidget
	User      identity.User // identità dichiarata: solo per il saluto
	Office    string        // ufficio e qualifica da AD, sotto il saluto
	Recognize bool          // senza cookie e con riconoscimento attivo: dashboard.js chiama /io
	Anonymous bool          // riconoscimento attivo ma utente non riconosciuto: aiuto per Firefox
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
	office := ""
	if _, p, ok := s.viewerProfile(r); ok {
		office = officeLine(p)
	}
	s.render(w, http.StatusOK, "dashboard.html", dashboardView{
		Office:    office,
		Dashboard: d,
		Greeting:  greeting(now.Hour()),
		Clock:     now.Format("15:04"),
		Version:   s.version,
		Today:     todayInfo(now),
		Calendar:  cw,
		User:      u,
		Recognize: !known && s.canRecognize(r),
		Anonymous: s.recognitionEnabled() && (!known || u.Anonymous),
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

func todayInfo(t time.Time) todayView {
	_, week := t.ISOWeek()
	days := 365
	if y := t.Year(); y%4 == 0 && (y%100 != 0 || y%400 == 0) {
		days = 366
	}
	return todayView{Day: t.Day(), Weekday: weekdays[t.Weekday()], Month: fmt.Sprintf("%s %d", months[t.Month()-1], t.Year()),
		Week: week, YearDay: t.YearDay(), YearDays: days}
}

// heroAttrs: attributi AD letti per la riga sotto il saluto.
var heroAttrs = []string{"physicalDeliveryOfficeName", "department", "title"}

// officeLine: "Ufficio · qualifica" dal profilo AD; "" se non c'è nulla.
func officeLine(p audience.Profile) string {
	first := func(attrs ...string) string {
		for _, a := range attrs {
			for _, v := range p.Attrs[strings.ToLower(a)] {
				if v = strings.TrimSpace(v); v != "" {
					return readable(v)
				}
			}
		}
		return ""
	}
	var parts []string
	for _, v := range []string{first("physicalDeliveryOfficeName", "department"), first("title")} {
		if v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " · ")
}

// readable: un valore AD tutto maiuscolo diventa "Prima lettera maiuscola".
func readable(s string) string {
	if s != strings.ToUpper(s) || s == strings.ToLower(s) {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + strings.ToLower(s[n:])
}
