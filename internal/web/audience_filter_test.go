package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// seedVisibility: gruppo "Tributi" (ufficio TRIBUTI), Rubrica pubblica,
// Webmail riservata a Tributi, guida generale nascosta a Tributi, urgente
// riservato a un gruppo senza membri, novità pubblica.
func seedVisibility(t *testing.T, db *database.DB) {
	t.Helper()
	db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	trib, _ := db.CreateAudienceGroup("Tributi")
	db.AddAudienceRule(database.AudienceRule{GroupID: trib, Kind: audience.KindAttr, Attr: "physicalDeliveryOfficeName", Value: "TRIBUTI"})
	nessuno, _ := db.CreateAudienceGroup("Nessuno")

	apps, _ := db.ListApps()
	for _, a := range apps {
		a.URL = "https://example.it/" + strings.ToLower(a.Title)
		if err := db.UpdateApp(a); err != nil {
			t.Fatal(err)
		}
		if a.Title == "Webmail" {
			db.SetContentAudience(database.ContentApp, a.ID, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{trib}})
		}
	}
	gid, _ := db.CreateGuide(database.Guide{Title: "Guida non per Tributi", URL: "https://example.it/g", Kind: "link", Enabled: true})
	db.SetContentAudience(database.ContentGuide, gid, database.ContentAudience{Mode: audience.ModeHide, Groups: []int64{trib}})
	start := fixedNow.Add(-time.Hour)
	uid, _ := db.CreateAlert(database.Alert{Title: "Urgente riservato", Level: database.LevelUrgent, StartsAt: start})
	db.SetContentAudience(database.ContentAlert, uid, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{nessuno}})
	db.CreateAlert(database.Alert{Title: "Novità per tutti", Level: database.LevelNews, StartsAt: start})
}

func viewerCookie(t *testing.T, s *Server, u identity.User) *http.Cookie {
	t.Helper()
	v, err := s.cookies.Encode(u)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: identity.CookieName, Value: v}
}

func TestDashboardFilteredForMember(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedVisibility(t, db)
	body := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"}), nil).Body.String()
	for _, want := range []string{"Rubrica", "Webmail", "Novità per tutti", "Mostra anche i contenuti non destinati a te (2)"} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	for _, no := range []string{"Guida non per Tributi", "Urgente riservato"} {
		if strings.Contains(body, no) {
			t.Errorf("non doveva esserci %q", no)
		}
	}
}

func TestDashboardAnonymousOnlyPublic(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedVisibility(t, db)
	body := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Anonymous: true}), nil).Body.String()
	if !strings.Contains(body, "Rubrica") || strings.Contains(body, "Webmail") || strings.Contains(body, "Guida non per Tributi") {
		t.Fatal("anonimo: solo pubblici (anche i «Nascosto a» restano nascosti)")
	}
}

func TestDashboardShowAllNoUrgentPopup(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedVisibility(t, db)
	h := map[string]string{"Cookie": viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"}).String() + "; " + showAllCookie + "=1"}
	body := do(t, s, "GET", "/", nil, nil, h).Body.String()
	if !strings.Contains(body, "Urgente riservato") || !strings.Contains(body, "Mostra solo i miei contenuti") {
		t.Fatal("con Mostra tutto l'urgente compare nel carosello")
	}
	if strings.Contains(body, `<dialog class="urgent"`) {
		t.Fatal("niente popup per un urgente non destinato all'utente")
	}
}

// Il refresh del carosello aggiorna anche il contatore di "Mostra anche…"
// (swap out-of-band): un avviso riservato pubblicato dopo il caricamento conta.
func TestAlertsPartialRefreshesAudienceToggle(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedVisibility(t, db)
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	body := do(t, s, "GET", "/partials/alerts", nil, c, hx).Body.String()
	if !strings.Contains(body, `id="audience-toggle" hx-swap-oob="true"`) || !strings.Contains(body, "non destinati a te (2)") {
		t.Fatalf("contatore nel refresh:\n%s", body)
	}
	trib, _ := db.CreateAudienceGroup("Altri")
	id, _ := db.CreateAlert(database.Alert{Title: "Nuovo riservato", Level: database.LevelNews, StartsAt: fixedNow.Add(-time.Minute)})
	db.SetContentAudience(database.ContentAlert, id, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{trib}})
	if body := do(t, s, "GET", "/partials/alerts", nil, c, hx).Body.String(); !strings.Contains(body, "non destinati a te (3)") {
		t.Fatalf("contatore dopo un nuovo avviso riservato:\n%s", body)
	}
	if page := do(t, s, "GET", "/", nil, c, nil).Body.String(); !strings.Contains(page, `<div id="audience-toggle">`) {
		t.Fatal("la plancia deve avere il contenitore del contatore")
	}
}

func TestAvvisiAndPartialFilteredByAudience(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedVisibility(t, db)
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	for _, path := range []string{"/avvisi", "/partials/alerts"} {
		if body := do(t, s, "GET", path, nil, c, nil).Body.String(); strings.Contains(body, "Urgente riservato") || !strings.Contains(body, "Novità per tutti") {
			t.Errorf("%s: filtro avvisi mancante", path)
		}
	}
}

func TestProfileCachedAcrossPages(t *testing.T) {
	calls := 0
	dir := testDirectory
	dir.calls = &calls
	s, db := newTestServerWith(t, nil, func(o *Options) { o.Directory = dir })
	seedVisibility(t, db)
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	for i := 0; i < 3; i++ {
		do(t, s, "GET", "/", nil, c, nil)
	}
	if calls != 1 {
		t.Fatalf("profilo letto da AD %d volte invece di 1", calls)
	}
}
