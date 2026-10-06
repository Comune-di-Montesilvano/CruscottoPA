package web

import (
	"net/http"

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
	ActiveAlerts []database.Alert
	Incomplete   []database.App
	Apps         int
	Guides       int
	Categories   int
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
	s.renderPage(w, r, "admin_overview.html", "overview", v)
}
