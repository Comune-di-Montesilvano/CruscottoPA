package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// Sezione "Assistenza": dove chiedere aiuto quando un applicativo non
// funziona. Un canale vale per uno o più applicativi.

type supportRow struct {
	database.SupportChannel
	AppTitles []string
}

type supportAppChoice struct {
	ID      int64
	Title   string
	Note    string // "senza indirizzo", "nascosto"
	Checked bool
}

type supportCategory struct {
	Name string
	Apps []supportAppChoice
}

type supportSection struct {
	Channels   []supportRow
	Form       database.SupportChannel
	Categories []supportCategory
	Errors     formErrors
}

func (s *Server) supportData(form database.SupportChannel, errs formErrors) (supportSection, error) {
	sec := supportSection{Form: form, Errors: errs}
	chans, err := s.db.ListSupportChannels()
	if err != nil {
		return sec, err
	}
	apps, err := s.db.ListApps()
	if err != nil {
		return sec, err
	}
	cats, err := s.db.ListCategories()
	if err != nil {
		return sec, err
	}
	titles := map[int64]string{}
	for _, a := range apps {
		titles[a.ID] = a.Title
	}
	for _, c := range chans {
		r := supportRow{SupportChannel: c}
		for _, id := range c.AppIDs {
			r.AppTitles = append(r.AppTitles, titles[id])
		}
		sec.Channels = append(sec.Channels, r)
	}
	checked := map[int64]bool{}
	for _, id := range form.AppIDs {
		checked[id] = true
	}
	for _, c := range cats {
		sc := supportCategory{Name: c.Name}
		for _, a := range apps {
			if a.CategoryID != c.ID {
				continue
			}
			note := ""
			switch {
			case a.URL == "":
				note = "senza indirizzo"
			case !a.Enabled:
				note = "nascosto"
			}
			sc.Apps = append(sc.Apps, supportAppChoice{ID: a.ID, Title: a.Title, Note: note, Checked: checked[a.ID]})
		}
		if len(sc.Apps) > 0 {
			sec.Categories = append(sec.Categories, sc)
		}
	}
	return sec, nil
}

func (s *Server) renderSupport(w http.ResponseWriter, status int, form database.SupportChannel, errs formErrors) {
	sec, err := s.supportData(form, errs)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, status, "support_section", sec)
}

func newSupportForm() database.SupportChannel {
	return database.SupportChannel{Enabled: true, AppIDs: []int64{}}
}

func (s *Server) handleSupportPage(w http.ResponseWriter, r *http.Request) {
	sec, err := s.supportData(newSupportForm(), nil)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, r, "admin_assistenza.html", "assistenza", sec)
}

func (s *Server) handleSupportEdit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := s.db.GetSupportChannel(id)
	if errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderSupport(w, http.StatusOK, c, nil)
}

func (s *Server) handleSupportSave(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Richiesta non valida", http.StatusBadRequest)
		return
	}
	form := database.SupportChannel{ID: id, AppIDs: []int64{},
		Title:   strings.TrimSpace(r.FormValue("title")),
		URL:     strings.TrimSpace(r.FormValue("url")),
		Note:    strings.TrimSpace(r.FormValue("note")),
		Enabled: r.FormValue("enabled") == "1"}
	errs := formErrors{}
	checkText(errs, "title", form.Title, 80, true)
	checkURL(errs, "url", form.URL, true)
	checkText(errs, "note", form.Note, 200, false)
	apps, err := s.db.ListApps()
	if err != nil {
		s.serverError(w, err)
		return
	}
	known := map[int64]bool{}
	for _, a := range apps {
		known[a.ID] = true
	}
	for _, v := range r.Form["app"] {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || !known[n] {
			errs.add("app", "Applicativo non valido.")
			continue
		}
		form.AppIDs = append(form.AppIDs, n)
	}
	if len(errs) > 0 {
		s.renderSupport(w, http.StatusUnprocessableEntity, form, errs)
		return
	}
	if id == 0 {
		_, err = s.db.CreateSupportChannel(form)
	} else {
		err = s.db.UpdateSupportChannel(form)
	}
	switch {
	case errors.Is(err, database.ErrNotFound):
		http.NotFound(w, r)
	case err != nil:
		s.serverError(w, err)
	default:
		s.renderSupport(w, http.StatusOK, newSupportForm(), nil)
	}
}

func (s *Server) handleSupportDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.DeleteSupportChannel(id); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderSupport(w, http.StatusOK, newSupportForm(), nil)
}

func (s *Server) handleSupportMove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.MoveSupportChannel(id, moveDir(r)); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderSupport(w, http.StatusOK, newSupportForm(), nil)
}
