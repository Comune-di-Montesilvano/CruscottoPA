package database

import (
	"testing"
	"time"
)

func TestBrandingDefaultsAndUpdate(t *testing.T) {
	db := newTestDB(t)

	b, err := db.GetBranding()
	if err != nil || b != (Branding{WebSearch: DefaultWebSearch}) {
		t.Fatalf("branding iniziale: atteso vuoto (con Google come ricerca web), ottenuto %+v (%v)", b, err)
	}

	want := Branding{
		EnteName:  "Comune di Esempio",
		LogoFile:  "0123456789abcdef0123456789abcdef.png",
		UpdatedAt: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC),
		UpdatedBy: "mrossi",
	}
	if err := db.UpdateBranding(want); err != nil {
		t.Fatalf("UpdateBranding: %v", err)
	}
	if got, err := db.GetBranding(); err != nil || got != want {
		t.Fatalf("GetBranding: atteso %+v, ottenuto %+v (%v)", want, got, err)
	}

	var n int
	db.QueryRow(`SELECT COUNT(*) FROM branding`).Scan(&n)
	if n != 1 {
		t.Fatalf("righe in branding: attesa 1, ottenute %d", n)
	}
	if _, err := db.Exec(`INSERT INTO branding (id) VALUES (2)`); err == nil {
		t.Fatal("una seconda riga di branding non deve essere ammessa")
	}
}

// Ricerca sul web dalla plancia: Google predefinito, sito dell'ente vuoto.
func TestBrandingWebSearch(t *testing.T) {
	db := newTestDB(t)
	b, err := db.GetBranding()
	if err != nil || b.WebSearch != DefaultWebSearch || b.SiteSearch != "" {
		t.Fatalf("predefiniti: %+v %v", b, err)
	}
	b.WebSearch = "https://www.bing.com/search?q=%s"
	b.SiteSearch = "https://www.comune.example.it/content/search?SearchText=%s"
	if err := db.UpdateBranding(b); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetBranding(); got.WebSearch != b.WebSearch || got.SiteSearch != b.SiteSearch {
		t.Fatalf("salvati: %+v", got)
	}
}
