package web

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// Le guide di un applicativo si aprono dentro la tile (che si allunga), non
// in una scheda sovrapposta.
func TestTileExpandsInPlace(t *testing.T) {
	s, db := newTestServer(t, nil)
	apps, _ := db.ListApps()
	app := apps[0]
	app.URL = "https://rubrica.local"
	db.UpdateApp(app)
	db.CreateGuide(database.Guide{AppID: &app.ID, Title: "Come cercare", Kind: "link", URL: "https://guide.local/1", Enabled: true})
	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	if strings.Contains(body, "tile-fly") || strings.Contains(body, "fly-open") {
		t.Fatal("la scheda sovrapposta non deve esserci più")
	}
	for _, want := range []string{`class="tile-more" id="guide-app-` + itoa(app.ID) + `"`, `aria-controls="guide-app-` + itoa(app.ID) + `"`, "Come cercare"} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	css, _ := os.ReadFile("../../web/static/css/plancia.css")
	if strings.Contains(string(css), ".tile:hover .tile-more") || !strings.Contains(string(css), ".tile.open .tile-more { display: block; }") {
		t.Error("niente :hover in CSS: l'apertura col mouse la gestisce dashboard.js con il ritardo")
	}
	js, _ := os.ReadFile("../../web/static/js/dashboard.js")
	for _, want := range []string{"HOVER_OPEN_MS = 300", "NEWS_HOVER_MS = 500", "clearTimeout(newsTimer);", "clearTimeout(hoverTimer);", `"pointerover"`, `e.pointerType !== "mouse"`, `"focusin"`} {
		if !strings.Contains(string(js), want) {
			t.Errorf("dashboard.js: manca %q (tile aperta al passaggio del mouse e col focus)", want)
		}
	}
}

// "Mostra anche i contenuti non destinati a te" sta in fondo agli applicativi.
func TestAudienceToggleAfterApps(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedVisibility(t, db)
	body := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"}), nil).Body.String()
	toggle := strings.Index(body, "Mostra anche i contenuti non destinati a te")
	lastApp := strings.LastIndex(body, `class="category"`)
	widgets := strings.Index(body, `class="widgets"`)
	if toggle < 0 || toggle < lastApp || toggle > widgets || toggle < strings.Index(body, `id="alerts"`) {
		t.Fatalf("toggle fuori posto: toggle=%d ultima categoria=%d widget=%d", toggle, lastApp, widgets)
	}
}

