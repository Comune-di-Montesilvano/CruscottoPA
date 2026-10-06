package database

import (
	"errors"
	"path/filepath"
	"testing"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		INSERT INTO groups (name) VALUES ('Anagrafe'), ('Tributi');
		INSERT INTO profiles (username, group_id) VALUES ('mrossi', 1), ('nogroup', NULL);
		INSERT INTO links (title, url, is_guide) VALUES
			('Zeta', 'https://zeta', 0),
			('Alfa', 'https://alfa', 0),
			('FAQ PEC', 'https://faq', 1),
			('Solo Tributi', 'https://tributi', 0);
		INSERT INTO group_links (group_id, link_id) VALUES (1, 1), (1, 2), (1, 3), (2, 4);
	`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return db
}

func TestGetDashboardForUser(t *testing.T) {
	db := newTestDB(t)

	d, err := db.GetDashboardForUser(" MROSSI ")
	if err != nil {
		t.Fatalf("GetDashboardForUser: %v", err)
	}
	if d.Group == nil || d.Group.Name != "Anagrafe" {
		t.Fatalf("gruppo atteso Anagrafe, ottenuto %+v", d.Group)
	}
	if len(d.Apps) != 2 || d.Apps[0].Title != "Alfa" || d.Apps[1].Title != "Zeta" {
		t.Fatalf("app attese [Alfa Zeta], ottenute %+v", d.Apps)
	}
	if len(d.Guides) != 1 || d.Guides[0].Title != "FAQ PEC" {
		t.Fatalf("guide attese [FAQ PEC], ottenute %+v", d.Guides)
	}
}

func TestGetDashboardForUserWithoutGroup(t *testing.T) {
	db := newTestDB(t)

	d, err := db.GetDashboardForUser("nogroup")
	if err != nil {
		t.Fatalf("GetDashboardForUser: %v", err)
	}
	if d.Group != nil || d.Profile.GroupID != nil {
		t.Fatalf("atteso profilo senza gruppo, ottenuto %+v", d)
	}
	if d.Apps == nil || d.Guides == nil || len(d.Apps)+len(d.Guides) != 0 {
		t.Fatalf("attese slice vuote non nil, ottenuto %+v", d)
	}
}

func TestGetDashboardForUserNotFound(t *testing.T) {
	db := newTestDB(t)

	if _, err := db.GetDashboardForUser("sconosciuto"); !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("atteso ErrProfileNotFound, ottenuto %v", err)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	db := newTestDB(t)

	if _, err := db.Exec(`INSERT INTO profiles (username, group_id) VALUES ('bad', 99)`); err == nil {
		t.Fatal("foreign_keys non attive: insert con group_id inesistente accettato")
	}
}
