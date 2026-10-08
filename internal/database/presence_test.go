package database

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPresence(t *testing.T) {
	db := newTestDB(t)
	t0 := time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC)
	if err := db.TouchPresence("Mario.Rossi", "Mario Rossi", t0); err != nil {
		t.Fatal(err)
	}
	db.TouchPresence("mario.rossi", "", t0.Add(2*time.Minute)) // entro 5 minuti: niente scrittura
	db.TouchPresence("", "x", t0)                              // anonimo: ignorato
	ps, _ := db.ListPresence()
	if len(ps) != 1 || ps[0].Username != "mario.rossi" || ps[0].Name != "Mario Rossi" || !ps[0].LastSeen.Equal(t0) {
		t.Fatalf("presenza: %+v", ps)
	}
	db.TouchPresence("mario.rossi", "", t0.Add(6*time.Minute))
	ps, _ = db.ListPresence()
	if !ps[0].LastSeen.Equal(t0.Add(6*time.Minute)) || ps[0].Name != "Mario Rossi" {
		t.Fatalf("dopo 6 minuti: %+v", ps[0])
	}
	db.SetPresenceClient("mario.rossi", true, "granted", t0.Add(7*time.Minute))
	db.SetPresenceClient("mario.rossi", false, "boh", t0.Add(8*time.Minute)) // permesso non valido: resta granted
	db.SetPresenceClient("sconosciuto", true, "granted", t0)                 // senza riga: ignorato
	ps, _ = db.ListPresence()
	if len(ps) != 1 || ps[0].LastApp == nil || !ps[0].LastApp.Equal(t0.Add(7*time.Minute)) || ps[0].Permission != "granted" {
		t.Fatalf("client: %+v", ps[0])
	}
	db.TouchPresence("anna.bianchi", "Anna", t0.Add(-20*24*time.Hour))
	act, _ := db.ActiveUsernames(t0.Add(10 * time.Minute))
	if len(act) != 1 || act[0] != "mario.rossi" {
		t.Fatalf("attivi: %v", act)
	}
}

// Il browser manda app e permesso a ogni caricamento: si scrive solo se
// cambiano (permesso) o ogni PresenceEvery (app).
func TestPresenceClientWritesOnlyOnChange(t *testing.T) {
	db := newTestDB(t)
	t0 := time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC)
	db.TouchPresence("mrossi", "", t0)
	db.SetPresenceClient("mrossi", true, "granted", t0)
	db.SetPresenceClient("mrossi", true, "granted", t0.Add(time.Minute)) // entro 5 minuti, stesso permesso
	ps, _ := db.ListPresence()
	if !ps[0].LastApp.Equal(t0) || !ps[0].PermissionAt.Equal(t0) {
		t.Fatalf("scritture inutili: %+v", ps[0])
	}
	db.SetPresenceClient("mrossi", true, "denied", t0.Add(6*time.Minute))
	ps, _ = db.ListPresence()
	if !ps[0].LastApp.Equal(t0.Add(6*time.Minute)) || ps[0].Permission != "denied" {
		t.Fatalf("cambio: %+v", ps[0])
	}
	if err := db.DeletePresence("MROSSI"); err != nil {
		t.Fatal(err)
	}
	if ps, _ = db.ListPresence(); len(ps) != 0 {
		t.Fatalf("dopo DeletePresence: %+v", ps)
	}
}

// Migrazione v9 su un DB v8 con dati.
func TestMigrationV9OnPopulatedV8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v8.db")
	all := migrations
	t.Cleanup(func() { migrations = all })
	migrations = all[:8]
	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO alerts (title, body, level, source, starts_at, created_at) VALUES ('A', '', 'news', '', '2026-10-01T08:00:00Z', '2026-10-01T08:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	migrations = all
	db, err := Open(path)
	if err != nil {
		t.Fatalf("migrazione v9: %v", err)
	}
	defer db.Close()
	if v, _ := db.SchemaVersion(); v != len(all) {
		t.Fatalf("versione: %d", v)
	}
	alerts, _ := db.ListActiveAlerts(time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC))
	if len(alerts) != 1 {
		t.Fatalf("avviso esistente: %+v", alerts)
	}
	if err := db.MarkRead(alerts[0].ID, "mrossi", ReadOpen, time.Now()); err != nil {
		t.Fatalf("letture dopo la v9: %v", err)
	}
}
