package database

import (
	"path/filepath"
	"reflect"
	"testing"
)

func layoutApp(t *testing.T, db *DB, cat int64, title string) int64 {
	t.Helper()
	id, err := db.CreateApp(App{CategoryID: cat, Title: title, URL: "https://" + title + ".example.it",
		IconKind: IconPack, IconValue: "apps", IconColor: "#475569", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func layoutCats(t *testing.T, db *DB) (c1, c2 int64) {
	t.Helper()
	cats, _ := db.ListCategories()
	c1 = cats[0].ID
	c2, err := db.CreateCategory("Altri")
	if err != nil {
		t.Fatal(err)
	}
	return c1, c2
}

func TestAppOpens(t *testing.T) {
	db := newTestDB(t)
	c1, _ := layoutCats(t, db)
	a := layoutApp(t, db, c1, "protocollo")
	db.AddAppOpen("MRossi", a, "2026-10-06")
	if err := db.AddAppOpen("mrossi", a, "2026-10-06"); err != nil {
		t.Fatal(err)
	}
	if m, _ := db.AppOpens("mrossi", "2000-01-01"); m[a] != 2 {
		t.Fatalf("aperture: %v", m)
	}
	if err := db.AddAppOpen("mrossi", 9999, "2026-10-06"); err == nil {
		t.Fatal("app inesistente accettata")
	}
}

// Le aperture contano da fromDay; la pulizia toglie quelle prima di un giorno.
func TestAppOpensWindowAndCleanup(t *testing.T) {
	db := newTestDB(t)
	c1, _ := layoutCats(t, db)
	a := layoutApp(t, db, c1, "protocollo")
	db.AddAppOpen("mrossi", a, "2026-07-01")
	db.AddAppOpen("mrossi", a, "2026-10-05")
	db.AddAppOpen("mrossi", a, "2026-10-06")
	if m, _ := db.AppOpens("mrossi", "2026-10-05"); m[a] != 2 {
		t.Fatalf("finestra: %v", m)
	}
	if err := db.CleanupAppOpens("2026-10-06"); err != nil {
		t.Fatal(err)
	}
	if m, _ := db.AppOpens("mrossi", "2000-01-01"); m[a] != 1 {
		t.Fatalf("dopo la pulizia: %v", m)
	}
}

// Migrazione v16: le aperture della v15 restano, nel giorno della migrazione.
func TestMigrationV16KeepsOpens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v15.db")
	all := migrations
	t.Cleanup(func() { migrations = all })
	migrations = all[:15]
	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c1, _ := layoutCats(t, old)
	a := layoutApp(t, old, c1, "protocollo")
	if _, err := old.Exec(`INSERT INTO user_app_opens (username, app_id, opens) VALUES ('mrossi', ?, 7)`, a); err != nil {
		t.Fatal(err)
	}
	old.Close()
	migrations = all
	db, err := Open(path)
	if err != nil {
		t.Fatalf("migrazione v16: %v", err)
	}
	defer db.Close()
	if m, _ := db.AppOpens("mrossi", "2000-01-01"); m[a] != 7 {
		t.Fatalf("aperture dopo la v16: %v", m)
	}
	if err := db.DeleteApp(a); err != nil {
		t.Fatal(err)
	}
	if m, _ := db.AppOpens("mrossi", "2000-01-01"); len(m) != 0 {
		t.Fatalf("cascade dopo la v16: %v", m)
	}
}

func TestUserLayoutRoundTrip(t *testing.T) {
	db := newTestDB(t)
	c1, c2 := layoutCats(t, db)
	a1, a2 := layoutApp(t, db, c1, "a1"), layoutApp(t, db, c1, "a2")
	if err := db.SaveFavorites("mrossi", []int64{a2, a1}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveCategoryOrder("mrossi", []int64{c2, c1}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAppOrder("mrossi", c1, []int64{a2, a1}); err != nil {
		t.Fatal(err)
	}
	l, err := db.UserLayout("MROSSI")
	if err != nil || !reflect.DeepEqual(l.Favorites, []int64{a2, a1}) {
		t.Fatalf("preferiti: %+v %v", l, err)
	}
	if l.Categories[c2] != 0 || l.Categories[c1] != 1 || l.Apps[a2] != 0 || l.Apps[a1] != 1 {
		t.Fatalf("ordini: %+v", l)
	}
	db.SaveFavorites("mrossi", []int64{a1})
	if l, _ = db.UserLayout("mrossi"); !reflect.DeepEqual(l.Favorites, []int64{a1}) {
		t.Fatalf("sostituzione: %v", l.Favorites)
	}
	if l, _ = db.UserLayout("anna"); l.Favorites == nil || l.Categories == nil || l.Apps == nil || len(l.Favorites) != 0 {
		t.Fatalf("utente senza layout: %+v", l)
	}
}

func TestSaveAppOrderOnlyThatCategory(t *testing.T) {
	db := newTestDB(t)
	c1, c2 := layoutCats(t, db)
	a1, b1 := layoutApp(t, db, c1, "a1"), layoutApp(t, db, c2, "b1")
	db.SaveAppOrder("mrossi", c2, []int64{b1})
	db.SaveAppOrder("mrossi", c1, []int64{a1})
	if l, _ := db.UserLayout("mrossi"); len(l.Apps) != 2 {
		t.Fatalf("ordine dell'altra categoria perso: %v", l.Apps)
	}
}

func TestResetLayoutKeepsOpens(t *testing.T) {
	db := newTestDB(t)
	c1, _ := layoutCats(t, db)
	a := layoutApp(t, db, c1, "a")
	db.AddAppOpen("mrossi", a, "2026-10-06")
	db.SaveFavorites("mrossi", []int64{a})
	db.SaveCategoryOrder("mrossi", []int64{c1})
	db.SaveAppOrder("mrossi", c1, []int64{a})
	if err := db.ResetLayout("mrossi"); err != nil {
		t.Fatal(err)
	}
	l, _ := db.UserLayout("mrossi")
	if len(l.Favorites)+len(l.Categories)+len(l.Apps) != 0 {
		t.Fatalf("layout rimasto: %+v", l)
	}
	if m, _ := db.AppOpens("mrossi", "2000-01-01"); m[a] != 1 {
		t.Fatal("aperture cancellate dal ripristino")
	}
	if err := db.DeleteUserLayout("mrossi"); err != nil {
		t.Fatal(err)
	}
	if m, _ := db.AppOpens("mrossi", "2000-01-01"); len(m) != 0 {
		t.Fatal("aperture rimaste")
	}
}

func TestLayoutCascade(t *testing.T) {
	db := newTestDB(t)
	c1, _ := layoutCats(t, db)
	a := layoutApp(t, db, c1, "a")
	db.AddAppOpen("mrossi", a, "2026-10-06")
	db.SaveFavorites("mrossi", []int64{a})
	db.SaveAppOrder("mrossi", c1, []int64{a})
	if err := db.DeleteApp(a); err != nil {
		t.Fatal(err)
	}
	l, _ := db.UserLayout("mrossi")
	m, _ := db.AppOpens("mrossi", "2000-01-01")
	if len(l.Favorites)+len(l.Apps)+len(m) != 0 {
		t.Fatalf("righe rimaste: %+v %v", l, m)
	}
}

func TestUpdateAppCategoryClearsUserOrder(t *testing.T) {
	db := newTestDB(t)
	c1, c2 := layoutCats(t, db)
	a := layoutApp(t, db, c1, "a")
	db.SaveAppOrder("mrossi", c1, []int64{a})
	app, _ := db.GetApp(a)
	app.Title = "a2"
	if err := db.UpdateApp(app); err != nil {
		t.Fatal(err)
	}
	if l, _ := db.UserLayout("mrossi"); len(l.Apps) != 1 {
		t.Fatal("riga tolta senza cambio di categoria")
	}
	app.CategoryID = c2
	db.UpdateApp(app)
	if l, _ := db.UserLayout("mrossi"); len(l.Apps) != 0 {
		t.Fatal("riga rimasta dopo il cambio di categoria")
	}
}

// Un aggiornamento fallito (categoria inesistente) non tocca l'ordine personale.
func TestUpdateAppFailureKeepsUserOrder(t *testing.T) {
	db := newTestDB(t)
	cats, _ := db.ListCategories()
	id, _ := db.CreateApp(App{CategoryID: cats[0].ID, Title: "a", URL: "https://a.it", IconKind: IconPack, IconValue: "apps", Enabled: true})
	db.SaveAppOrder("mrossi", cats[0].ID, []int64{id})
	app, _ := db.GetApp(id)
	app.CategoryID = 99999
	if err := db.UpdateApp(app); err == nil {
		t.Fatal("categoria inesistente accettata")
	}
	if l, _ := db.UserLayout("mrossi"); len(l.Apps) != 1 {
		t.Fatalf("ordine perso: %v", l.Apps)
	}
}
