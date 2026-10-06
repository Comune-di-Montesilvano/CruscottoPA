package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type alertForm struct {
	ID       int64
	Title    string
	Body     string
	Level    string
	StartsAt string // formato inputTimeLayout, fuso s.loc()
	EndsAt   string // "" = senza scadenza
}

type alertRow struct {
	database.Alert
	Status string // Attivo | Programmato | Scaduto
}

type alertsSection struct {
	Current []alertRow
	Expired []alertRow
	Form    alertForm
	Errors  formErrors
}

var alertLevels = []string{database.LevelUrgent, database.LevelMaintenance, database.LevelNews}

func (s *Server) parseInputTime(v string) (time.Time, error) {
	return time.ParseInLocation(inputTimeLayout, v, s.loc())
}

func (s *Server) newAlertForm() alertForm {
	return alertForm{Level: database.LevelNews, StartsAt: s.now().In(s.loc()).Format(inputTimeLayout)}
}

func (s *Server) formFromAlert(a database.Alert) alertForm {
	f := alertForm{ID: a.ID, Title: a.Title, Body: a.Body, Level: a.Level,
		StartsAt: a.StartsAt.In(s.loc()).Format(inputTimeLayout)}
	if a.EndsAt != nil {
		f.EndsAt = a.EndsAt.In(s.loc()).Format(inputTimeLayout)
	}
	return f
}

func (s *Server) alertStatus(a database.Alert) string {
	now := s.now()
	switch {
	case a.EndsAt != nil && !now.Before(*a.EndsAt):
		return "Scaduto"
	case a.StartsAt.After(now):
		return "Programmato"
	default:
		return "Attivo"
	}
}

func (s *Server) alertsData(form alertForm, errs formErrors) (alertsSection, error) {
	current, expired, err := s.db.ListAlertsForAdmin(s.now())
	if err != nil {
		return alertsSection{}, err
	}
	sec := alertsSection{Form: form, Errors: errs}
	for _, a := range current {
		sec.Current = append(sec.Current, alertRow{Alert: a, Status: s.alertStatus(a)})
	}
	for _, a := range expired {
		sec.Expired = append(sec.Expired, alertRow{Alert: a, Status: "Scaduto"})
	}
	return sec, nil
}

func (s *Server) renderAlerts(w http.ResponseWriter, status int, form alertForm, errs formErrors) {
	sec, err := s.alertsData(form, errs)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, status, "alerts_section", sec)
}

func (s *Server) handleAlertsPage(w http.ResponseWriter, r *http.Request) {
	sec, err := s.alertsData(s.newAlertForm(), nil)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, r, "admin_avvisi.html", "avvisi", sec)
}

func (s *Server) handleAlertEdit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := s.db.GetAlert(id)
	if errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderAlerts(w, http.StatusOK, s.formFromAlert(a), nil)
}

func (s *Server) handleAlertSave(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	form := alertForm{
		ID:       id,
		Title:    strings.TrimSpace(r.FormValue("title")),
		Body:     strings.TrimSpace(strings.ReplaceAll(r.FormValue("body"), "\r\n", "\n")),
		Level:    r.FormValue("level"),
		StartsAt: strings.TrimSpace(r.FormValue("starts_at")),
		EndsAt:   strings.TrimSpace(r.FormValue("ends_at")),
	}

	errs := formErrors{}
	checkText(errs, "title", form.Title, 120, true)
	checkText(errs, "body", form.Body, 2000, false)
	validLevel := false
	for _, l := range alertLevels {
		validLevel = validLevel || form.Level == l
	}
	if !validLevel {
		errs.add("level", "Livello non valido.")
	}
	starts, err := s.parseInputTime(form.StartsAt)
	if err != nil {
		errs.add("starts_at", "Data e ora non valide.")
	}
	var ends *time.Time
	if form.EndsAt != "" {
		e, err := s.parseInputTime(form.EndsAt)
		switch {
		case err != nil:
			errs.add("ends_at", "Data e ora non valide.")
		case !e.After(starts):
			errs.add("ends_at", "La fine deve essere successiva all'inizio.")
		default:
			ends = &e
		}
	}
	if len(errs) > 0 {
		s.renderAlerts(w, http.StatusUnprocessableEntity, form, errs)
		return
	}

	a := database.Alert{ID: id, Title: form.Title, Body: form.Body, Level: form.Level, StartsAt: starts, EndsAt: ends}
	if id == 0 {
		a.CreatedAt = s.now()
		a.CreatedBy = s.currentAdmin(r)
		_, err = s.db.CreateAlert(a)
	} else {
		var old database.Alert
		if old, err = s.db.GetAlert(id); err == nil {
			a.Notify = old.Notify // gestito dal sotto-progetto 3
			err = s.db.UpdateAlert(a)
		}
	}
	switch {
	case errors.Is(err, database.ErrNotFound):
		http.NotFound(w, r)
	case err != nil:
		s.serverError(w, err)
	default:
		s.renderAlerts(w, http.StatusOK, s.newAlertForm(), nil)
	}
}

func (s *Server) handleAlertDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.DeleteAlert(id); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderAlerts(w, http.StatusOK, s.newAlertForm(), nil)
}
