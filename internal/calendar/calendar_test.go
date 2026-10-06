package calendar

import (
	"testing"
	"time"
)

func TestEaster(t *testing.T) {
	for year, want := range map[int]time.Time{
		2025: Date(2025, 4, 20),
		2026: Date(2026, 4, 5),
		2027: Date(2027, 3, 28),
		2038: Date(2038, 4, 25),
	} {
		if got := Easter(year); !got.Equal(want) {
			t.Errorf("Easter(%d) = %s, atteso %s", year, got.Format("2006-01-02"), want.Format("2006-01-02"))
		}
	}
}

func TestHolidays(t *testing.T) {
	h := Holidays(2026)
	if len(h) != 12 {
		t.Fatalf("attese 12 festività, ottenute %d", len(h))
	}
	has := func(d time.Time, title string) bool {
		for _, o := range h {
			if o.Start.Equal(d) && o.Title == title && o.Kind == KindHoliday {
				return true
			}
		}
		return false
	}
	for _, c := range []struct {
		d     time.Time
		title string
	}{
		{Date(2026, 4, 5), "Pasqua"},
		{Date(2026, 4, 6), "Lunedì dell'Angelo"},
		{Date(2026, 11, 1), "Ognissanti"},
		{Date(2026, 12, 26), "Santo Stefano"},
	} {
		if !has(c.d, c.title) {
			t.Errorf("manca %s il %s", c.title, c.d.Format("2006-01-02"))
		}
	}
	for i := 1; i < len(h); i++ {
		if h[i].Start.Before(h[i-1].Start) {
			t.Fatal("festività non ordinate")
		}
	}
}

func TestMonthGrid(t *testing.T) {
	today := Date(2026, 10, 6)
	oct := Month(2026, time.October, nil, today)
	if oct.Label != "Ottobre 2026" || oct.Prev != "2026-09" || oct.Next != "2026-11" {
		t.Fatalf("intestazione: %+v", oct)
	}
	if len(oct.Weeks) != 5 || !oct.Weeks[0][0].Date.Equal(Date(2026, 9, 28)) || oct.Weeks[0][0].InMonth {
		t.Fatalf("ottobre 2026: attese 5 settimane da lun 28/09, ottenuto %d da %s", len(oct.Weeks), oct.Weeks[0][0].Date.Format("02/01"))
	}
	if c := oct.Weeks[1][1]; c.Num != 6 || !c.Today {
		t.Fatalf("oggi non evidenziato: %+v", c)
	}
	if c := oct.Weeks[0][6]; c.Num != 4 || !c.Red {
		t.Fatal("la domenica 4 ottobre va in rosso")
	}
	if c := oct.Weeks[1][1]; c.ID != "20261006" {
		t.Fatalf("ID cella: %s", c.ID)
	}
	if n := len(Month(2027, time.February, nil, today).Weeks); n != 4 {
		t.Fatalf("febbraio 2027 (da lunedì a domenica): attese 4 settimane, ottenute %d", n)
	}
	if n := len(Month(2026, time.August, nil, today).Weeks); n != 6 {
		t.Fatalf("agosto 2026: attese 6 settimane, ottenute %d", n)
	}
	nov := Month(2026, time.November, nil, today)
	if c := nov.Weeks[0][6]; c.Num != 1 || !c.Red || len(c.Entries) != 1 || c.Entries[0].Title != "Ognissanti" {
		t.Fatalf("1 novembre: festivo con voce Ognissanti, ottenuto %+v", c)
	}
}

func TestEventAcrossMonthsAndYears(t *testing.T) { // Review Focus #1
	ev := []Event{{ID: 1, Title: "Chiusura natalizia", Kind: KindClosure, Start: Date(2026, 12, 30), End: Date(2027, 1, 2)}}
	dec := Month(2026, time.December, ev, Date(2026, 12, 1))
	jan := Month(2027, time.January, ev, Date(2026, 12, 1))
	cell := func(m MonthView, d time.Time) DayCell {
		for _, w := range m.Weeks {
			for _, c := range w {
				if c.Date.Equal(d) {
					return c
				}
			}
		}
		t.Fatalf("giorno %s non in griglia", d)
		return DayCell{}
	}
	for _, c := range []DayCell{cell(dec, Date(2026, 12, 30)), cell(dec, Date(2026, 12, 31)), cell(jan, Date(2027, 1, 1)), cell(jan, Date(2027, 1, 2))} {
		if !c.Closure {
			t.Errorf("%s: attesa chiusura", c.Date.Format("02/01/2006"))
		}
	}
	if cell(jan, Date(2027, 1, 3)).Closure {
		t.Error("il 3 gennaio non è chiuso")
	}
	n := 0
	for _, o := range Upcoming(Date(2026, 12, 20), ev, 20) {
		if o.Title == "Chiusura natalizia" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("nei prossimi l'evento su più giorni compare una volta sola, trovato %d volte", n)
	}
}

func TestYearlyEvents(t *testing.T) {
	ev := []Event{
		{ID: 1, Title: "Patrono", Kind: KindClosure, Start: Date(2025, 5, 16), End: Date(2025, 5, 16), Yearly: true},
		{ID: 2, Title: "Bisestile", Kind: KindEvent, Start: Date(2024, 2, 29), End: Date(2024, 2, 29), Yearly: true},
	}
	occ := Occurrences(ev, Date(2026, 1, 1), Date(2026, 12, 31), false)
	got := map[string]string{}
	for _, o := range occ {
		got[o.Title] = o.Start.Format("2006-01-02")
	}
	if got["Patrono"] != "2026-05-16" || got["Bisestile"] != "2026-02-28" {
		t.Fatalf("annuali nel 2026: %v", got)
	}
	if occ := Occurrences(ev[:1], Date(2024, 1, 1), Date(2024, 12, 31), false); len(occ) != 0 {
		t.Fatalf("un annuale non compare negli anni precedenti alla sua creazione: %+v", occ)
	}
}

func TestUpcoming(t *testing.T) {
	ev := []Event{
		{ID: 1, Title: "Uffici chiusi (ponte)", Kind: KindClosure, Start: Date(2026, 10, 31), End: Date(2026, 10, 31)},
		{ID: 2, Title: "Formazione PEC", Kind: KindEvent, Start: Date(2026, 10, 8), End: Date(2026, 10, 8)},
		{ID: 3, Title: "Passato", Kind: KindEvent, Start: Date(2026, 9, 1), End: Date(2026, 9, 1)},
	}
	up := Upcoming(Date(2026, 10, 6), ev, 3)
	want := []string{"Formazione PEC", "Uffici chiusi (ponte)", "Ognissanti"}
	if len(up) != 3 {
		t.Fatalf("attesi 3 prossimi, ottenuti %+v", up)
	}
	for i, w := range want {
		if up[i].Title != w {
			t.Fatalf("prossimi: atteso %v, ottenuto %+v", want, up)
		}
	}
}

func TestParseMonth(t *testing.T) { // Review Focus #2 (logica)
	today := Date(2026, 10, 6)
	for in, want := range map[string]string{
		"2026-11": "2026-11",
		"2027-01": "2027-01",
		"2026-13": "2026-10",
		"abc":     "2026-10",
		"":        "2026-10",
		"0001-01": "2026-10",
		"9999-12": "2026-10",
	} {
		y, m := ParseMonth(in, today)
		if got := Date(y, m, 1).Format("2006-01"); got != want {
			t.Errorf("ParseMonth(%q) = %s, atteso %s", in, got, want)
		}
	}
}
