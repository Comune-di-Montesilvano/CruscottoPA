package database

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSupportChannels(t *testing.T) {
	db := newTestDB(t)
	apps, _ := db.ListApps() // seed: Rubrica, Webmail
	rub, mail := apps[0].ID, apps[1].ID
	id, err := db.CreateSupportChannel(SupportChannel{Title: "Portale Maggioli", URL: "https://assistenza.example", Note: "serve l'utenza", Enabled: true, AppIDs: []int64{rub, mail}})
	if err != nil {
		t.Fatal(err)
	}
	ced, _ := db.CreateSupportChannel(SupportChannel{Title: "CED", URL: "https://ced.example", Enabled: true, AppIDs: []int64{rub}})
	c, err := db.GetSupportChannel(id)
	if err != nil || c.Title != "Portale Maggioli" || !reflect.DeepEqual(c.AppIDs, []int64{rub, mail}) {
		t.Fatalf("Get: %+v %v", c, err)
	}
	c.AppIDs = []int64{mail}
	c.Note = ""
	if err := db.UpdateSupportChannel(c); err != nil {
		t.Fatal(err)
	}
	by, _ := db.SupportByApp()
	if len(by[rub]) != 1 || by[rub][0].ID != ced || len(by[mail]) != 1 || by[mail][0].ID != id {
		t.Fatalf("SupportByApp: %+v", by)
	}
	if err := db.MoveSupportChannel(ced, -1); err != nil {
		t.Fatal(err)
	}
	all, _ := db.ListSupportChannels()
	if len(all) != 2 || all[0].ID != ced {
		t.Fatalf("ordine: %+v", all)
	}
	// App eliminata: collegamento tolto, canale resta.
	if err := db.DeleteApp(mail); err != nil {
		t.Fatal(err)
	}
	c, _ = db.GetSupportChannel(id)
	if len(c.AppIDs) != 0 {
		t.Fatalf("collegamento all'app eliminata: %+v", c.AppIDs)
	}
	if err := db.DeleteSupportChannel(id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetSupportChannel(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dopo Delete: %v", err)
	}
	if err := db.UpdateSupportChannel(SupportChannel{ID: 999, Title: "x", URL: "https://x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update inesistente: %v", err)
	}
}

func TestDashboardSupport(t *testing.T) {
	db := newTestDB(t)
	apps, _ := db.ListApps()
	a := apps[0]
	a.URL = "https://rubrica.local"
	db.UpdateApp(a)
	db.CreateSupportChannel(SupportChannel{Title: "Attivo", URL: "https://a", Enabled: true, AppIDs: []int64{a.ID}})
	db.CreateSupportChannel(SupportChannel{Title: "Spento", URL: "https://b", Enabled: false, AppIDs: []int64{a.ID}})
	d, err := db.GetDashboard(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := d.Categories[0].Apps[0].Support
	if len(got) != 1 || got[0].Title != "Attivo" {
		t.Fatalf("canali sulla tile: %+v", got)
	}
}

func TestMigrationV8OnPopulatedV7(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v7.db")
	all := migrations
	t.Cleanup(func() { migrations = all })
	migrations = all[:7]
	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := old.SchemaVersion(); v != 7 {
		t.Fatalf("versione di partenza: %d", v)
	}
	if _, err := old.Exec(`INSERT INTO apps (category_id, title, url) VALUES (1, 'PagoPA', 'https://pagopa.local')`); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO audience_attributes (name, label) VALUES ('mail', 'Email')`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	migrations = all
	db, err := Open(path)
	if err != nil {
		t.Fatalf("migrazione v8: %v", err)
	}
	defer db.Close()
	if v, _ := db.SchemaVersion(); v != len(all) {
		t.Fatalf("versione dopo: %d", v)
	}
	apps, _ := db.ListApps()
	var pagopa *App
	for i := range apps {
		if apps[i].Title == "PagoPA" {
			pagopa = &apps[i]
		}
	}
	if pagopa == nil || pagopa.IconBg != "" {
		t.Fatalf("app esistente dopo la v8: %+v", pagopa)
	}
	attrs, _ := db.ListAudienceAttributes()
	if len(attrs) != 1 || attrs[0].Hero != 0 || attrs[0].HeroKind != HeroText {
		t.Fatalf("attributo esistente dopo la v8: %+v", attrs)
	}
	if _, err := db.CreateSupportChannel(SupportChannel{Title: "X", URL: "https://x", Enabled: true, AppIDs: []int64{pagopa.ID}}); err != nil {
		t.Fatalf("tabelle dell'assistenza: %v", err)
	}
}
