package database

import (
	"errors"
	"testing"
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
