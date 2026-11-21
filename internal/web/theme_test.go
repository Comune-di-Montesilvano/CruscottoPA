package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	// /* fisso: motivo */ sulla stessa riga della dichiarazione che esenta.
	cssFixed    = regexp.MustCompile(`[^;{}\n]*;?[ \t]*/\* fisso:[^*]*\*/`)
	cssRule     = regexp.MustCompile(`[^{}]*\{([^{}]*)\}`) // regole più interne: il gruppo 1 è il corpo
	cssColorFn  = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|%23[0-9a-fA-F]{3,8}\b|\b(rgba?|hsla?|hwb|lab|lch|oklab|oklch|color)\(`)
	cssVarRef   = regexp.MustCompile(`var\(--[\w-]+`)
	cssWord     = regexp.MustCompile(`[A-Za-z][A-Za-z-]*`)
	cssTokenDef = regexp.MustCompile(`(--[\w-]+)\s*:([^;{}]*)`)
	cssNamed    = map[string]bool{}
)

const cssDarkMedia = "@media (prefers-color-scheme: dark)"

func init() {
	for _, n := range strings.Fields("aliceblue antiquewhite aqua aquamarine azure beige bisque black blanchedalmond blue blueviolet brown burlywood cadetblue chartreuse chocolate coral cornflowerblue cornsilk crimson cyan darkblue darkcyan darkgoldenrod darkgray darkgreen darkgrey darkkhaki darkmagenta darkolivegreen darkorange darkorchid darkred darksalmon darkseagreen darkslateblue darkslategray darkslategrey darkturquoise darkviolet deeppink deepskyblue dimgray dimgrey dodgerblue firebrick floralwhite forestgreen fuchsia gainsboro ghostwhite gold goldenrod gray green greenyellow grey honeydew hotpink indianred indigo ivory khaki lavender lavenderblush lawngreen lemonchiffon lightblue lightcoral lightcyan lightgoldenrodyellow lightgray lightgreen lightgrey lightpink lightsalmon lightseagreen lightskyblue lightslategray lightslategrey lightsteelblue lightyellow lime limegreen linen magenta maroon mediumaquamarine mediumblue mediumorchid mediumpurple mediumseagreen mediumslateblue mediumspringgreen mediumturquoise mediumvioletred midnightblue mintcream mistyrose moccasin navajowhite navy oldlace olive olivedrab orange orangered orchid palegoldenrod palegreen paleturquoise palevioletred papayawhip peachpuff peru pink plum powderblue purple rebeccapurple red rosybrown royalblue saddlebrown salmon sandybrown seagreen seashell sienna silver skyblue slateblue slategray slategrey snow springgreen steelblue tan teal thistle tomato turquoise violet wheat white whitesmoke yellow yellowgreen canvas canvastext field fieldtext buttonface buttontext highlight highlighttext graytext linktext") {
		cssNamed[n] = true
	}
}

// cssHasColor dice se un valore CSS contiene un colore scritto a mano:
// esadecimale (anche %23 negli URL data:), funzioni di colore o nomi.
func cssHasColor(v string) bool {
	v = cssVarRef.ReplaceAllString(v, "")
	if cssColorFn.MatchString(v) {
		return true
	}
	for _, w := range cssWord.FindAllString(v, -1) {
		if cssNamed[strings.ToLower(w)] {
			return true
		}
	}
	return false
}

// cssStrayColors restituisce le dichiarazioni con un colore scritto fuori
// dalle definizioni dei token (custom property --x): ogni colore deve passare
// dai token, così il blocco @media (prefers-color-scheme: dark) li cambia tutti.
// Una dichiarazione seguita da /* fisso: motivo */ sulla stessa riga è un'eccezione voluta.
func cssStrayColors(css string) []string {
	css = cssFixed.ReplaceAllString(css, "")
	css = cssComment.ReplaceAllString(css, "")
	var out []string
	for _, m := range cssRule.FindAllStringSubmatch(css, -1) {
		for _, d := range strings.Split(m[1], ";") {
			d = strings.TrimSpace(d)
			k := strings.Index(d, ":")
			if k < 0 || strings.HasPrefix(d, "--") {
				continue // vuota o definizione di un token
			}
			if cssHasColor(d[k+1:]) {
				out = append(out, d)
			}
		}
	}
	return out
}

// cssTokensWithoutDark restituisce i token con un colore scritto a mano fuori
// dai blocchi del tema scuro che nessun blocco del tema scuro ridefinisce.
func cssTokensWithoutDark(css string) []string {
	css = cssComment.ReplaceAllString(css, "")
	var dark strings.Builder
	for {
		i := strings.Index(css, cssDarkMedia)
		if i < 0 {
			break
		}
		open := i + strings.Index(css[i:], "{")
		end, depth := open, 0
		for ; end < len(css); end++ {
			if css[end] == '{' {
				depth++
			} else if css[end] == '}' {
				if depth--; depth == 0 {
					break
				}
			}
		}
		dark.WriteString(css[open:end])
		css = css[:i] + css[min(end+1, len(css)):]
	}
	inDark := map[string]bool{}
	for _, m := range cssTokenDef.FindAllStringSubmatch(dark.String(), -1) {
		inDark[m[1]] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range cssTokenDef.FindAllStringSubmatch(css, -1) {
		if cssHasColor(m[2]) && !inDark[m[1]] && !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
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
		".x{color: red}":                                       1,
		".x{border: 1px solid SlateGray}":                      1,
		".x{color: oklch(0.5 0.1 200)}":                        1,
		".x{background: url(\"data:image/svg+xml,%23fff\")}":   1,
		"#fade-in{color:var(--a)} #abc .y{color:var(--b)}":     0,
		".x{color:#000;\n/* fisso: riga dopo */}":              1,
		".red-dot{color:var(--a)} .x{font: 1rem red-hat}":      0,
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

// Una sostituzione rotta (es. "#fff3c4" → "var(--x)3c4") rende invalida la
// dichiarazione senza errori: il browser la scarta in silenzio.
func TestThemeCSSNoMangledVars(t *testing.T) {
	mangled := regexp.MustCompile(`var\(--[a-z0-9-]+\)[0-9a-zA-Z]`)
	for _, f := range []string{"../../web/static/css/app.css", "../../web/static/css/admin.css", "../../web/static/css/plancia.css", "../../web/editor/src/editor.css"} {
		b, _ := os.ReadFile(f)
		if m := mangled.FindAllString(string(b), -1); len(m) > 0 {
			t.Errorf("%s: var() seguito da testo: %q", f, m)
		}
	}
	b, _ := os.ReadFile("../../web/static/css/plancia.css")
	if !strings.Contains(string(b), ".hero-together.is-milestone { color: var(--p-milestone);") {
		t.Error("pastiglia del traguardo: colore crema --p-milestone")
	}
}

// Il colore della tile sta in style="--c:…": il trascinamento in Personalizza
// deve ripristinare l'attributo style intero, non solo background.
func TestPersonalizzaKeepsTileColor(t *testing.T) {
	b, _ := os.ReadFile("../../web/static/js/personalizza.js")
	js := string(b)
	if strings.Contains(js, "style.background") {
		t.Error("personalizza.js salva/ripristina style.background: --c della tile andrebbe perso")
	}
	if !strings.Contains(js, `getAttribute("style")`) {
		t.Error("personalizza.js deve salvare l'attributo style della tile prima del trascinamento")
	}
}

func TestCSSTokensWithoutDarkHelper(t *testing.T) {
	const dark = "@media (prefers-color-scheme: dark)"
	for css, want := range map[string]int{
		":root{--a:#fff} " + dark + "{:root{--a:#111}}":                  0,
		":root{--a:#fff; --b:#000} " + dark + "{:root{--a:#111}}":        1,
		".st{--st:#fff} " + dark + "{.st{--st:#111}}":                    0,
		":root{--gutter: 2rem; --a: var(--b)}":                           0,
		":root{--s: 0 1px rgba(0,0,0,.1)} " + dark + "{:root{--s:none}}": 0,
		":root{--a:#fff}": 1,
	} {
		if got := cssTokensWithoutDark(css); len(got) != want {
			t.Errorf("cssTokensWithoutDark(%q) = %q, attesi %d", css, got, want)
		}
	}
}

// Ogni token di colore del tema chiaro ha il suo valore scuro: un token senza
// override resterebbe chiaro al buio senza che il test sui colori se ne accorga.
func TestThemeTokensHaveDark(t *testing.T) {
	for _, f := range []string{"../../web/static/css/app.css", "../../web/static/css/plancia.css", "../../web/editor/src/editor.css"} {
		b, _ := os.ReadFile(f)
		if m := cssTokensWithoutDark(string(b)); len(m) > 0 {
			t.Errorf("%s: token di colore senza valore scuro: %v", f, m)
		}
	}
}

// Focus visibile al buio: gli anelli usano i token --p-focus*, che al buio
// diventano pieni (le velature al 25–45% sul fondo scuro restavano sotto 3:1).
func TestThemeFocusAndBackdrop(t *testing.T) {
	b, _ := os.ReadFile("../../web/static/css/plancia.css")
	css := string(b)
	for _, sel := range []string{".tile-main:focus-visible", ".carousel:focus-visible", ".news-expand > summary:focus-visible > span", "body.personalizza .category > .section-title:focus-visible", ".search:focus-within"} {
		i := strings.Index(css, sel+" {")
		if i < 0 {
			t.Errorf("manca %s", sel)
			continue
		}
		rule := css[i : i+strings.Index(css[i:], "}")]
		if !strings.Contains(rule, "var(--p-focus") {
			t.Errorf("%s: l'anello di focus deve usare un token --p-focus*", sel)
		}
	}
	// ::backdrop non eredita dalla pagina nei motori meno recenti: i suoi token stanno su ::backdrop.
	if strings.Count(css, "::backdrop { --p-backdrop:") != 2 {
		t.Error("plancia.css: token del backdrop su ::backdrop, chiaro e scuro")
	}
	if strings.Contains(css, "--p-on-blue") {
		t.Error("plancia.css: --p-on-blue rinominato in --p-on-fill (vale anche per i fondi rossi e verdi)")
	}
}

// Ricerca a tendina: l'icona dell'app passa da --c e al buio sta su un riquadro chiaro.
func TestSearchIconTheme(t *testing.T) {
	b, _ := os.ReadFile("../../web/static/js/dashboard.js")
	js := string(b)
	if strings.Contains(js, "ic.style.color = it.c") || !strings.Contains(js, `ic.style.setProperty("--c", it.c)`) {
		t.Error("dashboard.js: colore dell'icona dell'app come --c, non color inline")
	}
	c, _ := os.ReadFile("../../web/static/css/plancia.css")
	if !strings.Contains(string(c), ".search-option .search-app-icon { color: var(--c); }") {
		t.Error("plancia.css: .search-app-icon usa --c")
	}
}
