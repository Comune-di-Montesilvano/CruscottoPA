package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/icons"
)

const defaultIconColor = "#475569"

type appForm struct {
	ID          int64
	CategoryID  int64
	Title       string
	Description string
	URL         string
	IconKind    string
	IconValue   string
	IconColor   string
	Enabled     bool
	Visibility  database.ContentAudience
}

type appRow struct {
	database.App
	CategoryName string
	GuideCount   int
}

type appsSection struct {
	Apps       []appRow
	Categories []database.Category
	Form       appForm
	Errors     formErrors

	VisibilityField  visibilityField
	VisibilityLabels map[int64]string
}

func newAppForm() appForm {
	return appForm{IconKind: database.IconPack, IconColor: defaultIconColor, Enabled: true}
}

func formFromApp(a database.App) appForm {
	return appForm{ID: a.ID, CategoryID: a.CategoryID, Title: a.Title, Description: a.Description,
		URL: a.URL, IconKind: a.IconKind, IconValue: a.IconValue, IconColor: a.IconColor, Enabled: a.Enabled}
}

func (s *Server) appsData(form appForm, errs formErrors) (appsSection, error) {
	apps, err := s.db.ListApps()
	if err != nil {
		return appsSection{}, err
	}
	cats, err := s.db.ListCategories()
	if err != nil {
		return appsSection{}, err
	}
	counts, err := s.db.GuideCountsByApp()
	if err != nil {
		return appsSection{}, err
	}
	catNames := map[int64]string{}
	for _, c := range cats {
		catNames[c.ID] = c.Name
	}
	rows := make([]appRow, 0, len(apps))
	for _, a := range apps {
		rows = append(rows, appRow{App: a, CategoryName: catNames[a.CategoryID], GuideCount: counts[a.ID]})
	}
	if form.CategoryID == 0 && len(cats) > 0 {
		form.CategoryID = cats[0].ID
	}
	sec := appsSection{Apps: rows, Categories: cats, Form: form, Errors: errs}
	if sec.VisibilityField, err = s.visibilityField(form.Visibility); err != nil {
		return sec, err
	}
	sec.VisibilityLabels, err = s.visibilityLabels(database.ContentApp)
	return sec, err
}

func (s *Server) renderApps(w http.ResponseWriter, status int, form appForm, errs formErrors) {
	sec, err := s.appsData(form, errs)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, status, "apps_section", sec)
}

func (s *Server) handleAppsPage(w http.ResponseWriter, r *http.Request) {
	form := newAppForm()
	if v := r.URL.Query().Get("modifica"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			if a, err := s.db.GetApp(id); err == nil {
				form = formFromApp(a)
			}
		}
	}
	sec, err := s.appsData(form, nil)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, r, "admin_app.html", "app", sec)
}

func (s *Server) handleAppEdit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := s.db.GetApp(id)
	if errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	form := formFromApp(a)
	if form.Visibility, err = s.db.GetContentAudience(database.ContentApp, id); err != nil {
		s.serverError(w, err)
		return
	}
	s.renderApps(w, http.StatusOK, form, nil)
}

