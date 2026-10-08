package web

import (
	"fmt"
	"log/slog"
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
	Greeting    string
	Clock       string
	Version     string
	Today       todayView
	Calendar    calendarWidget
	User        identity.User // identità dichiarata: solo per il saluto
	SearchIndex []searchItem  // ricerca a tendina, già filtrata per gruppi
	Hero        heroView      // contatti da AD sotto il saluto (attributi scelti dall'admin)
	Recognize   bool          // senza cookie e con riconoscimento attivo: dashboard.js chiama /io
	Anonymous   bool          // riconoscimento attivo ma utente non riconosciuto: aiuto per Firefox
	Filter      *contentFilter
	Admin       bool // link al pannello admin nel footer
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
	if known && !u.Anonymous && u.Username != "" {
		if err := s.db.TouchPresence(u.Username, u.Name, s.now()); err != nil {
			slog.Warn("presenza", "err", err)
		}
	}
	var hero heroView
	if _, p, ok := s.viewerProfile(r); ok {
		attrs, err := s.db.HeroAttributes()
		if err != nil {
			s.serverError(w, err)
			return
		}
		hero = heroLine(heroItems(p, attrs))
	}
	s.render(w, http.StatusOK, "dashboard.html", dashboardView{
		Hero:        hero,
		SearchIndex: buildSearchIndex(d),
		Dashboard:   d,
		Greeting:    greeting(now.Hour()),
		Clock:       now.Format("15:04"),
		Version:     s.version,
		Today:       todayInfo(now),
		Calendar:    cw,
		User:        u,
		Recognize:   !known && s.canRecognize(r),
		Anonymous:   s.recognitionEnabled() && (!known || u.Anonymous),
		Filter:      f,
		Admin:       s.viewerIsAdmin(r),
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

// heroView: la riga sotto il saluto. Org: i testi (ufficio, qualifica…)
// uniti da «·»; Chips: interno ed email, mostrati come etichette con icona.
type heroView struct {
	Org   string
	Chips []heroItem
}

func heroLine(items []heroItem) heroView {
	var org []string
	v := heroView{}
	for _, it := range items {
		if it.Kind == database.HeroPhone || it.Kind == database.HeroMail {
			v.Chips = append(v.Chips, it)
		} else {
			org = append(org, it.Text)
		}
	}
	v.Org = strings.Join(org, " · ")
	return v
}

// heroItem: una voce della riga sotto il saluto.
type heroItem struct{ Kind, Text string }

// heroItems: valori degli attributi marcati per la testata, nell'ordine
// scelto; primo valore non vuoto, valori vuoti saltati.
func heroItems(p audience.Profile, attrs []database.AudienceAttribute) []heroItem {
	out := []heroItem{}
	for _, a := range attrs {
		v := ""
		for _, x := range p.Attrs[strings.ToLower(a.Name)] {
			if x = strings.TrimSpace(x); x != "" {
				v = x
				break
			}
		}
		if v == "" {
			continue
		}
		switch a.HeroKind {
		case database.HeroPhone:
			v = "Int. " + v
		case database.HeroMail:
		default:
			v = readable(v)
		}
		out = append(out, heroItem{Kind: a.HeroKind, Text: v})
	}
	return out
}

// readable: un valore AD tutto maiuscolo diventa "Prima lettera maiuscola".
func readable(s string) string {
	if s != strings.ToUpper(s) || s == strings.ToLower(s) {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + strings.ToLower(s[n:])
}
