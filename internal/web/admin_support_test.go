package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestSupportAdmin(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	apps, _ := db.ListApps()
	a, b := itoa(apps[0].ID), itoa(apps[1].ID)

	page := do(t, s, "GET", "/admin/assistenza", nil, c, nil).Body.String()
	if !strings.Contains(page, `name="app" value="`+a+`"`) || !strings.Contains(page, "Applicativi") {
		t.Fatalf("form con le caselle degli applicativi:\n%s", page)
	}
	rec := do(t, s, "POST", "/admin/assistenza", url.Values{"title": {"Portale Maggioli"}, "url": {"https://assistenza.example"},
		"note": {"serve l'utenza"}, "enabled": {"1"}, "app": {a, b}}, c, hx)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Portale Maggioli") {
		t.Fatalf("crea: %d\n%s", rec.Code, rec.Body)
	}
	all, _ := db.ListSupportChannels()
	if len(all) != 1 || len(all[0].AppIDs) != 2 || !all[0].Enabled {
		t.Fatalf("salvato: %+v", all)
	}
	id := itoa(all[0].ID)
	for _, bad := range []url.Values{
		{"title": {""}, "url": {"https://x"}},
		{"title": {"X"}, "url": {"javascript:alert(1)"}},
		{"title": {"X"}, "url": {"https://x"}, "app": {"999999"}},
	} {
		if rec := do(t, s, "POST", "/admin/assistenza/"+id, bad, c, hx); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%v: atteso 422, %d", bad, rec.Code)
		}
	}
	if body := do(t, s, "GET", "/admin/assistenza/"+id+"/modifica", nil, c, hx).Body.String(); !strings.Contains(body, `value="`+a+`" checked`) {
		t.Fatalf("modifica: caselle spuntate\n%s", body)
	}
	// Senza applicativi il canale resta, con l'avviso.
	do(t, s, "POST", "/admin/assistenza/"+id, url.Values{"title": {"Portale Maggioli"}, "url": {"https://assistenza.example"}}, c, hx)
	if body := do(t, s, "GET", "/admin/assistenza", nil, c, nil).Body.String(); !strings.Contains(body, "Non collegato a nessun applicativo") {
		t.Fatal("manca l'avviso per il canale senza applicativi")
	}
	if rec := do(t, s, "POST", "/admin/assistenza/"+id+"/elimina", nil, c, hx); rec.Code != http.StatusOK {
		t.Fatalf("elimina: %d", rec.Code)
	}
	if rec := do(t, s, "GET", "/admin/assistenza/999/modifica", nil, c, hx); rec.Code != http.StatusNotFound {
		t.Fatalf("inesistente: %d", rec.Code)
	}
	body := do(t, s, "GET", "/admin", nil, c, nil).Body.String()
	if !strings.Contains(body, `href="/admin/assistenza"`) || !strings.Contains(body, "canali di assistenza") {
		t.Fatal("voce Assistenza nel menu e conteggio in panoramica")
	}
}
