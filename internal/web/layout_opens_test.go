package web

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// openDay: il giorno di fixedNow a Roma.
const openDay = "2026-10-06"

func opens(t *testing.T, db *database.DB, user string, id int64) int {
	t.Helper()
	m, err := db.AppOpens(user, "2000-01-01")
	if err != nil {
		t.Fatal(err)
	}
	return m[id]
}

func TestAppOpenCounted(t *testing.T) {
	s, db := newTestServer(t, nil)
	id := webApp(t, db, "protocollo")
	click(t, s, "/app/"+itoa(id)+"/clic", "mrossi")
	if n := opens(t, db, "mrossi", id); n != 1 {
		t.Fatalf("aperture: %d", n)
	}
	click(t, s, "/app/"+itoa(id)+"/clic", "")
	do(t, s, "POST", "/app/"+itoa(id)+"/clic", nil, viewerCookie(t, s, identity.User{Anonymous: true}), nil)
	if n := opens(t, db, "", id); n != 0 {
		t.Fatalf("anonimo contato: %d", n)
	}
}

// Due aperture a meno di 30 secondi contano come una.
func TestAppOpenThrottle(t *testing.T) {
	s, db := newTestServer(t, nil)
	id := webApp(t, db, "protocollo")
	click(t, s, "/app/"+itoa(id)+"/clic", "mrossi")
	s.now = func() time.Time { return fixedNow.Add(29 * time.Second) }
	click(t, s, "/app/"+itoa(id)+"/clic", "mrossi")
	if n := opens(t, db, "mrossi", id); n != 1 {
		t.Fatalf("entro 30 s: %d", n)
	}
	s.now = func() time.Time { return fixedNow.Add(31 * time.Second) }
	click(t, s, "/app/"+itoa(id)+"/clic", "mrossi")
	if n := opens(t, db, "mrossi", id); n != 2 {
		t.Fatalf("dopo 30 s: %d", n)
	}
}

func TestAppOpenWithoutStats(t *testing.T) {
	s, db := newTestServerWith(t, nil, func(o *Options) { o.Config.StatsEnabled = false })
	id := webApp(t, db, "protocollo")
	click(t, s, "/app/"+itoa(id)+"/clic", "mrossi")
	if n := opens(t, db, "mrossi", id); n != 1 {
		t.Fatalf("aperture con STATS_ENABLED=false: %d", n)
	}
	if n := clicks(t, db, id); n != 0 {
		t.Fatalf("clic aggregato con STATS_ENABLED=false: %d", n)
	}
}

// App non destinata aperta con «Mostra tutto»: conta per i preferiti e per la
// fiammella. Un'app che non è in plancia non conta mai.
func TestAppOpenNotTargeted(t *testing.T) {
	s, db := newTestServer(t, nil)
	id := webApp(t, db, "riservata")
	g, _ := db.CreateAudienceGroup("Nessuno")
	db.SetContentAudience(database.ContentApp, id, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{g}})
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "mrossi"})
	req := do(t, s, "POST", "/app/"+itoa(id)+"/clic", nil, nil, map[string]string{
		"Cookie": c.Name + "=" + c.Value + "; " + showAllCookie + "=1"})
	if req.Code != http.StatusOK {
		t.Fatalf("status %d", req.Code)
	}
	if n := opens(t, db, "mrossi", id); n != 1 {
		t.Fatalf("app non destinata non contata: %d", n)
	}
	if n := clicks(t, db, id); n != 1 {
		t.Fatalf("clic aggregato di un'app non destinata: %d", n)
	}
	off := webApp(t, db, "spenta")
	app, _ := db.GetApp(off)
	app.Enabled = false
	db.UpdateApp(app)
	click(t, s, "/app/"+itoa(off)+"/clic", "mrossi")
	if n := opens(t, db, "mrossi", off); n != 0 {
		t.Fatalf("app disattivata contata: %d", n)
	}
}

// Le aperture si salvano nel giorno di Roma.
func TestAppOpenDay(t *testing.T) {
	s, db := newTestServer(t, nil)
	id := webApp(t, db, "protocollo")
	click(t, s, "/app/"+itoa(id)+"/clic", "mrossi")
	if m, _ := db.AppOpens("mrossi", openDay); m[id] != 1 {
		t.Fatalf("aperture del giorno: %v", m)
	}
	if m, _ := db.AppOpens("mrossi", "2026-10-07"); m[id] != 0 {
		t.Fatalf("apertura nel giorno sbagliato: %v", m)
	}
}

func TestSearchIndexAppID(t *testing.T) {
	d := database.Dashboard{Categories: []database.CategoryWithApps{{
		Category: database.Category{ID: 1, Name: "Applicativi"},
		Apps: []database.AppWithGuides{{App: database.App{ID: 7, Title: "Protocollo", URL: "https://p.example.it"},
			Guides: []database.Guide{{ID: 3, Title: "Guida", Kind: database.GuideKindLink, URL: "https://g.example.it"}}}},
	}}}
	for _, it := range buildSearchIndex(d) {
		if it.Kind == "app" && it.ID != 7 || it.Kind != "app" && it.ID != 0 {
			t.Fatalf("voce %+v", it)
		}
	}
}

func TestSearchBeaconJS(t *testing.T) {
	js, err := os.ReadFile("../../web/static/js/dashboard.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`a[data-app]`, `dataset.app = `} {
		if !strings.Contains(string(js), want) {
			t.Errorf("dashboard.js senza %q", want)
		}
	}
}
