package web

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/guidesrc"
)

type guideForm struct {
	ID         int64
	AppID      int64 // 0 = generale
	Kind       string
	Title      string
	URL        string // link
	Body       string // markdown
	File       string // pdf
	SourceURL  string // github
	Enabled    bool
	Visibility database.ContentAudience
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

	VisibilityField  visibilityField
	VisibilityLabels map[int64]string
}

func newGuideForm() guideForm { return guideForm{Kind: database.GuideKindLink, Enabled: true} }

func formFromGuide(g database.Guide) guideForm {
	f := guideForm{ID: g.ID, Kind: g.Kind, Title: g.Title, URL: g.URL, Body: g.Body, File: g.File, SourceURL: g.SourceURL, Enabled: g.Enabled}
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
	sec := guidesSection{Guides: rows, Apps: apps, Form: form, Errors: errs}
	if sec.VisibilityField, err = s.visibilityField(form.Visibility); err != nil {
		return sec, err
	}
	sec.VisibilityLabels, err = s.visibilityLabels(database.ContentGuide)
	return sec, err
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
	form := formFromGuide(g)
	if form.Visibility, err = s.db.GetContentAudience(database.ContentGuide, id); err != nil {
		s.serverError(w, err)
		return
	}
	s.renderGuides(w, http.StatusOK, form, nil)
}

func (s *Server) handleGuideSave(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	form := guideForm{
		ID:        id,
		Kind:      r.FormValue("kind"),
		Title:     strings.TrimSpace(r.FormValue("title")),
		URL:       strings.TrimSpace(r.FormValue("url")),
		Body:      strings.TrimSpace(strings.ReplaceAll(r.FormValue("body"), "\r\n", "\n")),
		File:      r.FormValue("file"),
		SourceURL: strings.TrimSpace(r.FormValue("source_url")),
		Enabled:   r.FormValue("enabled") == "1",
	}
	if form.Kind == "" {
		form.Kind = database.GuideKindLink // form senza tipo (compatibilità)
	}
	form.AppID, _ = strconv.ParseInt(r.FormValue("app_id"), 10, 64)

	errs := formErrors{}
	checkText(errs, "title", form.Title, 120, true)
	var src guidesrc.Source
	switch form.Kind {
	case database.GuideKindLink:
		checkURL(errs, "url", form.URL, true)
	case database.GuideKindMarkdown:
		checkText(errs, "body", form.Body, 100000, true)
	case database.GuideKindPDF:
		if !strings.HasSuffix(form.File, ".pdf") || !guideMediaRe.MatchString(form.File) || !s.mediaExists(form.File) {
			errs.add("file", "Carica il PDF.")
		}
	case database.GuideKindGitHub:
		if src, err = guidesrc.ParseGitHubURL(form.SourceURL); err != nil {
			errs.add("source_url", err.Error())
		}
	default:
		errs.add("kind", "Tipo di guida non valido.")
	}
	if form.AppID != 0 {
		if _, err := s.db.GetApp(form.AppID); errors.Is(err, database.ErrNotFound) {
			errs.add("app", "Applicativo non trovato.")
		} else if err != nil {
			s.serverError(w, err)
			return
		}
	}
	form.Visibility = parseVisibility(r, errs)
	if err := s.checkGroups(form.Visibility, errs); err != nil {
		s.serverError(w, err)
		return
	}
	// La guida com'è ora: PDF da cancellare se cambia, copia GitHub da tenere.
	var cur database.Guide
	if id != 0 {
		if cur, err = s.db.GetGuide(id); err != nil && !errors.Is(err, database.ErrNotFound) {
			s.serverError(w, err)
			return
		}
	}
	// GitHub per ultimo: si scarica solo se il resto del form è valido. Se il
	// link non cambia e GitHub non risponde, si salva lo stesso con l'ultima
	// copia (l'errore resta sulla guida): l'admin può sempre cambiare titolo o
	// visibilità.
	var fetched, fetchErr string
	if form.Kind == database.GuideKindGitHub && len(errs) == 0 {
		if fetched, err = s.fetchGuide(r.Context(), src.Raw); err != nil {
			if cur.Kind == database.GuideKindGitHub && cur.SourceURL == form.SourceURL && cur.FetchedAt != nil {
				fetched, fetchErr = cur.Body, err.Error()
			} else {
				errs.add("source_url", "Download non riuscito: "+err.Error())
			}
		}
	}
	if len(errs) > 0 {
		s.renderGuides(w, http.StatusUnprocessableEntity, form, errs)
		return
	}

	g := database.Guide{ID: id, Title: form.Title, Kind: form.Kind, Enabled: form.Enabled}
	switch form.Kind { // solo i campi del tipo scelto
	case database.GuideKindLink:
		g.URL = form.URL
	case database.GuideKindMarkdown:
		g.Body = form.Body
	case database.GuideKindPDF:
		g.File = form.File
	case database.GuideKindGitHub:
		g.SourceURL, g.Body = form.SourceURL, fetched
	}
	if form.AppID != 0 {
		g.AppID = &form.AppID
	}
	if id == 0 {
		id, err = s.db.CreateGuideWithAudience(g, form.Visibility)
	} else {
		err = s.db.UpdateGuideWithAudience(g, form.Visibility)
	}
	switch {
	case errors.Is(err, database.ErrNotFound):
		http.NotFound(w, r)
	case err != nil:
		s.serverError(w, err)
	default:
		if g.Kind == database.GuideKindGitHub {
			if fetchErr != "" {
				err = s.db.SetGuideFetchError(id, g.SourceURL, fetchErr)
			} else {
				err = s.db.SetGuideFetched(id, g.SourceURL, fetched, s.now())
			}
			if err != nil {
				s.serverError(w, err)
				return
			}
		}
		if cur.File != "" && cur.File != g.File {
			s.removeUpload(uploadGuide, cur.File)
		}
		s.cleanMedia()
		s.renderGuides(w, http.StatusOK, newGuideForm(), nil)
	}
}

// mediaExists: il file caricato c'è davvero (nome già validato da guideMediaRe).
func (s *Server) mediaExists(name string) bool {
	_, err := os.Stat(filepath.Join(s.uploadDir(uploadGuide), name))
	return err == nil
}

// handleGuideRefresh: "Aggiorna ora" di una guida GitHub. L'esito (anche
// l'errore) è salvato sulla guida e si vede nell'elenco.
func (s *Server) handleGuideRefresh(w http.ResponseWriter, r *http.Request) {
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
	if g.Kind == database.GuideKindGitHub {
		s.refreshGuide(r.Context(), g) // l'errore resta sulla guida
	}
	s.renderGuides(w, http.StatusOK, newGuideForm(), nil)
}

// guideKindLabel: tipo mostrato nell'elenco dell'admin.
func guideKindLabel(k string) string {
	switch k {
	case database.GuideKindMarkdown:
		return "Testo"
	case database.GuideKindPDF:
		return "PDF"
	case database.GuideKindGitHub:
		return "GitHub"
	}
	return "Link"
}

func (s *Server) handleGuideDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	g, err := s.db.GetGuide(id)
	if err == nil {
		err = s.db.DeleteGuide(id)
	}
	if errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.removeUpload(uploadGuide, g.File)
	s.cleanMedia()
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
