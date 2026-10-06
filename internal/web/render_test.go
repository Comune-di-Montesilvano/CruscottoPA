package web

import (
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
