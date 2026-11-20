package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// favSection: la sezione dei preferiti nella pagina (fino al carosello).
func favSection(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, "data-personalizza")
	if i < 0 {
		t.Fatalf("sezione preferiti assente")
	}
	i = strings.LastIndex(body[:i], "<section")
	j := strings.Index(body[i:], `id="alerts"`)
	if j < 0 {
		return body[i:]
	}
	return body[i : i+j]
}

func addOpens(t *testing.T, db *database.DB, user string, id int64, n int) {
	t.Helper()
	for range n {
		if err := db.AddAppOpen(user, id, openDay); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFavoritesSection(t *testing.T) {
	s, db := newTestServer(t, nil)
	a, b := webApp(t, db, "protocollo"), webApp(t, db, "presenze")
	db.CreateGuide(database.Guide{AppID: &a, Title: "Come protocollare", Kind: "link", URL: "https://wiki/x", Enabled: true})
	addOpens(t, db, "mrossi", a, 10)
	db.SaveFavorites("mrossi", []int64{b})
	sec := favSection(t, visit(t, s, "mrossi"))
	ia, ib := strings.Index(sec, `data-tile-id="`+itoa(a)+`"`), strings.Index(sec, `data-tile-id="`+itoa(b)+`"`)
	if ia < 0 || ib < 0 || ib > ia {
		t.Fatalf("ordine dei preferiti (B fissata prima di A):\n%s", sec)
	}
	if !strings.Contains(sec, `data-tile-id="`+itoa(b)+`" data-pinned`) || !strings.Contains(sec, `data-sortable="preferiti"`) {
		t.Fatalf("fissato non segnato:\n%s", sec)
	}
	if !strings.Contains(sec, `id="fav-guide-app-`+itoa(a)+`"`) {
		t.Fatalf("id della copia non unici:\n%s", sec)
	}
	for _, c := range []*http.Cookie{nil, viewerCookie(t, s, identity.User{Anonymous: true})} {
		if body := do(t, s, "GET", "/", nil, c, nil).Body.String(); strings.Contains(body, "data-personalizza") {
			t.Fatal("preferiti per chi non è riconosciuto")
		}
	}
}

func TestFavoritesHiddenForNewUser(t *testing.T) {
	s, db := newTestServer(t, nil)
	a := webApp(t, db, "protocollo")
	addOpens(t, db, "mrossi", a, 9) // sotto la soglia
	sec := favSection(t, visit(t, s, "mrossi"))
	if !strings.Contains(sec, "favorites-empty") || strings.Contains(sec, "data-tile-id") {
		t.Fatalf("utente nuovo:\n%s", sec)
	}
	if !strings.Contains(sec, "data-personalizza-toggle") {
		t.Fatal("manca «Personalizza»")
	}
}

func TestUserOrderApplied(t *testing.T) {
	s, db := newTestServer(t, nil)
	cats, _ := db.ListCategories()
	c1 := cats[0].ID
	c2, _ := db.CreateCategory("Zeta")
	a1, a2 := webApp(t, db, "alfa"), webApp(t, db, "beta")
	z := webApp(t, db, "zeta")
	app, _ := db.GetApp(z)
	app.CategoryID = c2
	db.UpdateApp(app)
	db.SaveCategoryOrder("mrossi", []int64{c2, c1})
	db.SaveAppOrder("mrossi", c1, []int64{a2, a1})
	body := visit(t, s, "mrossi")
	main := body[strings.Index(body, `data-sortable="categorie"`):]
	if strings.Index(main, `data-id="`+itoa(c2)+`"`) > strings.Index(main, `data-id="`+itoa(c1)+`"`) {
		t.Fatal("ordine personale delle categorie non applicato")
	}
	if strings.Index(main, "https://beta.example.it") > strings.Index(main, "https://alfa.example.it") {
		t.Fatal("ordine personale delle app non applicato")
	}
	idx := body[strings.Index(body, `id="search-index"`):]
	if strings.Index(idx, "alfa.example.it") > strings.Index(idx, "beta.example.it") {
		t.Fatal("l'indice di ricerca ha cambiato ordine")
	}
	other := visit(t, s, "anna")
	om := other[strings.Index(other, `data-sortable="categorie"`):]
	if strings.Index(om, "https://alfa.example.it") > strings.Index(om, "https://beta.example.it") {
		t.Fatal("un altro utente vede l'ordine di mrossi")
	}
}

func TestAlertsPartialNoFavorites(t *testing.T) {
	s, db := newTestServer(t, nil)
	a := webApp(t, db, "protocollo")
	db.SaveFavorites("mrossi", []int64{a})
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "mrossi"})
	if body := do(t, s, "GET", "/partials/alerts", nil, c, hx).Body.String(); strings.Contains(body, "data-personalizza") {
		t.Fatal("il refresh del carosello porta i preferiti")
	}
}

// riservata: un'app destinata a un gruppo senza membri.
func riservata(t *testing.T, db *database.DB, title string) int64 {
	t.Helper()
	id := webApp(t, db, title)
	g, _ := db.CreateAudienceGroup("Nessuno " + title)
	db.SetContentAudience(database.ContentApp, id, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{g}})
	return id
}

