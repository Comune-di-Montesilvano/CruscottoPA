// Package calendar calcola festività nazionali, occorrenze degli eventi
// dell'ente, griglia del mese e prossimi appuntamenti per il widget della
// plancia. Logica pura: nessun accesso al database.
package calendar

import (
	"fmt"
	"sort"
	"time"
)

type Kind string

const (
	KindHoliday Kind = "holiday"
	KindClosure Kind = "closure"
	KindEvent   Kind = "event"
)

// Event è un evento del calendario dell'ente (date a mezzanotte UTC).
type Event struct {
	ID          int64
	Title       string
	Kind        Kind
	Start, End  time.Time
	Description string
	Yearly      bool
}

// Occurrence è una manifestazione concreta di un evento o di una festività.
type Occurrence struct {
	Start, End  time.Time
	Title       string
	Kind        Kind
	Description string
}

type DayCell struct {
	Date    time.Time
	Num     int
	ID      string // AAAAMMGG, usato per gli id dei popover
	InMonth bool
	Today   bool
	Red     bool // domenica o festività
	Closure bool
	Event   bool
	Entries []Occurrence
}

type MonthView struct {
	Year  int
	Month time.Month
	Label string // "Ottobre 2026"
	Prev  string // "2026-09"
	Next  string // "2026-11"
	Weeks [][]DayCell
}

var monthNames = [...]string{"Gennaio", "Febbraio", "Marzo", "Aprile", "Maggio", "Giugno",
	"Luglio", "Agosto", "Settembre", "Ottobre", "Novembre", "Dicembre"}

func Date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// Day restituisce il giorno di t (nel fuso di t) a mezzanotte UTC.
func Day(t time.Time) time.Time {
	y, m, d := t.Date()
	return Date(y, m, d)
}

// Easter calcola la Pasqua gregoriana (algoritmo anonimo di Meeus/Jones/Butcher).
func Easter(year int) time.Time {
	a := year % 19
	b, c := year/100, year%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return Date(year, time.Month(month), day)
}

// Holidays restituisce le festività nazionali italiane dell'anno, ordinate.
func Holidays(year int) []Occurrence {
	fixed := []struct {
		m    time.Month
		d    int
		name string
	}{
		{time.January, 1, "Capodanno"},
		{time.January, 6, "Epifania"},
		{time.April, 25, "Festa della Liberazione"},
		{time.May, 1, "Festa del Lavoro"},
		{time.June, 2, "Festa della Repubblica"},
		{time.August, 15, "Ferragosto"},
		{time.November, 1, "Ognissanti"},
		{time.December, 8, "Immacolata Concezione"},
		{time.December, 25, "Natale"},
		{time.December, 26, "Santo Stefano"},
	}
	e := Easter(year)
	pm := e.AddDate(0, 0, 1)
	out := []Occurrence{
		{Start: e, End: e, Title: "Pasqua", Kind: KindHoliday},
		{Start: pm, End: pm, Title: "Lunedì dell'Angelo", Kind: KindHoliday},
	}
	for _, f := range fixed {
		d := Date(year, f.m, f.d)
		out = append(out, Occurrence{Start: d, End: d, Title: f.name, Kind: KindHoliday})
	}
	sortOccurrences(out)
	return out
}

// Occurrences restituisce le occorrenze che si sovrappongono a [from, to]
// (estremi inclusi), espandendo gli eventi annuali e aggiungendo, se richiesto,
// le festività nazionali. Ordinate per inizio.
func Occurrences(events []Event, from, to time.Time, withHolidays bool) []Occurrence {
	from, to = Day(from), Day(to)
	var out []Occurrence
	for _, ev := range events {
		if !ev.Yearly {
			if overlaps(ev.Start, ev.End, from, to) {
				out = append(out, occurrence(ev, ev.Start, ev.End))
			}
			continue
		}
		span := ev.End.Sub(ev.Start)
		for y := from.Year() - 1; y <= to.Year(); y++ {
			if y < ev.Start.Year() {
				continue
			}
			s := yearDate(y, ev.Start.Month(), ev.Start.Day())
			if e := s.Add(span); overlaps(s, e, from, to) {
				out = append(out, occurrence(ev, s, e))
			}
		}
	}
	if withHolidays {
		for y := from.Year(); y <= to.Year(); y++ {
			for _, h := range Holidays(y) {
				if overlaps(h.Start, h.End, from, to) {
					out = append(out, h)
				}
			}
		}
	}
	sortOccurrences(out)
	return out
}