// Avvisi nella colonna degli applicativi: i widget (oggi, calendario) stanno
// accanto agli avvisi, in alto. Le aperture coprono la pagina con un'ombra.
func TestAlertsBesideWidgets(t *testing.T) {
	s, db := newTestServer(t, nil)
	db.CreateAlert(database.Alert{Title: "A", Body: "x", Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	layout, col, alerts, apps, widgets := strings.Index(body, `class="layout"`), strings.Index(body, `class="main-col"`), strings.Index(body, `id="alerts"`), strings.Index(body, `class="apps"`), strings.Index(body, `class="widgets"`)
	if !(layout < col && col < alerts && alerts < apps && apps < widgets) {
		t.Fatalf("ordine: layout=%d colonna=%d avvisi=%d app=%d widget=%d", layout, col, alerts, apps, widgets)
	}
	css, _ := os.ReadFile("../../web/static/css/plancia.css")
	for _, want := range []string{
		".tile.open .tile-card { position: absolute;",
		".carousel.js-on .news:has(> .news-expand[open]) { position: absolute;",
		"body.plancia:has(.tile.open, .news-expand[open])::before { opacity: 1;",
	} {
		if !strings.Contains(string(css), want) {
			t.Errorf("plancia.css: manca %q", want)
		}
	}
}

// "Oggi" sta nella testata insieme all'orologio; nella colonna di destra
// prima le guide generali, poi il calendario.
func TestTodayInHeroAndWidgetOrder(t *testing.T) {
	s, db := newTestServer(t, nil)
	db.CreateGuide(database.Guide{Title: "VPN da casa", Kind: "link", URL: "https://wiki/vpn", Enabled: true})
	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	header := body[:strings.Index(body, "</header>")]
	for _, want := range []string{`class="hero-today"`, "data-clock", "Settimana 41", "ottobre 2026"} {
		if !strings.Contains(header, want) {
			t.Errorf("testata: manca %q", want)
		}
	}
	if strings.Contains(header, "data-date") || strings.Contains(header, "hero-info") {
		t.Error("niente data sotto il saluto; per chi non è riconosciuto nemmeno l'ufficio")
	}
	if strings.Count(body, "data-clock") != 1 || strings.Contains(body, `class="widget today"`) {
		t.Error("orologio e giorno solo nella testata")
	}
	if g, c := strings.Index(body, `class="widget guides"`), strings.Index(body, `id="widget-calendario"`); g < 0 || c < g {
		t.Errorf("ordine dei widget: guide=%d calendario=%d", g, c)
	}
}

// Sotto il saluto: gli attributi AD scelti dall'admin, nell'ordine scelto.
func TestHeroContacts(t *testing.T) {
	s, db := newTestServer(t, nil)
	off, _ := db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	tit, _ := db.CreateAudienceAttribute("title", "Qualifica")
	db.CreateAudienceAttribute("department", "Settore") // non in testata
	db.SetAttributeHero(off, database.HeroText)
	db.SetAttributeHero(tit, database.HeroText)
	body := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"}), nil).Body.String()
	header := body[:strings.Index(body, "</header>")]
	if !strings.Contains(header, `<span class="hero-item">Tributi</span>`) || !strings.Contains(header, "Istruttore amministrativo") {
		t.Fatalf("contatti nella testata:\n%s", header)
	}
	if strings.Index(header, "Tributi") > strings.Index(header, "Istruttore") {
		t.Error("ordine della testata non rispettato")
	}
	anon := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	if strings.Contains(anon[:strings.Index(anon, "</header>")], "hero-info") {
		t.Error("riga dei contatti per un anonimo")
	}
}

func TestHeroItems(t *testing.T) {
	attrs := []database.AudienceAttribute{
		{Name: "description", Hero: 1, HeroKind: database.HeroText},
		{Name: "telephoneNumber", Hero: 2, HeroKind: database.HeroPhone},
		{Name: "mail", Hero: 3, HeroKind: database.HeroMail},
		{Name: "pager", Hero: 4, HeroKind: database.HeroText},
	}
	// Più valori: il primo non vuoto; soli spazi: saltato.
	p := audience.Profile{Attrs: map[string][]string{
		"description":     {" ", "CED"},
		"telephonenumber": {"731"},
		"mail":            {"m.rossi@example.it"},
		"pager":           {"  "},
	}}
	got := heroItems(p, attrs)
	want := []heroItem{{"text", "Ced"}, {"phone", "Int. 731"}, {"mail", "m.rossi@example.it"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("heroItems = %+v", got)
	}
}

func TestTileSupport(t *testing.T) {
	s, db := newTestServer(t, nil)
	apps, _ := db.ListApps()
	a := apps[0]
	a.URL = "https://rubrica.local"
	db.UpdateApp(a)
	db.CreateSupportChannel(database.SupportChannel{Title: "Portale Maggioli", URL: "https://assistenza.example", Note: "serve l'utenza", Enabled: true, AppIDs: []int64{a.ID}})
	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	for _, want := range []string{
		`class="tile-support-flag`,
		`Problemi con ` + a.Title + `?`,
		`<a href="https://assistenza.example" target="_blank" rel="noopener">Portale Maggioli ↗</a>`,
		`serve l&#39;utenza`,
		`aria-controls="guide-app-` + itoa(a.ID) + `"`, // si apre anche senza guide
	} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
}

// Canale di un'app nascosta al visitatore: non compare.
func TestTileSupportFollowsAppVisibility(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedVisibility(t, db) // Webmail riservata al gruppo Tributi
	var webmail int64
	apps, _ := db.ListApps()
	for _, a := range apps {
		if a.Title == "Webmail" {
			webmail = a.ID
		}
	}
	db.CreateSupportChannel(database.SupportChannel{Title: "Canale riservato", URL: "https://r.example", Enabled: true, AppIDs: []int64{webmail}})
	body := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Anonymous: true}), nil).Body.String()
	if strings.Contains(body, "Canale riservato") {
		t.Fatal("canale di un'app nascosta visibile")
	}
	member := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"}), nil).Body.String()
	if !strings.Contains(member, "Canale riservato") {
		t.Fatal("chi vede l'app deve vederne l'assistenza")
	}
}

func TestSearchDropdownMarkup(t *testing.T) {
	s, _ := newTestServer(t, nil)
	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	for _, want := range []string{`role="combobox"`, `aria-controls="search-results"`, `aria-expanded="false"`, `id="search-results" role="listbox"`} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	if strings.Contains(body, "data-search-item") || strings.Contains(body, `id="no-results"`) {
		t.Error("il vecchio filtro delle tile va tolto")
	}
	js, _ := os.ReadFile("../../web/static/js/dashboard.js")
	for _, want := range []string{`getElementById("search-index")`, `"ArrowDown"`, `"ArrowUp"`, `"Enter"`, "aria-activedescendant", `normalize("NFD")`} {
		if !strings.Contains(string(js), want) {
			t.Errorf("dashboard.js: manca %q", want)
		}
	}
	if strings.Contains(string(js), "data-search-item") {
		t.Error("dashboard.js filtra ancora le tile")
	}
}

// Uscendo dalla ricerca con Tab (blur) la tendina si chiude; le opzioni non
// prendono il focus (pattern combobox: il focus resta sull'input).
func TestSearchDropdownClosesOnBlur(t *testing.T) {
	js, _ := os.ReadFile("../../web/static/js/dashboard.js")
	for _, want := range []string{`"focusout"`, "!wrap.contains(e.relatedTarget)", "a.tabIndex = -1"} {
		if !strings.Contains(string(js), want) {
			t.Errorf("dashboard.js: manca %q", want)
		}
	}
}
