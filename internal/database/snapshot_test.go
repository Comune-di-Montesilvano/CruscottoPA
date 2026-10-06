package database

import (
	"path/filepath"
	"testing"
)

func TestSnapshot(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.CreateCategory("Esterni"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "copia.db")
	if err := db.Snapshot(path); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := db.Snapshot(path); err == nil {
		t.Fatal("VACUUM INTO su file esistente deve fallire: mai sovrascrivere in silenzio")
	}

	cp, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cp.Close()
	cats, _ := cp.ListCategories()
	if len(cats) != 2 || cats[1].Name != "Esterni" {
		t.Fatalf("copia senza i dati: %+v", cats)
	}
	if v, _ := cp.SchemaVersion(); v != CurrentSchemaVersion() {
		t.Fatalf("schema copia %d, atteso %d", v, CurrentSchemaVersion())
	}
}
