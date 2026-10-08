package web

import (
	"os"
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

// Sotto il saluto: ufficio e qualifica da AD (i valori tutti maiuscoli resi
// leggibili).
func TestHeroShowsOffice(t *testing.T) {
	s, _ := newTestServer(t, nil)
	body := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"}), nil).Body.String()
	header := body[:strings.Index(body, "</header>")]
	if !strings.Contains(header, `<p class="hero-info">Tributi · Istruttore amministrativo</p>`) {
		t.Fatalf("ufficio nella testata:\n%s", header)
	}
	if got := officeLine(audience.Profile{Attrs: map[string][]string{"department": {"Ragioneria"}}}); got != "Ragioneria" {
		t.Errorf("department come ripiego: %q", got)
	}
	if got := officeLine(audience.Profile{}); got != "" {
		t.Errorf("profilo vuoto: %q", got)
	}
}