// Con «Mostra tutto» l'app non destinata fissata sta nei preferiti come le
// altre; nelle categorie resta segnata e senza puntina.
func TestFavoritesNotTargetedWithShowAll(t *testing.T) {
	s, db := newTestServer(t, nil)
	id := riservata(t, db, "riservata")
	db.SaveFavorites("mrossi", []int64{id})
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "mrossi"})
	body := do(t, s, "GET", "/", nil, nil, map[string]string{"Cookie": c.Name + "=" + c.Value + "; " + showAllCookie + "=1"}).Body.String()
	fav := favSection(t, body)
	if !strings.Contains(fav, `data-tile-id="`+itoa(id)+`" data-pinned style`) {
		t.Fatalf("app non destinata fissata assente dai preferiti:\n%s", fav)
	}
	tile := body[strings.Index(body, `data-sortable="categorie"`):]
	tile = tile[strings.Index(tile, `data-tile-id="`+itoa(id)+`"`):]
	tile = tile[:strings.Index(tile, "</a>")]
	if !strings.Contains(tile, "data-altri") || strings.Contains(body[strings.Index(body, `data-sortable="categorie"`):], `data-pin="`+itoa(id)+`"`) {
		t.Fatalf("riquadro non destinato:\n%s", tile)
	}
}

// Senza «Mostra tutto» l'app non destinata aperta spesso compare solo nei
// preferiti, con le sole guide destinate; le categorie non la mostrano.
func TestFavoritesNotTargetedAuto(t *testing.T) {
	s, db := newTestServer(t, nil)
	id := riservata(t, db, "riservata")
	g := id
	db.CreateGuide(database.Guide{AppID: &g, Title: "Guida per tutti", Kind: "link", URL: "https://wiki.example.it/a", Enabled: true})
	hidden, _ := db.CreateGuide(database.Guide{AppID: &g, Title: "Guida riservata", Kind: "link", URL: "https://wiki.example.it/b", Enabled: true})
	grp, _ := db.CreateAudienceGroup("Nessuno guida")
	db.SetContentAudience(database.ContentGuide, hidden, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{grp}})
	addOpens(t, db, "mrossi", id, favMinOpens)
	body := visit(t, s, "mrossi")
	fav := favSection(t, body)
	if !strings.Contains(fav, `data-tile-id="`+itoa(id)+`"`) || strings.Contains(fav, "data-altri") {
		t.Fatalf("app non destinata aperta spesso:\n%s", fav)
	}
	if !strings.Contains(fav, "Guida per tutti") || strings.Contains(fav, "Guida riservata") {
		t.Fatalf("guide nei preferiti:\n%s", fav)
	}
	if strings.Contains(body[strings.Index(body, `id="alerts"`):], `data-tile-id="`+itoa(id)+`"`) {
		t.Fatal("app non destinata nelle categorie senza «Mostra tutto»")
	}
}

// Contano solo le aperture degli ultimi favOpensDays giorni.
func TestFavoritesOpensWindow(t *testing.T) {
	s, db := newTestServer(t, nil)
	a := webApp(t, db, "protocollo")
	for range favMinOpens {
		db.AddAppOpen("mrossi", a, "2026-07-08") // 91 giorni prima di fixedNow
	}
	if sec := favSection(t, visit(t, s, "mrossi")); strings.Contains(sec, `data-tile-id="`+itoa(a)+`"`) {
		t.Fatalf("aperture vecchie contate:\n%s", sec)
	}
	for range favMinOpens {
		db.AddAppOpen("mrossi", a, "2026-07-09") // 90 giorni, oggi compreso
	}
	if sec := favSection(t, visit(t, s, "mrossi")); !strings.Contains(sec, `data-tile-id="`+itoa(a)+`"`) {
		t.Fatalf("aperture nella finestra non contate:\n%s", sec)
	}
}

func TestCleanupRemovesLayout(t *testing.T) {
	s, db := newTestServer(t, nil)
	a := webApp(t, db, "protocollo")
	db.TouchPresence("cessato", "Ex", fixedNow)
	addOpens(t, db, "cessato", a, 3)
	db.SaveFavorites("cessato", []int64{a})
	s.CleanupPresence()
	l, _ := db.UserLayout("cessato")
	if m, _ := db.AppOpens("cessato", "2000-01-01"); len(m) != 0 || len(l.Favorites) != 0 {
		t.Fatalf("dati di chi AD non conosce più: %v %+v", m, l)
	}
}

func TestFavoritesPartial(t *testing.T) {
	s, db := newTestServer(t, nil)
	a := webApp(t, db, "protocollo")
	db.SaveFavorites("mrossi", []int64{a})
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "mrossi"})
	rec := do(t, s, "GET", "/partials/preferiti", nil, c, hx)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-sortable="preferiti"`) || !strings.Contains(rec.Body.String(), `data-tile-id="`+itoa(a)+`"`) {
		t.Fatalf("parziale: %d\n%s", rec.Code, rec.Body.String())
	}
	rec = do(t, s, "GET", "/partials/preferiti", nil, nil, hx)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "data-personalizza") {
		t.Fatalf("parziale anonimo: %d\n%s", rec.Code, rec.Body.String())
	}
}
