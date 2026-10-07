package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/guidesrc"
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

func fakeFetch(body string, err error) func(*Options) {
	return func(o *Options) {
		o.GuideFetch = func(context.Context, string) (string, error) { return body, err }
	}
}

func TestGuideKindsSave(t *testing.T) {
	s, db := newTestServerWith(t, nil, fakeFetch("# Da GitHub", nil))
	c := login(t, s)
	pdf := uploadMedia(t, s, c, "pdf", pdfBytes)["name"].(string)
	for _, form := range []url.Values{
		{"kind": {"markdown"}, "title": {"Interna"}, "body": {"**ciao**"}, "url": {"https://ignorato"}, "app_id": {"0"}, "enabled": {"1"}},
		{"kind": {"pdf"}, "title": {"Manuale"}, "file": {pdf}, "app_id": {"0"}, "enabled": {"1"}},
		{"kind": {"github"}, "title": {"Da repo"}, "source_url": {"https://github.com/o/r/blob/main/a.md"}, "app_id": {"0"}, "enabled": {"1"}},
	} {
		if rec := do(t, s, "POST", "/admin/guide", form, c, hx); rec.Code != 200 {
			t.Fatalf("%s: %d\n%s", form.Get("kind"), rec.Code, rec.Body)
		}
	}
	gs, _ := db.ListGuides()
	if len(gs) != 3 || gs[0].Body != "**ciao**" || gs[0].URL != "" || gs[1].File != pdf || gs[2].Body != "# Da GitHub" || gs[2].FetchedAt == nil {
		t.Fatalf("DB: %+v", gs)
	}
	rec := do(t, s, "GET", "/admin/guide/"+itoa(gs[2].ID)+"/modifica", nil, c, hx)
	if !strings.Contains(rec.Body.String(), `value="https://github.com/o/r/blob/main/a.md"`) || !strings.Contains(rec.Body.String(), `value="github" data-guide-kind checked`) {
		t.Fatalf("modifica github non precompilata:\n%s", rec.Body)
	}
}

func TestGuideKindValidation(t *testing.T) {
	s, _ := newTestServerWith(t, nil, fakeFetch("", guidesrc.ErrNotFound))
	c := login(t, s)
	for name, form := range map[string]url.Values{
		"markdown vuota": {"kind": {"markdown"}, "title": {"X"}, "body": {" "}},
		"pdf assente":    {"kind": {"pdf"}, "title": {"X"}, "file": {"0123456789abcdef0123456789abcdef.pdf"}},
		"pdf percorso":   {"kind": {"pdf"}, "title": {"X"}, "file": {"../test.db"}},
		"github url":     {"kind": {"github"}, "title": {"X"}, "source_url": {"https://gitlab.com/o/r/blob/main/a.md"}},
		"github 404":     {"kind": {"github"}, "title": {"X"}, "source_url": {"https://github.com/o/r/blob/main/a.md"}},
		"tipo":           {"kind": {"html"}, "title": {"X"}},
	} {
		if rec := do(t, s, "POST", "/admin/guide", form, c, hx); rec.Code != 422 {
			t.Errorf("%s: atteso 422, %d", name, rec.Code)
		}
	}
}

func TestGuideRefreshNow(t *testing.T) {
	body, ferr := "# v1", error(nil)
	s, db := newTestServerWith(t, nil, func(o *Options) {
		o.GuideFetch = func(context.Context, string) (string, error) { return body, ferr }
	})
	c := login(t, s)
	do(t, s, "POST", "/admin/guide", url.Values{"kind": {"github"}, "title": {"G"}, "source_url": {"https://github.com/o/r/blob/main/a.md"}, "enabled": {"1"}}, c, hx)
	gs, _ := db.ListGuides()
	id := itoa(gs[0].ID)
	body = "# v2"
	do(t, s, "POST", "/admin/guide/"+id+"/aggiorna", nil, c, hx)
	if g, _ := db.GetGuide(gs[0].ID); g.Body != "# v2" {
		t.Fatalf("aggiorna: %q", g.Body)
	}
	ferr = errors.New("rete giù")
	rec := do(t, s, "POST", "/admin/guide/"+id+"/aggiorna", nil, c, hx)
	g, _ := db.GetGuide(gs[0].ID)
	if rec.Code != 200 || g.Body != "# v2" || g.FetchError == "" || !strings.Contains(rec.Body.String(), "Ultimo aggiornamento fallito") {
		t.Fatalf("errore: %d %+v\n%s", rec.Code, g, rec.Body)
	}
}

func TestGuidePDFReplacedRemovesOldFile(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	a := uploadMedia(t, s, c, "pdf", pdfBytes)["name"].(string)
	b := uploadMedia(t, s, c, "pdf", pdfBytes)["name"].(string)
	do(t, s, "POST", "/admin/guide", url.Values{"kind": {"pdf"}, "title": {"M"}, "file": {a}, "enabled": {"1"}}, c, hx)
	gs, _ := db.ListGuides()
	do(t, s, "POST", "/admin/guide/"+itoa(gs[0].ID), url.Values{"kind": {"pdf"}, "title": {"M"}, "file": {b}, "enabled": {"1"}}, c, hx)
	if _, err := os.Stat(filepath.Join(s.uploadDir(uploadGuide), a)); !os.IsNotExist(err) {
		t.Error("PDF vecchio non cancellato")
	}
	do(t, s, "POST", "/admin/guide/"+itoa(gs[0].ID)+"/elimina", nil, c, hx)
	if _, err := os.Stat(filepath.Join(s.uploadDir(uploadGuide), b)); !os.IsNotExist(err) {
		t.Error("PDF non cancellato con la guida")
	}
}

// Review M3: con GitHub irraggiungibile una guida già scaricata si può ancora
// modificare (titolo, visibilità…) se il link non cambia; resta l'ultima copia.
func TestGuideEditWhileGitHubDown(t *testing.T) {
	var ferr error
	s, db := newTestServerWith(t, nil, func(o *Options) {
		o.GuideFetch = func(context.Context, string) (string, error) { return "# copia", ferr }
	})
	c := login(t, s)
	src := "https://github.com/o/r/blob/main/a.md"
	do(t, s, "POST", "/admin/guide", url.Values{"kind": {"github"}, "title": {"G"}, "source_url": {src}, "enabled": {"1"}}, c, hx)
	gs, _ := db.ListGuides()
	id := itoa(gs[0].ID)
	ferr = errors.New("rete giù")
	rec := do(t, s, "POST", "/admin/guide/"+id, url.Values{"kind": {"github"}, "title": {"Nuovo titolo"}, "source_url": {src}}, c, hx)
	g, _ := db.GetGuide(gs[0].ID)
	if rec.Code != 200 || g.Title != "Nuovo titolo" || g.Body != "# copia" || g.Enabled || g.FetchError == "" {
		t.Fatalf("modifica con GitHub giù: %d %+v", rec.Code, g)
	}
	// Link nuovo: il download serve, quindi l'errore blocca il salvataggio.
	rec = do(t, s, "POST", "/admin/guide/"+id, url.Values{"kind": {"github"}, "title": {"X"}, "source_url": {"https://github.com/o/r/blob/main/b.md"}}, c, hx)
	if rec.Code != 422 {
		t.Fatalf("link nuovo con GitHub giù: atteso 422, %d", rec.Code)
	}
}
