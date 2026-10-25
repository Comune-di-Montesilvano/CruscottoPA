package web

import (
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

func webApp(t *testing.T, db *database.DB, title string) int64 {
	t.Helper()
	cats, _ := db.ListCategories()
	id, err := db.CreateApp(database.App{CategoryID: cats[0].ID, Title: title, URL: "https://" + title + ".example.it",
		IconKind: database.IconPack, IconValue: "apps", IconColor: "#475569", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func clicks(t *testing.T, db *database.DB, id int64) int {
	t.Helper()
	m, err := db.AppClicksSince("2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	return m[id]
}

func click(t *testing.T, s *Server, path, user string) {
	t.Helper()
	var c = viewerCookie(t, s, identity.User{Username: user, Name: user})
	if user == "" {
		c = nil
	}
	if rec := do(t, s, "POST", path, nil, c, nil); rec.Code != 200 {
		t.Fatalf("%s: %d (deve essere sempre 200)", path, rec.Code)
	}
}

func TestAppClickCountsOncePerDay(t *testing.T) {
	s, db := newTestServer(t, nil)
	id := webApp(t, db, "protocollo")
	for range 3 {
		click(t, s, "/app/"+itoa(id)+"/clic", "mrossi")
	}
	if n := clicks(t, db, id); n != 1 {
		t.Fatalf("mrossi: %d clic", n)
	}
	click(t, s, "/app/"+itoa(id)+"/clic", "anna")
	if n := clicks(t, db, id); n != 2 {
		t.Fatalf("con anna: %d clic", n)
	}
}

func TestAppClickAnonymousIgnored(t *testing.T) {
	s, db := newTestServer(t, nil)
	id := webApp(t, db, "protocollo")
	click(t, s, "/app/"+itoa(id)+"/clic", "")
	c := viewerCookie(t, s, identity.User{Anonymous: true})
	do(t, s, "POST", "/app/"+itoa(id)+"/clic", nil, c, nil)
	if n := clicks(t, db, id); n != 0 {
		t.Fatalf("anonimo contato: %d", n)
	}
}

func TestAppClickUnknownApp(t *testing.T) {
	s, db := newTestServer(t, nil)
	click(t, s, "/app/99999/clic", "mrossi")
	click(t, s, "/app/abc/clic", "mrossi")
	if m, _ := db.AppClicksSince("2026-01-01"); len(m) != 0 {
		t.Fatalf("righe: %v", m)
	}
}

func TestAppClickReservedApp(t *testing.T) {
	s, db := newTestServer(t, nil)
	id := webApp(t, db, "riservata")
	g, _ := db.CreateAudienceGroup("Nessuno")
	db.SetContentAudience(database.ContentApp, id, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{g}})
	click(t, s, "/app/"+itoa(id)+"/clic", "mrossi")
	if n := clicks(t, db, id); n != 0 {
		t.Fatalf("app non destinata contata: %d", n)
	}
}

func TestAppClickStatsDisabled(t *testing.T) {
	s, db := newTestServerWith(t, nil, func(o *Options) { o.Config.StatsEnabled = false })
	id := webApp(t, db, "protocollo")
	click(t, s, "/app/"+itoa(id)+"/clic", "mrossi")
	if n := clicks(t, db, id); n != 0 {
		t.Fatalf("contato con STATS_ENABLED=false: %d", n)
	}
}

func TestHotTiles(t *testing.T) {
	s, db := newTestServer(t, nil)
	ids := []int64{webApp(t, db, "a9"), webApp(t, db, "b7"), webApp(t, db, "c5"), webApp(t, db, "d4")}
	off := webApp(t, db, "spenta")
	for i, n := range []int{9, 7, 5, 4} {
		for range n {
			db.AddAppClick(ids[i], "2026-10-05")
		}
	}
	for range 20 {
		db.AddAppClick(off, "2026-10-05")
	}
	a, _ := db.GetApp(off)
	a.Enabled = false
	db.UpdateApp(a)
	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	if n := strings.Count(body, `class="tile-hot"`); n != 3 {
		t.Fatalf("fiammelle: %d", n)
	}
	for _, title := range []string{"a9", "b7", "c5"} {
		i := strings.Index(body, `href="https://`+title+`.example.it"`)
		j := strings.LastIndex(body[:i], `<div class="tile-card">`)
		if j < 0 || !strings.Contains(body[j:i], "tile-hot") {
			t.Fatalf("fiammella mancante su %s", title)
		}
	}
	if strings.Contains(body, "spenta.example.it") {
		t.Fatal("app disabilitata in pagina")
	}
}
