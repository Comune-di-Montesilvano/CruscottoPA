package web

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestPersonalizzaJS(t *testing.T) {
	b, err := os.ReadFile("../../web/static/js/personalizza.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, want := range []string{"pointerdown", "setPointerCapture", "300", "sessionStorage", `"/preferiti"`, `"/ordine/categorie"`, `"/ordine/ripristina"`, `"/partials/preferiti"`, "ArrowLeft", `role", "status"`, "Escape", "passive: false", `credentials: "same-origin"`} {
		if !strings.Contains(js, want) {
			t.Errorf("personalizza.js: manca %q", want)
		}
	}
	// HTML dal server solo dalla risposta di /partials/preferiti.
	if n := len(regexp.MustCompile(`(innerHTML|outerHTML)\s*=`).FindAllString(js, -1)); n != 1 {
		t.Errorf("personalizza.js: %d assegnazioni di HTML, attesa una (il parziale dei preferiti)", n)
	}
}

func TestPersonalizzaCSS(t *testing.T) {
	b, _ := os.ReadFile("../../web/static/css/plancia.css")
	css := string(b)
	for _, want := range []string{"body.personalizza", "@keyframes pers-wiggle", "animation: pers-wiggle"} {
		if !strings.Contains(css, want) {
			t.Errorf("plancia.css: manca %q", want)
		}
	}
	// Con «effetti di animazione» spenti: solo il bordo, niente tremolio.
	re := regexp.MustCompile(`@media \(prefers-reduced-motion: reduce\) \{[^}]*body\.personalizza[^{]*\{ animation: none; \}`)
	if !re.MatchString(css) {
		t.Error("plancia.css: il tremolio non rispetta prefers-reduced-motion")
	}
}

func TestDashboardJSRespectsEdit(t *testing.T) {
	b, _ := os.ReadFile("../../web/static/js/dashboard.js")
	if n := strings.Count(string(b), `classList.contains("personalizza")`); n < 2 {
		t.Errorf("dashboard.js: carosello e apertura delle tile devono fermarsi in modifica (%d controlli)", n)
	}
}

func TestPersonalizzaScriptIncluded(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if body := visit(t, s, "mrossi"); !strings.Contains(body, `<script src="/static/js/personalizza.js" defer></script>`) {
		t.Fatal("plancia senza personalizza.js")
	}
}

// Categorie: il bersaglio è tutta la categoria (non solo il titolo), con il
// confronto sul punto medio; un rilascio sul posto non salva nulla.
func TestPersonalizzaDragTargets(t *testing.T) {
	b, _ := os.ReadFile("../../web/static/js/personalizza.js")
	js := string(b)
	for _, want := range []string{`closest(".category")`, "d.from", "items(d.box).indexOf(d.el) === d.from"} {
		if !strings.Contains(js, want) {
			t.Errorf("personalizza.js: manca %q", want)
		}
	}
}

// Minori della review: refresh in coda, Esc prudente, focus col mouse, tetto
// dal server, frecce dei fissati entro i fissati, annuncio dopo il refresh.
func TestPersonalizzaMinors(t *testing.T) {
	b, _ := os.ReadFile("../../web/static/js/personalizza.js")
	js := string(b)
	for _, want := range []string{"queue = queue.then(refreshFavorites)", "pendingFav", "function abortDrag(", `closest("input, textarea, select, dialog")`, "focus({ preventScroll: true })", "dataset.max", "announceFav("} {
		if !strings.Contains(js, want) {
			t.Errorf("personalizza.js: manca %q", want)
		}
	}
	if regexp.MustCompile(`>=\s*6\b`).MatchString(js) {
		t.Error("personalizza.js: tetto dei preferiti scritto a mano")
	}
	css, _ := os.ReadFile("../../web/static/css/plancia.css")
	if !strings.Contains(string(css), "body.personalizza .audience-toggle { display: none; }") {
		t.Error("plancia.css: «Mostra anche…» visibile in modifica")
	}
}

func TestFavoritesMaxAttr(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if body := visit(t, s, "mrossi"); !strings.Contains(favSection(t, body), `data-max="6"`) {
		t.Fatal("tetto dei preferiti non passato al JS")
	}
}
