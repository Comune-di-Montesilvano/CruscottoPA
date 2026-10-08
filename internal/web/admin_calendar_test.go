package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/calendar"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func calForm(title, kind, start, end string) url.Values {
	return url.Values{"title": {title}, "kind": {kind}, "starts_on": {start}, "ends_on": {end}}
}

func TestCalendarAdminCRUD(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)

	rec := do(t, s, "POST", "/admin/calendario", calForm("Ponte dei Santi", "closure", "2026-10-31", ""), c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Ponte dei Santi") || !strings.Contains(rec.Body.String(), "sab 31 ott") {
		t.Fatalf("crea: %d\n%s", rec.Code, rec.Body)
	}
	evs, _ := db.ListCalendarEvents()
	if len(evs) != 1 || !evs[0].EndsOn.Equal(evs[0].StartsOn) || evs[0].Kind != "closure" {
		t.Fatalf("senza data di fine l'evento dura un giorno: %+v", evs)
	}
	id := itoa(evs[0].ID)

	if rec := do(t, s, "GET", "/admin/calendario/"+id+"/modifica", nil, c, hx); !strings.Contains(rec.Body.String(), `value="2026-10-31"`) {
		t.Fatal("modifica: form non precompilato")
	}
	f := calForm("Festa patronale", "closure", "2026-05-16", "")
	f.Set("yearly", "1")
	if rec := do(t, s, "POST", "/admin/calendario/"+id, f, c, hx); rec.Code != 200 || !strings.Contains(rec.Body.String(), "ogni anno") {
		t.Fatalf("update: %d\n%s", rec.Code, rec.Body)
	}
	if e, _ := db.GetCalendarEvent(evs[0].ID); !e.Yearly || e.Title != "Festa patronale" {
		t.Fatalf("DB dopo update: %+v", e)
	}
	if rec := do(t, s, "POST", "/admin/calendario/"+id+"/elimina", nil, c, hx); rec.Code != 200 {
		t.Fatalf("elimina: %d", rec.Code)
	}
	if evs, _ := db.ListCalendarEvents(); len(evs) != 0 {
		t.Fatal("evento non eliminato")
	}
}

func TestCalendarAdminValidation(t *testing.T) { // Review Focus #4
	s, db := newTestServer(t, nil)
	c := login(t, s)
	for name, tc := range map[string]struct {
		form url.Values
		msg  string
	}{
		"fine prima":      {calForm("x", "event", "2026-10-10", "2026-10-09"), "La fine deve essere uguale o successiva all&#39;inizio."},
		"un anno":         {calForm("x", "closure", "2026-10-10", "2027-10-10"), "Durata massima 31 giorni."},
		"32 giorni":       {calForm("x", "closure", "2026-10-01", "2026-11-01"), "Durata massima 31 giorni."},
		"data non valida": {calForm("x", "event", "2026-13-01", ""), "Data non valida."},
		"titolo vuoto":    {calForm(" ", "event", "2026-10-10", ""), "Campo obbligatorio."},
		"tipo":            {calForm("x", "festa", "2026-10-10", ""), "Tipo non valido."},
	} {
		t.Run(name, func(t *testing.T) {
			rec := do(t, s, "POST", "/admin/calendario", tc.form, c, hx)
			if !invalid(rec) || !strings.Contains(rec.Body.String(), tc.msg) {
				t.Fatalf("atteso 422 con %q, ottenuto %d\n%s", tc.msg, rec.Code, rec.Body)
			}
		})
	}
	if rec := do(t, s, "POST", "/admin/calendario", calForm("31 giorni", "closure", "2026-10-01", "2026-10-31"), c, hx); rec.Code != 200 {
		t.Fatalf("31 giorni esatti sono ammessi: %d", rec.Code)
	}
	if evs, _ := db.ListCalendarEvents(); len(evs) != 1 {
		t.Fatalf("salvato solo l'evento valido, trovati %d", len(evs))
	}
}

func TestCalendarAdminListsAndAccess(t *testing.T) {
	s, db := newTestServer(t, nil)
	db.CreateCalendarEvent(database.CalendarEvent{Title: "Vecchio evento", Kind: "event", StartsOn: calendar.Date(2026, 9, 1), EndsOn: calendar.Date(2026, 9, 1)})
	db.CreateCalendarEvent(database.CalendarEvent{Title: "Patrono", Kind: "closure", StartsOn: calendar.Date(2020, 5, 16), EndsOn: calendar.Date(2020, 5, 16), Yearly: true})
	if rec := do(t, s, "GET", "/admin/calendario", nil, nil, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("senza sessione: %d", rec.Code)
	}
	c := login(t, s)
	page := do(t, s, "GET", "/admin/calendario", nil, c, nil).Body.String()
	past := strings.Index(page, "Passati")
	if past < 0 || strings.Index(page, "Vecchio evento") < past {
		t.Fatal("un evento concluso va tra i Passati")
	}
	if i := strings.Index(page, "Patrono"); i < 0 || i > past || !strings.Contains(page, "ogni anno") {
		t.Fatal("un annuale resta sempre tra i Prossimi")
	}
	if !strings.Contains(page, `href="/admin/calendario" aria-current="page"`) {
		t.Fatal("voce Calendario nel menu")
	}
}

func TestOverviewShowsUpcomingClosures(t *testing.T) {
	s, db := newTestServer(t, nil)
	db.CreateCalendarEvent(database.CalendarEvent{Title: "Chiusura per disinfestazione", Kind: "closure", StartsOn: calendar.Date(2026, 10, 10), EndsOn: calendar.Date(2026, 10, 10)})
	db.CreateCalendarEvent(database.CalendarEvent{Title: "Chiusura lontana", Kind: "closure", StartsOn: calendar.Date(2026, 11, 30), EndsOn: calendar.Date(2026, 11, 30)})
	db.CreateCalendarEvent(database.CalendarEvent{Title: "Corso", Kind: "event", StartsOn: calendar.Date(2026, 10, 9), EndsOn: calendar.Date(2026, 10, 9)})
	c := login(t, s)
	body := do(t, s, "GET", "/admin", nil, c, nil).Body.String()
	if !strings.Contains(body, "Chiusure nei prossimi 14 giorni") || !strings.Contains(body, "Chiusura per disinfestazione") {
		t.Fatal("manca la chiusura imminente in panoramica")
	}
	if strings.Contains(body, "Chiusura lontana") || strings.Contains(body, "Corso") {
		t.Fatal("solo chiusure entro 14 giorni")
	}
}

// Anno digitato male (0026 al posto di 2026, 2062): va segnalato, e nell'elenco
// admin l'anno deve essere visibile per accorgersi degli errori.
func TestCalendarAdminImplausibleYear(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	for _, d := range []string{"0026-10-10", "2062-10-10", "2015-10-10"} {
		rec := do(t, s, "POST", "/admin/calendario", calForm("x", "event", d, ""), c, hx)
		if !invalid(rec) || !strings.Contains(rec.Body.String(), "Anno non plausibile") {
			t.Errorf("%s: atteso 422 Anno non plausibile, ottenuto %d", d, rec.Code)
		}
	}
	rec := do(t, s, "POST", "/admin/calendario", calForm("Corso", "event", "2027-03-10", ""), c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "mer 10 mar 2027") {
		t.Fatalf("nell'elenco admin la data deve mostrare l'anno: %d\n%s", rec.Code, rec.Body)
	}
	if evs, _ := db.ListCalendarEvents(); len(evs) != 1 {
		t.Fatalf("salvato solo l'evento valido, trovati %d", len(evs))
	}
}
