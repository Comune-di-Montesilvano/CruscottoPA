package web

import "testing"

func TestValidURL(t *testing.T) {
	for in, want := range map[string]bool{
		"https://sicraweb.comune.local/login": true,
		"http://10.0.0.5:8080":                true,
		"HTTPS://Esempio.it":                  true,
		"www.comune.it":                       false, // Review Focus #1
		"comune.it/albo":                      false,
		"javascript:alert(1)":                 false,
		"ftp://files.local":                   false,
		"https://":                            false,
		"https://a b.it":                      false,
		"":                                    false,
	} {
		if got := validURL(in); got != want {
			t.Errorf("validURL(%q) = %v, atteso %v", in, got, want)
		}
	}
}

func TestCheckURLMessage(t *testing.T) {
	e := formErrors{}
	checkURL(e, "url", "www.comune.it", false)
	if e["url"] != "Inserisci l'indirizzo completo, es. https://…" {
		t.Fatalf("messaggio: %q", e["url"])
	}
	e = formErrors{}
	checkURL(e, "url", "", false)
	if len(e) != 0 {
		t.Fatal("URL facoltativo vuoto: nessun errore")
	}
}

func TestCheckTextAndColor(t *testing.T) {
	e := formErrors{}
	checkText(e, "title", "  ", 10, true)
	checkText(e, "desc", "àèìòùàèìòù", 10, false) // 10 rune, 20 byte: ok
	checkText(e, "long", "12345678901", 10, false)
	checkColor(e, "c1", "#1C4F9b")
	checkColor(e, "c2", "red")
	if e["title"] != "Campo obbligatorio." || e["long"] == "" || e["c2"] == "" {
		t.Fatalf("errori attesi mancanti: %v", e)
	}
	if _, ok := e["desc"]; ok {
		t.Fatal("la lunghezza va contata in caratteri, non in byte")
	}
	if _, ok := e["c1"]; ok {
		t.Fatal("#1C4F9b è un colore valido")
	}
}
