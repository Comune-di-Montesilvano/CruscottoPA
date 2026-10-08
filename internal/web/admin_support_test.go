package web

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
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

func TestAppFormShowsSupport(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	apps, _ := db.ListApps()
	db.CreateSupportChannel(database.SupportChannel{Title: "Portale Maggioli", URL: "https://x", Enabled: true, AppIDs: []int64{apps[0].ID}})
	body := do(t, s, "GET", "/admin/app/"+itoa(apps[0].ID)+"/modifica", nil, c, hx).Body.String()
	if !strings.Contains(body, "Assistenza:") || !strings.Contains(body, "Portale Maggioli") || !strings.Contains(body, `href="/admin/assistenza"`) {
		t.Fatalf("scheda app senza assistenza:\n%s", body)
	}
}

// La scheda dell'app elenca anche i canali disattivati, segnalandoli.
func TestAppFormShowsDisabledSupport(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	apps, _ := db.ListApps()
	db.CreateSupportChannel(database.SupportChannel{Title: "Vecchio portale", URL: "https://x", Enabled: false, AppIDs: []int64{apps[0].ID}})
	body := do(t, s, "GET", "/admin/app/"+itoa(apps[0].ID)+"/modifica", nil, c, hx).Body.String()
	if !strings.Contains(body, "Vecchio portale (disattivato)") {
		t.Fatalf("canale disattivato nella scheda:\n%s", body)
	}
}

func TestIconBgAdmin(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	apps, _ := db.ListApps()
	a := apps[0]
	a.IconKind, a.IconValue, a.IconBg = "url", "https://logo.example/pagopa.svg", "#0066cc"
	db.UpdateApp(a)
	page := do(t, s, "GET", "/admin/app", nil, c, nil).Body.String()
	if !strings.Contains(page, `src="https://logo.example/pagopa.svg" alt="" style="background:#0066cc"`) {
		t.Errorf("icona da URL nell'elenco admin senza sfondo:\n%s", page)
	}
	js, _ := os.ReadFile("../../web/static/js/admin.js")
	if !strings.Contains(string(js), `[name="icon_bg_on"]`) {
		t.Error("admin.js: scegliere un colore deve spuntare «Riquadro dell'icona colorato»")
	}
	gruppi := do(t, s, "GET", "/admin/gruppi", nil, c, nil).Body.String()
	if !strings.Contains(gruppi, `<td colspan="4" class="muted">Nessun attributo`) {
		t.Error("riga «Nessun attributo» senza colspan")
	}
}
