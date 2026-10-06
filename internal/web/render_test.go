package web

import (
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/calendar"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"strings"
	"testing"
)

func TestMonogram(t *testing.T) {
	for in, want := range map[string]string{
		"Rubrica":                 "Ru",
		"Sito comunale":           "SC",
		"albo pretorio":           "AP",
		"  ":                      "?",
		"Èlite":                   "Èl",
		"Sportello del cittadino": "SD",
	} {
		if got := monogram(in); got != want {
			t.Errorf("monogram(%q) = %q, atteso %q", in, got, want)
		}
	}
}

func TestLevelLabel(t *testing.T) {
	if levelLabel("urgent") != "Urgente" || levelLabel("maintenance") != "Manutenzione" || levelLabel("news") != "Novità" {
		t.Fatal("etichette livello errate")
	}
}

func TestLinkify(t *testing.T) {
	got := string(linkify("Vedi https://wiki.local/a?b=1&c=2 <b>ora</b>\nriga 2"))
	if !strings.Contains(got, `<a href="https://wiki.local/a?b=1&amp;c=2" target="_blank" rel="noopener">`) {
		t.Fatalf("link mancante o non escapato: %s", got)
	}
	if strings.Contains(got, "<b>") || !strings.Contains(got, "&lt;b&gt;ora&lt;/b&gt;") {
		t.Fatalf("HTML del testo non escapato: %s", got)
	}
	if strings.Contains(string(linkify(`javascript:alert(1)`)), "<a") {
		t.Fatal("solo http/https diventano link")
	}
}

func TestHumanSizeAndKindLabel(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 512: "512 B", 1536: "1,5 KB", 5 << 20: "5,0 MB", 3 << 30: "3,0 GB"} {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, atteso %q", n, got, want)
		}
	}
	if kindLabel("auto") != "Automatico" || kindLabel("manuale") != "Manuale" || kindLabel("pre-ripristino") != "Pre-ripristino" {
		t.Fatal("etichette tipo backup errate")
	}
}

func TestTint(t *testing.T) {
	for in, want := range map[string]string{
		"#000000": "#d9d9d9",
		"#ffffff": "#ffffff",
		"#2563eb": "#dee8fc",
		"red":     "#eef1f5",
		"":        "#eef1f5",
	} {
		if got := tint(in); got != want {
			t.Errorf("tint(%q) = %q, atteso %q", in, got, want)
		}
	}
}

func TestAlertVersion(t *testing.T) { // Review Focus #3
	start := time.Date(2026, 10, 6, 7, 0, 0, 0, time.UTC)
	a := database.Alert{ID: 1, Title: "Phishing", Body: "Non aprire", Level: "urgent", StartsAt: start}
	v := alertVersion(a)
	if len(v) != 8 || alertVersion(a) != v {
		t.Fatalf("versione non stabile o di lunghezza errata: %q", v)
	}
	b := a
	b.Body = "Non aprire gli allegati .zip"
	end := start.Add(time.Hour)
	c := a
	c.EndsAt = &end
	if alertVersion(b) == v || alertVersion(c) == v {
		t.Fatal("modificare testo o scadenza deve cambiare la versione")
	}
	d := a
	d.Source = "CED"
	if alertVersion(d) != v {
		t.Fatal("la fonte non deve far ricomparire un urgente già letto")
	}
}

func TestAlertSourceShortDayOccRange(t *testing.T) {
	if alertSource("") != "Servizio informatico" || alertSource("  ") != "Servizio informatico" || alertSource("Ufficio Stipendi") != "Ufficio Stipendi" {
		t.Fatal("alertSource")
	}
	if got := shortDay(calendar.Date(2026, 10, 8)); got != "gio 8 ott" {
		t.Fatalf("shortDay: %q", got)
	}
	one := calendar.Occurrence{Start: calendar.Date(2026, 10, 31), End: calendar.Date(2026, 10, 31)}
	two := calendar.Occurrence{Start: calendar.Date(2026, 12, 30), End: calendar.Date(2027, 1, 2)}
	if occRange(one) != "sab 31 ott" || occRange(two) != "mer 30 dic – sab 2 gen" {
		t.Fatalf("occRange: %q / %q", occRange(one), occRange(two))
	}
}
