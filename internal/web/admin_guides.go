package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type guideForm struct {
	ID      int64
	AppID   int64 // 0 = generale
	Title   string
	URL     string
	Enabled bool
}

type guideRow struct {
	database.Guide
	AppTitle string // "" = generale
}

type guidesSection struct {
	Guides []guideRow
	Apps   []database.App
	Form   guideForm
	Errors formErrors
}

func newGuideForm() guideForm { return guideForm{Enabled: true} }

func formFromGuide(g database.Guide) guideForm {
	f := guideForm{ID: g.ID, Title: g.Title, URL: g.URL, Enabled: g.Enabled}
	if g.AppID != nil {
		f.AppID = *g.AppID
	}
	return f
}

func (s *Server) guidesData(form guideForm, errs formErrors) (guidesSection, error) {
	guides, err := s.db.ListGuides()
	if err != nil {
		return guidesSection{}, err
	}
	apps, err := s.db.ListApps()
	if err != nil {
		return guidesSection{}, err
	}
	titles := map[int64]string{}
	for _, a := range apps {
		titles[a.ID] = a.Title
	}
	rows := make([]guideRow, 0, len(guides))
	for _, g := range guides {
		row := guideRow{Guide: g}
		if g.AppID != nil {
			row.AppTitle = titles[*g.AppID]
		}
		rows = append(rows, row)
	}
	return guidesSection{Guides: rows, Apps: apps, Form: form, Errors: errs}, nil
}

func (s *Server) renderGuides(w http.ResponseWriter, status int, form guideForm, errs formErrors) {
	sec, err := s.guidesData(form, errs)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, status, "guides_section", sec)
}

func (s *Server) handleGuidesPage(w http.ResponseWriter, r *http.Request) {
	sec, err := s.guidesData(newGuideForm(), nil)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, r, "admin_guide.html", "guide", sec)
}

func (s *Server) handleGuideEdit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	g, err := s.db.GetGuide(id)
	if errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderGuides(w, http.StatusOK, formFromGuide(g), nil)
}

func (s *Server) handleGuideSave(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	form := guideForm{
		ID:      id,
		Title:   strings.TrimSpace(r.FormValue("title")),
		URL:     strings.TrimSpace(r.FormValue("url")),
		Enabled: r.FormValue("enabled") == "1",
	}
	form.AppID, _ = strconv.ParseInt(r.FormValue("app_id"), 10, 64)

	errs := formErrors{}
	checkText(errs, "title", form.Title, 120, true)
	checkURL(errs, "url", form.URL, true)
	if form.AppID != 0 {
		if _, err := s.db.GetApp(form.AppID); errors.Is(err, database.ErrNotFound) {
			errs.add("app", "Applicativo non trovato.")
		} else if err != nil {
			s.serverError(w, err)
			return
		}
	}
	if len(errs) > 0 {
		s.renderGuides(w, http.StatusUnprocessableEntity, form, errs)
		return
	}

	g := database.Guide{ID: id, Title: form.Title, Kind: database.GuideKindLink, URL: form.URL, Enabled: form.Enabled}
	if form.AppID != 0 {
		g.AppID = &form.AppID
	}
	if id == 0 {
		_, err = s.db.CreateGuide(g)
	} else {
		err = s.db.UpdateGuide(g)
	}
	switch {
	case errors.Is(err, database.ErrNotFound):
		http.NotFound(w, r)
	case err != nil:
		s.serverError(w, err)
	default:
		s.renderGuides(w, http.StatusOK, newGuideForm(), nil)
	}
}

func (s *Server) handleGuideDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.DeleteGuide(id); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderGuides(w, http.StatusOK, newGuideForm(), nil)
}

func (s *Server) handleGuideMove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.MoveGuide(id, moveDir(r)); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderGuides(w, http.StatusOK, newGuideForm(), nil)
}
