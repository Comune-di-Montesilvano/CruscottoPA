package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestGuidesCRUD(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	apps, _ := db.ListApps()
	webmail := apps[1]

	rec := do(t, s, "POST", "/admin/guide", url.Values{
		"title": {"VPN da casa"}, "url": {"https://wiki.local/vpn"}, "app_id": {"0"}, "enabled": {"1"},
	}, c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "VPN da casa") || !strings.Contains(rec.Body.String(), "Generale") {
		t.Fatalf("crea generale: %d\n%s", rec.Code, rec.Body)
	}
	rec = do(t, s, "POST", "/admin/guide", url.Values{
		"title": {"Firma email"}, "url": {"https://wiki.local/firma"}, "app_id": {itoa(webmail.ID)}, "enabled": {"1"},
	}, c, hx)
	if rec.Code != 200 {
		t.Fatalf("crea agganciata: %d", rec.Code)
	}

	gs, _ := db.ListGuides()
	if len(gs) != 2 || gs[0].AppID != nil || gs[1].AppID == nil || *gs[1].AppID != webmail.ID || gs[1].Kind != "link" {
		t.Fatalf("DB: %+v", gs)
	}

	firma := gs[1].ID
	rec = do(t, s, "GET", "/admin/guide/"+itoa(firma)+"/modifica", nil, c, hx)
	if !strings.Contains(rec.Body.String(), `value="Firma email"`) {
		t.Fatal("modifica: form non precompilato")
	}
	do(t, s, "POST", "/admin/guide/"+itoa(firma), url.Values{
		"title": {"Firma email"}, "url": {"https://wiki.local/firma"}, "app_id": {"0"},
	}, c, hx)
	g, _ := db.GetGuide(firma)
	if g.AppID != nil || g.Enabled {
		t.Fatalf("update: attesa generale e nascosta, ottenuto %+v", g)
	}

	if rec := do(t, s, "POST", "/admin/guide/"+itoa(firma)+"/sposta", url.Values{"dir": {"up"}}, c, hx); rec.Code != 200 {
		t.Fatalf("sposta: %d", rec.Code)
	}
	if rec := do(t, s, "POST", "/admin/guide/"+itoa(firma)+"/elimina", nil, c, hx); rec.Code != 200 {
		t.Fatalf("elimina: %d", rec.Code)
	}
	if _, err := db.GetGuide(firma); err == nil {
		t.Fatal("guida non eliminata")
	}
}

func TestGuideValidation(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	rec := do(t, s, "POST", "/admin/guide", url.Values{"title": {""}, "url": {"wiki/vpn"}, "app_id": {"999"}}, c, hx)
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("atteso 422, ottenuto %d", rec.Code)
	}
	for _, want := range []string{"Campo obbligatorio.", "Inserisci l&#39;indirizzo completo", "Applicativo non trovato."} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	rec = do(t, s, "POST", "/admin/guide", url.Values{"title": {"x"}, "url": {""}, "app_id": {"0"}}, c, hx)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatal("per le guide l'URL è obbligatorio")
	}
}
