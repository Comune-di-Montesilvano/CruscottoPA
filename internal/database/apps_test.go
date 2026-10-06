package database

import (
	"errors"
	"testing"
)

func TestAppCRUD(t *testing.T) {
	db := newTestDB(t)
	cats, _ := db.ListCategories()
	cat := cats[0].ID

	id, err := db.CreateApp(App{
		CategoryID: cat, Title: "Sicraweb", URL: "https://sicraweb.local",
		IconKind: IconPack, IconValue: "description", IconColor: "#475569", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.GetApp(id)
	if err != nil || a.Title != "Sicraweb" || !a.Enabled || a.SortOrder != 2 {
		t.Fatalf("GetApp: %+v %v (sort_order atteso 2, dopo le 2 app del seed)", a, err)
	}

	a.Title, a.Enabled = "Sicraweb EVO", false
	if err := db.UpdateApp(a); err != nil {
		t.Fatal(err)
	}
	a, _ = db.GetApp(id)
	if a.Title != "Sicraweb EVO" || a.Enabled {
		t.Fatalf("UpdateApp: %+v", a)
	}

	if err := db.DeleteApp(id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetApp(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dopo delete: %v", err)
	}
}

func TestUpdateAppCategoryMovesToEnd(t *testing.T) {
	db := newTestDB(t)
	other, _ := db.CreateCategory("Gestionali esterni")
	tinn, _ := db.CreateApp(App{CategoryID: other, Title: "TINN", Enabled: true})

	apps, _ := db.ListApps()
	rubrica := apps[0] // seed, categoria "Applicativi"
	rubrica.CategoryID = other
	if err := db.UpdateApp(rubrica); err != nil {
		t.Fatal(err)
	}
	r, _ := db.GetApp(rubrica.ID)
	t2, _ := db.GetApp(tinn)
	if r.SortOrder <= t2.SortOrder {
		t.Fatalf("cambio categoria: attesa in coda (sort %d > %d)", r.SortOrder, t2.SortOrder)
	}
}

func TestMoveAppStaysInCategory(t *testing.T) {
	db := newTestDB(t)
	other, _ := db.CreateCategory("Altro")
	db.CreateApp(App{CategoryID: other, Title: "Zeta", Enabled: true})

	apps, _ := db.ListApps()
	webmail := apps[1]
	if err := db.MoveApp(webmail.ID, -1); err != nil {
		t.Fatal(err)
	}
	apps, _ = db.ListApps()
	if apps[0].Title != "Webmail" || apps[1].Title != "Rubrica" || apps[2].Title != "Zeta" {
		t.Fatalf("ordine dopo move: %s %s %s", apps[0].Title, apps[1].Title, apps[2].Title)
	}
	// In fondo alla propria categoria: non scavalca in un'altra categoria.
	if err := db.MoveApp(apps[1].ID, 1); err != nil {
		t.Fatal(err)
	}
	apps, _ = db.ListApps()
	if apps[2].Title != "Zeta" {
		t.Fatalf("move ha attraversato la categoria: %s", apps[2].Title)
	}
}
