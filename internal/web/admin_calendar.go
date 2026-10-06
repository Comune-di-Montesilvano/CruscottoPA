package web

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/calendar"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

const maxEventDays = 31

type calendarForm struct {
	ID          int64
	Title       string
	Kind        string
	StartsOn    string // AAAA-MM-GG
	EndsOn      string // "" = stesso giorno
	Description string
	Yearly      bool
}

type calendarRow struct {
	database.CalendarEvent
	When string
}

type calendarSection struct {
	Upcoming []calendarRow
	Past     []calendarRow
	Form     calendarForm
	Errors   formErrors
}

func newCalendarForm() calendarForm { return calendarForm{Kind: "closure"} }

func formFromCalendarEvent(e database.CalendarEvent) calendarForm {
	f := calendarForm{ID: e.ID, Title: e.Title, Kind: e.Kind, StartsOn: e.StartsOn.Format(database.DayLayout),
		Description: e.Description, Yearly: e.Yearly}
	if !e.EndsOn.Equal(e.StartsOn) {
		f.EndsOn = e.EndsOn.Format(database.DayLayout)
	}
	return f
}

// validateCalendarForm restituisce l'evento da salvare oppure gli errori per campo.
func validateCalendarForm(f calendarForm) (database.CalendarEvent, formErrors) {
	errs := formErrors{}
	checkText(errs, "title", f.Title, 120, true)
	checkText(errs, "description", f.Description, 500, false)
	if f.Kind != "closure" && f.Kind != "event" {
		errs.add("kind", "Tipo non valido.")
	}
	start, startErr := time.Parse(database.DayLayout, f.StartsOn)
	if startErr != nil {
		errs.add("starts_on", "Data non valida.")
	}
	end := start
	if f.EndsOn != "" {
		e, err := time.Parse(database.DayLayout, f.EndsOn)
		if err != nil {
			errs.add("ends_on", "Data non valida.")
		} else {
			end = e
		}
	}
	if startErr == nil && errs["ends_on"] == "" {
		switch {
		case end.Before(start):
			errs.add("ends_on", "La fine deve essere uguale o successiva all'inizio.")
		case end.Sub(start) > (maxEventDays-1)*24*time.Hour:
			errs.add("ends_on", "Durata massima 31 giorni.")
		}
	}
	return database.CalendarEvent{ID: f.ID, Title: f.Title, Kind: f.Kind, StartsOn: start, EndsOn: end,
		Description: f.Description, Yearly: f.Yearly}, errs
}

func calendarWhen(e database.CalendarEvent) string {
	w := occRange(calendar.Occurrence{Start: e.StartsOn, End: e.EndsOn})
	if e.Yearly {
		w += " · ogni anno"
	}
	return w
}

func (s *Server) calendarData(form calendarForm, errs formErrors) (calendarSection, error) {
	evs, err := s.db.ListCalendarEvents()
	if err != nil {
		return calendarSection{}, err
	}
	today := s.today()
	sec := calendarSection{Form: form, Errors: errs}
	for _, e := range evs {
		row := calendarRow{CalendarEvent: e, When: calendarWhen(e)}
		if e.Yearly || !e.EndsOn.Before(today) {
			sec.Upcoming = append(sec.Upcoming, row)
		} else {
			sec.Past = append(sec.Past, row)
		}
	}
	sort.Slice(sec.Past, func(i, j int) bool { return sec.Past[i].EndsOn.After(sec.Past[j].EndsOn) })
	if len(sec.Past) > 30 {
		sec.Past = sec.Past[:30]
	}
	return sec, nil
}

func (s *Server) renderCalendar(w http.ResponseWriter, status int, form calendarForm, errs formErrors) {
	sec, err := s.calendarData(form, errs)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, status, "calendar_section", sec)
}

func (s *Server) handleCalendarPage(w http.ResponseWriter, r *http.Request) {
	sec, err := s.calendarData(newCalendarForm(), nil)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, r, "admin_calendario.html", "calendario", sec)
}

func (s *Server) handleCalendarEdit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	e, err := s.db.GetCalendarEvent(id)
	if errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderCalendar(w, http.StatusOK, formFromCalendarEvent(e), nil)
}

func (s *Server) handleCalendarSave(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	form := calendarForm{
		ID:          id,
		Title:       strings.TrimSpace(r.FormValue("title")),
		Kind:        r.FormValue("kind"),
		StartsOn:    strings.TrimSpace(r.FormValue("starts_on")),
		EndsOn:      strings.TrimSpace(r.FormValue("ends_on")),
		Description: strings.TrimSpace(r.FormValue("description")),
		Yearly:      r.FormValue("yearly") == "1",
	}
	ev, errs := validateCalendarForm(form)
	if len(errs) > 0 {
		s.renderCalendar(w, http.StatusUnprocessableEntity, form, errs)
		return
	}
	if id == 0 {
		_, err = s.db.CreateCalendarEvent(ev)
	} else {
		err = s.db.UpdateCalendarEvent(ev)
	}
	switch {
	case errors.Is(err, database.ErrNotFound):
		http.NotFound(w, r)
	case err != nil:
		s.serverError(w, err)
	default:
		s.renderCalendar(w, http.StatusOK, newCalendarForm(), nil)
	}
}

func (s *Server) handleCalendarDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.DeleteCalendarEvent(id); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderCalendar(w, http.StatusOK, newCalendarForm(), nil)
}