// Month costruisce la griglia del mese (settimane da lunedì a domenica,
// completate con i giorni dei mesi adiacenti).
func Month(year int, month time.Month, events []Event, today time.Time) MonthView {
	first := Date(year, month, 1)
	last := first.AddDate(0, 1, -1)
	start := first.AddDate(0, 0, -mondayIndex(first))
	end := last.AddDate(0, 0, 6-mondayIndex(last))
	occ := Occurrences(events, start, end, true)
	today = Day(today)

	v := MonthView{
		Year: year, Month: month,
		Label: fmt.Sprintf("%s %d", monthNames[month-1], year),
		Prev:  first.AddDate(0, -1, 0).Format("2006-01"),
		Next:  first.AddDate(0, 1, 0).Format("2006-01"),
	}
	var week []DayCell
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		c := DayCell{Date: d, Num: d.Day(), ID: d.Format("20060102"), InMonth: d.Month() == month,
			Today: d.Equal(today), Red: d.Weekday() == time.Sunday}
		for _, o := range occ {
			if d.Before(o.Start) || d.After(o.End) {
				continue
			}
			c.Entries = append(c.Entries, o)
			switch o.Kind {
			case KindHoliday:
				c.Red = true
			case KindClosure:
				c.Closure = true
			case KindEvent:
				c.Event = true
			}
		}
		week = append(week, c)
		if len(week) == 7 {
			v.Weeks = append(v.Weeks, week)
			week = nil
		}
	}
	return v
}

// Upcoming restituisce al massimo n occorrenze (festività comprese) che
// terminano da oggi in poi, entro 400 giorni; un evento in corso compare una volta.
func Upcoming(today time.Time, events []Event, n int) []Occurrence {
	today = Day(today)
	occ := Occurrences(events, today, today.AddDate(0, 0, 400), true)
	if len(occ) > n {
		occ = occ[:n]
	}
	return occ
}

// ParseMonth interpreta "AAAA-MM"; valori assenti, non validi o fuori
// dall'intervallo 1900–2200 danno il mese di today.
func ParseMonth(s string, today time.Time) (int, time.Month) {
	if t, err := time.Parse("2006-01", s); err == nil && t.Year() >= 1900 && t.Year() <= 2200 {
		return t.Year(), t.Month()
	}
	return today.Year(), today.Month()
}

func occurrence(ev Event, s, e time.Time) Occurrence {
	return Occurrence{Start: s, End: e, Title: ev.Title, Kind: ev.Kind, Description: ev.Description}
}

func overlaps(s, e, from, to time.Time) bool { return !e.Before(from) && !s.After(to) }

func isLeap(y int) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }

// yearDate gestisce gli annuali del 29 febbraio negli anni non bisestili.
func yearDate(y int, m time.Month, d int) time.Time {
	if m == time.February && d == 29 && !isLeap(y) {
		d = 28
	}
	return Date(y, m, d)
}

// mondayIndex: lunedì = 0 … domenica = 6.
func mondayIndex(t time.Time) int { return (int(t.Weekday()) + 6) % 7 }

var kindOrder = map[Kind]int{KindHoliday: 0, KindClosure: 1, KindEvent: 2}

func sortOccurrences(o []Occurrence) {
	sort.SliceStable(o, func(i, j int) bool {
		if !o[i].Start.Equal(o[j].Start) {
			return o[i].Start.Before(o[j].Start)
		}
		if kindOrder[o[i].Kind] != kindOrder[o[j].Kind] {
			return kindOrder[o[i].Kind] < kindOrder[o[j].Kind]
		}
		return o[i].Title < o[j].Title
	})
}
