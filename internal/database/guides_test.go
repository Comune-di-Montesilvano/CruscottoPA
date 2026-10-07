package database

import (
	"errors"
	"testing"
	"time"
)

func ptr(v int64) *int64 { return &v }

func TestGuideCRUDAndOrdering(t *testing.T) {
	db := newTestDB(t)
	apps, _ := db.ListApps()
	webmail := apps[1].ID

	gen, err := db.CreateGuide(Guide{Title: "VPN da casa", Kind: GuideKindLink, URL: "https://wiki/vpn", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	firma, _ := db.CreateGuide(Guide{AppID: ptr(webmail), Title: "Firma email", Kind: GuideKindLink, URL: "https://wiki/firma", Enabled: true})
	db.CreateGuide(Guide{AppID: ptr(webmail), Title: "Archiviazione", Kind: GuideKindLink, URL: "https://wiki/arch", Enabled: true})

	gs, _ := db.ListGuides()
	if len(gs) != 3 || gs[0].ID != gen || gs[0].AppID != nil {
		t.Fatalf("ListGuides: le generali vanno per prime, ottenuto %+v", gs)
	}

	g, _ := db.GetGuide(firma)
	g.Title = "Firma email aziendale"
	if err := db.UpdateGuide(g); err != nil {
		t.Fatal(err)
	}
	if err := db.MoveGuide(firma, 1); err != nil {
		t.Fatal(err)
	}
	gs, _ = db.ListGuides()
	if gs[2].Title != "Firma email aziendale" {
		t.Fatalf("MoveGuide entro l'app: ottenuto ordine %s, %s", gs[1].Title, gs[2].Title)
	}

	counts, _ := db.GuideCountsByApp()
	if counts[webmail] != 2 || len(counts) != 1 {
		t.Fatalf("GuideCountsByApp: %v", counts)
	}

	if err := db.DeleteGuide(gen); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetGuide(gen); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dopo delete: %v", err)
	}
}

func TestDeleteAppMakesGuidesGeneral(t *testing.T) {
	db := newTestDB(t)
	apps, _ := db.ListApps()
	id, _ := db.CreateGuide(Guide{AppID: ptr(apps[0].ID), Title: "Cercare un interno", Kind: GuideKindLink, URL: "https://wiki/x", Enabled: true})

	if err := db.DeleteApp(apps[0].ID); err != nil {
		t.Fatal(err)
	}
	g, _ := db.GetGuide(id)
	if g.AppID != nil {
		t.Fatalf("ON DELETE SET NULL: attesa guida generale, app_id=%v", *g.AppID)
	}
}

func TestMigrationV7GuideColumns(t *testing.T) {
	db := newTestDB(t)
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	id, err := db.CreateGuide(Guide{Title: "Manuale", Kind: GuideKindGitHub, SourceURL: "https://github.com/o/r/blob/main/a.md", Body: "# A", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetGuideFetched(id, "# B", at); err != nil {
		t.Fatal(err)
	}
	g, _ := db.GetGuide(id)
	if g.Body != "# B" || g.FetchedAt == nil || !g.FetchedAt.Equal(at) || g.FetchError != "" || g.SourceURL == "" {
		t.Fatalf("dopo fetch: %+v", g)
	}
	if err := db.SetGuideFetchError(id, "404"); err != nil {
		t.Fatal(err)
	}
	g, _ = db.GetGuide(id)
	if g.Body != "# B" || g.FetchError != "404" {
		t.Fatalf("errore deve lasciare il body: %+v", g)
	}
	todo, _ := db.GuidesToRefresh(at.Add(time.Hour))
	if len(todo) != 1 || todo[0].ID != id {
		t.Fatalf("da aggiornare: %+v", todo)
	}
	if todo, _ = db.GuidesToRefresh(at); len(todo) != 0 {
		t.Fatalf("appena aggiornata: %+v", todo)
	}
	g.File, g.Title = "x.pdf", "Manuale 2"
	if err := db.UpdateGuide(g); err != nil {
		t.Fatal(err)
	}
	if g2, _ := db.GetGuide(id); g2.File != "x.pdf" || g2.FetchedAt == nil {
		t.Fatalf("update: %+v", g2)
	}
}

func TestGetPlanciaGuide(t *testing.T) {
	db := newTestDB(t)
	apps, _ := db.ListApps() // seed: Rubrica e Webmail senza URL → non visibili
	hidden := apps[0].ID
	gen, _ := db.CreateGuide(Guide{Title: "G", Kind: GuideKindMarkdown, Body: "x", Enabled: true})
	off, _ := db.CreateGuide(Guide{Title: "Off", Kind: GuideKindMarkdown, Body: "x"})
	ofHidden, _ := db.CreateGuide(Guide{AppID: &hidden, Title: "H", Kind: GuideKindMarkdown, Body: "x", Enabled: true})
	if _, err := db.GetPlanciaGuide(gen); err != nil {
		t.Fatalf("generale abilitata: %v", err)
	}
	for _, id := range []int64{off, ofHidden, 9999} {
		if _, err := db.GetPlanciaGuide(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("guida %d: atteso ErrNotFound, %v", id, err)
		}
	}
}

func TestValidGuideKind(t *testing.T) {
	for _, k := range []string{"link", "markdown", "pdf", "github"} {
		if !ValidGuideKind(k) {
			t.Error(k)
		}
	}
	if ValidGuideKind("html") || ValidGuideKind("") {
		t.Error("tipo non valido accettato")
	}
}
