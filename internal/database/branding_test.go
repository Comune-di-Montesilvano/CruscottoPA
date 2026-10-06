package database

import (
	"testing"
	"time"
)

func TestBrandingDefaultsAndUpdate(t *testing.T) {
	db := newTestDB(t)

	b, err := db.GetBranding()
	if err != nil || b != (Branding{}) {
		t.Fatalf("branding iniziale: atteso vuoto, ottenuto %+v (%v)", b, err)
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