func (s *Server) handleAppSave(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var current database.App
	if id != 0 {
		if current, err = s.db.GetApp(id); errors.Is(err, database.ErrNotFound) {
			http.NotFound(w, r)
			return
		} else if err != nil {
			s.serverError(w, err)
			return
		}
	}

	// Limite sul corpo intero: icona (512 KB) + margine per gli altri campi.
	r.Body = http.MaxBytesReader(w, r.Body, maxIconBytes+64<<10)
	if err := r.ParseMultipartForm(maxIconBytes + 64<<10); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			form := newAppForm()
			if id != 0 {
				form = formFromApp(current)
			}
			s.renderApps(w, http.StatusUnprocessableEntity, form, formErrors{"icon": errIconSize.Error()})
			return
		}
		http.Error(w, "Richiesta non valida", http.StatusBadRequest)
		return
	}

	form := appForm{
		ID:          id,
		Title:       strings.TrimSpace(r.FormValue("title")),
		Description: strings.TrimSpace(r.FormValue("description")),
		URL:         strings.TrimSpace(r.FormValue("url")),
		IconKind:    r.FormValue("icon_kind"),
		IconColor:   strings.TrimSpace(r.FormValue("icon_color")),
		Enabled:     r.FormValue("enabled") == "1",
	}
	form.CategoryID, _ = strconv.ParseInt(r.FormValue("category_id"), 10, 64)
	if form.IconColor == "" {
		form.IconColor = defaultIconColor
	}

	errs := formErrors{}
	checkText(errs, "title", form.Title, 120, true)
	checkText(errs, "description", form.Description, 200, false)
	checkURL(errs, "url", form.URL, false)
	checkColor(errs, "icon", form.IconColor)
	if _, err := s.db.GetCategory(form.CategoryID); errors.Is(err, database.ErrNotFound) {
		errs.add("category", "Scegli una categoria.")
	} else if err != nil {
		s.serverError(w, err)
		return
	}

	switch form.IconKind {
	case database.IconMonogram:
		form.IconValue = ""
	case database.IconPack:
		form.IconValue = strings.TrimSpace(r.FormValue("icon_value_pack"))
		if !icons.Valid(form.IconValue) {
			errs.add("icon", "Scegli un'icona dal catalogo.")
		}
	case database.IconURL:
		form.IconValue = strings.TrimSpace(r.FormValue("icon_value_url"))
		if !validURL(form.IconValue) {
			errs.add("icon", "Indirizzo dell'icona non valido (https://…).")
		}
	case database.IconUpload:
		if current.IconKind == database.IconUpload {
			form.IconValue = current.IconValue // mantenuta se non arriva un nuovo file
		}
	default:
		errs.add("icon", "Tipo di icona non valido.")
	}

	// Il file si salva solo se il resto del form è valido: niente file orfani.
	var uploaded string
	if form.IconKind == database.IconUpload && len(errs) == 0 {
		file, _, err := r.FormFile("icon_file")
		switch {
		case err == nil:
			name, saveErr := s.saveUpload(uploadIcons, file)
			file.Close()
			switch {
			case errors.Is(saveErr, errIconType), errors.Is(saveErr, errIconSize):
				errs.add("icon", saveErr.Error())
			case saveErr != nil:
				s.serverError(w, saveErr)
				return
			default:
				uploaded, form.IconValue = name, name
			}
		case errors.Is(err, http.ErrMissingFile), errors.Is(err, http.ErrNotMultipart):
			if form.IconValue == "" {
				errs.add("icon", "Carica un file per l'icona.")
			}
		default:
			s.serverError(w, err)
			return
		}
	}

	form.Visibility = parseVisibility(r, errs)
	if err := s.checkGroups(form.Visibility, errs); err != nil {
		s.serverError(w, err)
		return
	}
	if len(errs) > 0 {
		s.renderApps(w, http.StatusUnprocessableEntity, form, errs)
		return
	}

	a := database.App{ID: id, CategoryID: form.CategoryID, Title: form.Title, Description: form.Description,
		URL: form.URL, IconKind: form.IconKind, IconValue: form.IconValue, IconColor: form.IconColor, Enabled: form.Enabled}
	if id == 0 {
		id, err = s.db.CreateAppWithAudience(a, form.Visibility)
	} else {
		err = s.db.UpdateAppWithAudience(a, form.Visibility)
	}
	if err != nil {
		s.removeUpload(uploadIcons, uploaded)
		s.serverError(w, err)
		return
	}
	if current.IconKind == database.IconUpload && current.IconValue != a.IconValue {
		s.removeUpload(uploadIcons, current.IconValue)
	}
	s.renderApps(w, http.StatusOK, newAppForm(), nil)
}

func (s *Server) handleAppDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := s.db.GetApp(id)
	if errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	if err := s.db.DeleteApp(id); err != nil {
		s.serverError(w, err)
		return
	}
	if a.IconKind == database.IconUpload {
		s.removeUpload(uploadIcons, a.IconValue)
	}
	s.renderApps(w, http.StatusOK, newAppForm(), nil)
}

func (s *Server) handleAppMove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.MoveApp(id, moveDir(r)); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderApps(w, http.StatusOK, newAppForm(), nil)
}

func (s *Server) handleIconSearch(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "icon_results", icons.Search(r.URL.Query().Get("q"), 60))
}
