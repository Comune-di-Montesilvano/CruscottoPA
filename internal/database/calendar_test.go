package database

import (
	"errors"
	"testing"
	"time"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestCalendarEventCRUD(t *testing.T) {
	db := newTestDB(t)
	id, err := db.CreateCalendarEvent(CalendarEvent{Title: "Patrono", Kind: "closure",
		StartsOn: day(2026, 5, 16), EndsOn: day(2026, 5, 16), Yearly: true})
	if err != nil {
		t.Fatal(err)
	}
	db.CreateCalendarEvent(CalendarEvent{Title: "Formazione", Kind: "event",
		StartsOn: day(2026, 3, 1), EndsOn: day(2026, 3, 2), Description: "Sala consiliare"})

	list, err := db.ListCalendarEvents()
	if err != nil || len(list) != 2 || list[0].Title != "Formazione" || !list[0].EndsOn.Equal(day(2026, 3, 2)) || list[0].Description != "Sala consiliare" {
		t.Fatalf("ListCalendarEvents: %+v %v", list, err)
	}

	e, _ := db.GetCalendarEvent(id)
	if !e.Yearly || e.Kind != "closure" || !e.StartsOn.Equal(day(2026, 5, 16)) {
		t.Fatalf("GetCalendarEvent: %+v", e)
	}
	e.Title, e.Yearly = "Festa patronale", false
	if err := db.UpdateCalendarEvent(e); err != nil {
		t.Fatal(err)
	}
	if e, _ = db.GetCalendarEvent(id); e.Title != "Festa patronale" || e.Yearly {
		t.Fatalf("UpdateCalendarEvent: %+v", e)
	}
	if err := db.DeleteCalendarEvent(id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetCalendarEvent(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dopo delete: %v", err)
	}
	if err := db.UpdateCalendarEvent(CalendarEvent{ID: 999, Title: "x", Kind: "event", StartsOn: day(2026, 1, 1), EndsOn: day(2026, 1, 1)}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update inesistente: %v", err)
	}
}

func TestCalendarEventKindChecked(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.CreateCalendarEvent(CalendarEvent{Title: "x", Kind: "festa", StartsOn: day(2026, 1, 1), EndsOn: day(2026, 1, 1)}); err == nil {
		t.Fatal("tipo non ammesso accettato dal DB")
	}
}
