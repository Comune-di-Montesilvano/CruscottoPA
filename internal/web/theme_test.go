package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	cssComment     = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssFixed       = regexp.MustCompile(`[^;{}]*;?\s*/\* fisso:[^*]*\*/`)
	cssColorTokens = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|\brgba?\(|\bhsla?\(|[\s:,(](white|black)(\s|;|,|\)|!|$)`)
	cssDecl        = regexp.MustCompile(`[^;{}]+`)
)

// cssStrayColors restituisce le dichiarazioni con un colore scritto fuori
// dalle definizioni dei token (custom property --x): ogni colore deve passare
// dai token, così il blocco @media (prefers-color-scheme: dark) li cambia tutti.
// Una dichiarazione seguita da /* fisso: motivo */ è un'eccezione voluta.
func cssStrayColors(css string) []string {
	css = cssFixed.ReplaceAllString(css, "")
	css = cssComment.ReplaceAllString(css, "")
	var out []string
	for _, d := range cssDecl.FindAllString(css, -1) {
		if strings.HasPrefix(strings.TrimSpace(d), "--") {
			continue // definizione di un token
		}
		if cssColorTokens.MatchString(d) {
			out = append(out, strings.TrimSpace(d))
		}
	}
	return out
}

func TestCSSStrayColorsHelper(t *testing.T) {
	for css, want := range map[string]int{
		":root{--a:#fff} .x{color:#000}":                       1,
		".x{color:#000; /* fisso: logo */}":                    0,
		"@media (prefers-color-scheme: dark){:root{--a:#111}}": 0,
		".x{background:rgba(0,0,0,.2)}":                        1,
		".x{color:white}":                                      1,
		".x{color: black; white-space: nowrap}":                1,
		"body.p{--p-a: #fff; --p-s: 0 1px rgba(0,0,0,.1)}":     0,
		".x{white-space: nowrap}":                              0,
		".x{color:transparent; border-color:currentColor}":     0,
		".x{color:var(--ink)} .y{background:hsl(0 0% 0%)}":     1,
		"/* #fff in un commento */ .x{color:var(--ink)}":       0,
		".x{color:#abc; background:#fff /* fisso: icone */}":   1,
	} {
		if got := cssStrayColors(css); len(got) != want {
			t.Errorf("cssStrayColors(%q) = %q, attesi %d", css, got, want)
		}
	}
}

func TestThemeCSS(t *testing.T) {
	for _, c := range []struct {
		file      string
		darkBlock bool
	}{
		{"../../web/static/css/app.css", true},
		{"../../web/static/css/admin.css", false}, // solo token condivisi di app.css
		{"../../web/static/css/plancia.css", true},
		{"../../web/editor/src/editor.css", true},
	} {
		b, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatal(err)
		}
		css := string(b)
		if stray := cssStrayColors(css); len(stray) > 0 {
			t.Errorf("%s: colori fuori dai token (%d):\n%s", c.file, len(stray), strings.Join(stray, "\n"))
		}
		if c.darkBlock && !strings.Contains(css, "@media (prefers-color-scheme: dark)") {
			t.Errorf("%s: manca il blocco del tema scuro", c.file)
		}
	}
	b, _ := os.ReadFile("../../web/static/css/app.css")
	if !strings.Contains(string(b), "color-scheme: light dark") {
		t.Error("app.css: manca color-scheme: light dark (scrollbar, campi e dialog nativi)")
	}
}

func TestThemeTemplates(t *testing.T) {
	s, _ := newTestServer(t, nil)
	home := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	for _, want := range []string{
		`<meta name="theme-color" content="#1565c0" media="(prefers-color-scheme: light)">`,
		`<meta name="theme-color" content="#0f3a78" media="(prefers-color-scheme: dark)">`,
	} {
		if !strings.Contains(home, want) {
			t.Errorf("plancia: manca %s", want)
		}
	}
	const darkLogo = `<source srcset="/static/img/logo-on-dark.svg" media="(prefers-color-scheme: dark)">`
	for _, url := range []string{"/", "/avvisi", "/admin/login"} {
		if body := do(t, s, "GET", url, nil, nil, nil).Body.String(); !strings.Contains(body, darkLogo) {
			t.Errorf("%s: il logo CruscottoPA non ha la versione scura", url)
		}
	}
	files, _ := filepath.Glob("../../web/templates/*.html")
	for _, f := range files {
		if filepath.Base(f) == "partials_brand.html" {
			continue
		}
		b, _ := os.ReadFile(f)
		if strings.Contains(string(b), "logo-on-light.svg") {
			t.Errorf("%s: usare il template logo_light_dark", f)
		}
	}
}
