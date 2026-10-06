package web

import (
	"net/http"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/calendar"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type adminPage struct {
	User    string
	Section string
	Version string
}

// pageView è il dato di ogni pagina admin: shell (adminPage) + contenuto (Body).
type pageView struct {
	adminPage
	Body any
}

func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, page, section string, body any) {
	s.render(w, http.StatusOK, page, pageView{
		adminPage: adminPage{User: s.currentAdmin(r), Section: section, Version: s.version},
		Body:      body,
	})
}

type overviewView struct {
	ActiveAlerts  []database.Alert
	Incomplete    []database.App
	Apps          int
	Guides        int
	Categories    int
	BackupWarning string
	Closures      []calendar.Occurrence
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	alerts, err := s.db.ListActiveAlerts(s.now())
	if err != nil {
		s.serverError(w, err)
		return
	}
	apps, err := s.db.ListApps()
	if err != nil {
		s.serverError(w, err)
		return
	}
	guides, err := s.db.ListGuides()
	if err != nil {
		s.serverError(w, err)
		return
	}
	cats, err := s.db.ListCategories()
	if err != nil {
		s.serverError(w, err)
		return
	}
	v := overviewView{ActiveAlerts: alerts, Incomplete: []database.App{},
		Apps: len(apps), Guides: len(guides), Categories: len(cats)}
	for _, a := range apps {
		if a.URL == "" {
			v.Incomplete = append(v.Incomplete, a)
		}
	}
	v.BackupWarning = backupWarning(s.backup.Status(), s.now())
	evs, err := s.db.ListCalendarEvents()
	if err != nil {
		s.serverError(w, err)
		return
	}
	today := s.today()
	for _, o := range calendar.Occurrences(toCalendarEvents(evs), today, today.AddDate(0, 0, 13), false) {
		if o.Kind == calendar.KindClosure {
			v.Closures = append(v.Closures, o)
		}
	}
	s.renderPage(w, r, "admin_overview.html", "overview", v)
}
