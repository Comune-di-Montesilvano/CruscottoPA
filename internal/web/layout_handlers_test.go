package web

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

func ids(vs ...int64) url.Values {
	f := url.Values{}
	for _, v := range vs {
		f.Add("id", itoa(v))
	}
	return f
}

// post invia come mrossi e restituisce il corpo.
func post(t *testing.T, s *Server, path string, form url.Values) string {
	t.Helper()
	rec := do(t, s, "POST", path, form, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "mrossi"}), nil)
	if rec.Code != 200 {
		t.Fatalf("%s: status %d", path, rec.Code)
	}
	return rec.Body.String()
}

func TestSaveFavorites(t *testing.T) {
	s, db := newTestServer(t, nil)
	a, b := webApp(t, db, "a"), webApp(t, db, "b")
	if body := post(t, s, "/preferiti", ids(b, a)); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("risposta: %s", body)
	}
	if l, _ := db.UserLayout("mrossi"); !reflect.DeepEqual(l.Favorites, []int64{b, a}) {
		t.Fatalf("preferiti: %v", l.Favorites)
	}
	var many []int64
	for _, n := range []string{"c", "d", "e", "f", "g", "h"} {
		many = append(many, webApp(t, db, n))
	}
	post(t, s, "/preferiti", ids(append([]int64{a, b}, many...)...))
	if l, _ := db.UserLayout("mrossi"); len(l.Favorites) != favMax {
		t.Fatalf("oltre il tetto: %v", l.Favorites)
	}
}

func TestSaveKeepsHiddenPin(t *testing.T) {
	s, db := newTestServer(t, nil)
	a, b := webApp(t, db, "a"), webApp(t, db, "b")
	db.SaveFavorites("mrossi", []int64{b})
	app, _ := db.GetApp(b)
	app.Enabled = false
	db.UpdateApp(app)
	post(t, s, "/preferiti", ids(a))
	if l, _ := db.UserLayout("mrossi"); !reflect.DeepEqual(l.Favorites, []int64{a, b}) {
		t.Fatalf("fissato nascosto perso: %v", l.Favorites)
	}
}

func TestSaveDropsNotTargeted(t *testing.T) {
	s, db := newTestServer(t, nil)
	a, r := webApp(t, db, "a"), webApp(t, db, "riservata")
	g, _ := db.CreateAudienceGroup("Nessuno")
	db.SetContentAudience(database.ContentApp, r, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{g}})
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "mrossi"})
	do(t, s, "POST", "/preferiti", ids(r, a), nil, map[string]string{"Cookie": c.Name + "=" + c.Value + "; " + showAllCookie + "=1"})
	if l, _ := db.UserLayout("mrossi"); !reflect.DeepEqual(l.Favorites, []int64{a}) {
		t.Fatalf("app non destinata salvata: %v", l.Favorites)
	}
}

func TestSaveOrders(t *testing.T) {
	s, db := newTestServer(t, nil)
	cats, _ := db.ListCategories()
	c1 := cats[0].ID
	c2, _ := db.CreateCategory("Zeta")
	a1, a2 := webApp(t, db, "a1"), webApp(t, db, "a2")
	z := webApp(t, db, "z")
	app, _ := db.GetApp(z)
	app.CategoryID = c2
	db.UpdateApp(app)
	post(t, s, "/ordine/categorie", ids(c2, c1, 9999))
	post(t, s, "/ordine/categorie/"+itoa(c1), ids(a2, z, a1))
	l, _ := db.UserLayout("mrossi")
	if l.Categories[c2] != 0 || l.Categories[c1] != 1 || len(l.Categories) != 2 {
		t.Fatalf("categorie: %v", l.Categories)
	}
	if l.Apps[a2] != 0 || l.Apps[a1] != 1 {
		t.Fatalf("app: %v", l.Apps)
	}
	if _, ok := l.Apps[z]; ok {
		t.Fatal("app di un'altra categoria salvata")
	}
}

func TestResetLayoutEndpoint(t *testing.T) {
	s, db := newTestServer(t, nil)
	a := webApp(t, db, "a")
	db.SaveFavorites("mrossi", []int64{a})
	db.AddAppOpen("mrossi", a)
	if body := post(t, s, "/ordine/ripristina", nil); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("risposta: %s", body)
	}
	l, _ := db.UserLayout("mrossi")
	m, _ := db.AppOpens("mrossi")
	if len(l.Favorites) != 0 || m[a] != 1 {
		t.Fatalf("dopo il ripristino: %+v %v", l, m)
	}
}

func TestLayoutEndpointsAnonymous(t *testing.T) {
	s, db := newTestServer(t, nil)
	a := webApp(t, db, "a")
	for _, path := range []string{"/preferiti", "/ordine/categorie", "/ordine/ripristina"} {
		rec := do(t, s, "POST", path, ids(a), nil, nil)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok":false`) {
			t.Fatalf("%s anonimo: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	if l, _ := db.UserLayout(""); len(l.Favorites)+len(l.Categories) != 0 {
		t.Fatal("scritture senza utente")
	}
}

func TestLayoutEndpointsCSRF(t *testing.T) {
	s, db := newTestServer(t, nil)
	a := webApp(t, db, "a")
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "mrossi"})
	do(t, s, "POST", "/preferiti", ids(a), c, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if l, _ := db.UserLayout("mrossi"); len(l.Favorites) != 0 {
		t.Fatal("richiesta da un altro sito accettata")
	}
}

// Il settimo fissato visibile (non mostrato) resta salvato dopo un salvataggio.
func TestSaveKeepsSeventhPin(t *testing.T) {
	s, db := newTestServer(t, nil)
	var pins []int64
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		pins = append(pins, webApp(t, db, n))
	}
	db.SaveFavorites("mrossi", pins)
	post(t, s, "/preferiti", ids(pins[1], pins[0], pins[2], pins[3], pins[4]))
	want := []int64{pins[1], pins[0], pins[2], pins[3], pins[4], pins[6]}
	if l, _ := db.UserLayout("mrossi"); !reflect.DeepEqual(l.Favorites, want) {
		t.Fatalf("settimo perso: %v, attesi %v", l.Favorites, want)
	}
}
