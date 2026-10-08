package web

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

func searchIndexOf(t *testing.T, body string) []searchItem {
	t.Helper()
	const open = `<script type="application/json" id="search-index">`
	i := strings.Index(body, open)
	if i < 0 {
		t.Fatal("indice di ricerca assente")
	}
	raw := body[i+len(open):]
	raw = raw[:strings.Index(raw, "</script>")]
	var items []searchItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		t.Fatalf("JSON non valido: %v\n%s", err, raw)
	}
	return items
}

func TestSearchIndex(t *testing.T) {
	s, db := newTestServer(t, nil)
	apps, _ := db.ListApps()
	a := apps[0]
	a.URL = "https://rubrica.local"
	a.Title = `Rubrica </script><b>"x"`
	db.UpdateApp(a)
	db.CreateGuide(database.Guide{AppID: &a.ID, Title: "Cercare un interno", Kind: "link", URL: "https://wiki/1", Enabled: true})
	db.CreateGuide(database.Guide{Title: "VPN da casa", Kind: "markdown", Body: "x", Enabled: true})
	db.CreateSupportChannel(database.SupportChannel{Title: "Portale Maggioli", URL: "https://assistenza.example", Note: "utenza", Enabled: true, AppIDs: []int64{a.ID}})

	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	// Un titolo con </script> non deve chiudere il blocco.
	if strings.Contains(body, `Rubrica </script>`) {
		t.Fatal("</script> non escapato nell'indice")
	}
	items := searchIndexOf(t, body)
	byKey := map[string]searchItem{}
	for _, it := range items {
		byKey[it.Kind+":"+it.Title] = it
	}
	app, ok := byKey["app:"+a.Title]
	if !ok || app.URL != "https://rubrica.local" || !app.NewTab || app.Sub == "" {
		t.Fatalf("app nell'indice: %+v", items)
	}
	if g := byKey["guide:Cercare un interno"]; g.Sub != a.Title || g.URL != "https://wiki/1" {
		t.Errorf("guida dell'app: %+v", g)
	}
	if g := byKey["guide:VPN da casa"]; g.Sub != "Guida generale" || g.NewTab || !strings.HasPrefix(g.URL, "/guide/") {
		t.Errorf("guida generale: %+v", g)
	}
	sup := byKey["support:Portale Maggioli"]
	if sup.URL != "https://assistenza.example" || !strings.Contains(sup.Sub, a.Title) || sup.Text != "utenza" {
		t.Errorf("canale: %+v", sup)
	}
}

// Contenuti nascosti dai gruppi fuori dall'indice.
func TestSearchIndexHidesFiltered(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedVisibility(t, db) // Webmail e "Guida non per Tributi" non visibili a un anonimo
	anon := searchIndexOf(t, do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Anonymous: true}), nil).Body.String())
	seen := false
	for _, it := range anon {
		if it.Title == "Webmail" || it.Title == "Guida non per Tributi" {
			t.Errorf("nascosto nell'indice: %+v", it)
		}
		seen = seen || it.Title == "Rubrica"
	}
	if !seen {
		t.Errorf("app pubblica assente dall'indice: %+v", anon)
	}
}

// Un URL non http/https (es. javascript:) non entra nell'indice: il JS lo
// userebbe come href.
func TestSearchIndexSkipsUnsafeURLs(t *testing.T) {
	g := database.Guide{ID: 1, Title: "Guida", Kind: database.GuideKindLink, URL: "javascript:alert(1)"}
	d := database.Dashboard{
		Categories: []database.CategoryWithApps{{Category: database.Category{Name: "C"}, Apps: []database.AppWithGuides{{
			App:     database.App{Title: "App", URL: "javascript:alert(1)"},
			Guides:  []database.Guide{g},
			Support: []database.SupportChannel{{ID: 1, Title: "Canale", URL: "vbscript:x"}},
		}}}},
		GeneralGuides: []database.Guide{{ID: 2, Title: "Interna", Kind: database.GuideKindMarkdown}, {ID: 3, Title: "Doppia barra", Kind: database.GuideKindLink, URL: "//evil.example"}},
	}
	items := buildSearchIndex(d)
	if len(items) != 1 || items[0].Title != "Interna" {
		t.Fatalf("indice con URL non sicuri: %+v", items)
	}
}
