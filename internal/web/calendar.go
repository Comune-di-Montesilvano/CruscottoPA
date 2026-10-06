package web

import (
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/calendar"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func toCalendarEvents(evs []database.CalendarEvent) []calendar.Event {
	out := make([]calendar.Event, 0, len(evs))
	for _, e := range evs {
		out = append(out, calendar.Event{ID: e.ID, Title: e.Title, Kind: calendar.Kind(e.Kind),
			Start: e.StartsOn, End: e.EndsOn, Description: e.Description, Yearly: e.Yearly})
	}
	return out
}

// today è il giorno corrente nel fuso TZ, a mezzanotte UTC (come le date di calendario).
func (s *Server) today() time.Time { return calendar.Day(s.now().In(s.loc())) }
