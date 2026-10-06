package database

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenAppliesMigrationsAndSeed(t *testing.T) {
	db := newTestDB(t)

	v, err := db.SchemaVersion()
	if err != nil || v != len(migrations) {
		t.Fatalf("versione schema: attesa %d, ottenuta %d (%v)", len(migrations), v, err)
	}

	var cats, apps int
	db.QueryRow(`SELECT COUNT(*) FROM categories`).Scan(&cats)
	db.QueryRow(`SELECT COUNT(*) FROM apps WHERE url = ''`).Scan(&apps)
	if cats != 1 || apps != 2 {
		t.Fatalf("seed: attese 1 categoria e 2 app senza URL, ottenute %d e %d", cats, apps)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for i := 0; i < 2; i++ {
		db, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i+1, err)
		}
		db.Close()
	}
	db, _ := Open(path)
	defer db.Close()
	var apps int
	db.QueryRow(`SELECT COUNT(*) FROM apps`).Scan(&apps)
	if apps != 2 {
		t.Fatalf("seed ripetuto: attese 2 app, ottenute %d", apps)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.Exec(`INSERT INTO apps (category_id, title) VALUES (999, 'x')`); err == nil {
		t.Fatal("foreign_keys non attive: insert con category_id inesistente accettato")
	}
}

func TestTimeRoundTrip(t *testing.T) {
	in := time.Date(2026, 3, 29, 10, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	s := formatTime(in)
	if s != "2026-03-29T08:00:00Z" {
		t.Fatalf("formatTime: %s", s)
	}
	out, err := parseTime(s)
	if err != nil || !out.Equal(in) {
		t.Fatalf("parseTime: %v %v", out, err)
	}
}
