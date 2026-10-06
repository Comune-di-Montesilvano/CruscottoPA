package database

import (
	"testing"
	"time"
)

func TestGetDashboard(t *testing.T) {
	db := newTestDB(t)
	apps, _ := db.ListApps()
	rubrica, webmail := apps[0], apps[1]

	// Solo Rubrica ha un URL; Webmail resta "da completare".
	rubrica.URL = "https://rubrica.local"
	db.UpdateApp(rubrica)

	ext, _ := db.CreateCategory("Gestionali esterni")
	off, _ := db.CreateApp(App{CategoryID: ext, Title: "Spenta", URL: "https://x", Enabled: false})
	db.CreateCategory("Vuota")

	db.CreateGuide(Guide{AppID: ptr(rubrica.ID), Title: "Cercare un interno", Kind: GuideKindLink, URL: "https://g/1", Enabled: true})
	db.CreateGuide(Guide{AppID: ptr(rubrica.ID), Title: "Disabilitata", Kind: GuideKindLink, URL: "https://g/2", Enabled: false})
	db.CreateGuide(Guide{AppID: ptr(webmail.ID), Title: "Di app senza URL", Kind: GuideKindLink, URL: "https://g/3", Enabled: true})
	db.CreateGuide(Guide{AppID: ptr(off), Title: "Di app spenta", Kind: GuideKindLink, URL: "https://g/4", Enabled: true})
	db.CreateGuide(Guide{Title: "VPN", Kind: GuideKindLink, URL: "https://g/5", Enabled: true})
	mkAlert(t, db, "attivo", LevelNews, now.Add(-time.Hour), nil)

	d, err := db.GetDashboard(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Alerts) != 1 {
		t.Fatalf("avvisi: %+v", d.Alerts)
	}
	if len(d.Categories) != 1 || d.Categories[0].Name != "Applicativi" {
		t.Fatalf("categorie visibili: solo quelle con app visibili, ottenuto %+v", d.Categories)
	}
	cat := d.Categories[0]
	if len(cat.Apps) != 1 || cat.Apps[0].Title != "Rubrica" {
		t.Fatalf("app visibili: solo abilitate con URL, ottenuto %+v", cat.Apps)
	}
	if g := cat.Apps[0].Guides; len(g) != 1 || g[0].Title != "Cercare un interno" {
		t.Fatalf("guide di Rubrica: %+v", g)
	}
	if len(d.GeneralGuides) != 1 || d.GeneralGuides[0].Title != "VPN" {
		t.Fatalf("guide generali: le guide di app nascoste non devono comparire, ottenuto %+v", d.GeneralGuides)
	}
}

func TestGetDashboardFreshInstall(t *testing.T) {
	db := newTestDB(t)
	d, err := db.GetDashboard(now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Categories == nil || d.Alerts == nil || d.GeneralGuides == nil || len(d.Categories) != 0 {
		t.Fatalf("installazione nuova: attese slice vuote non nil, ottenuto %+v", d)
	}
}
