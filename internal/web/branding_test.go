package web

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

const favicon = `<link rel="icon" href="/static/img/favicon.ico">`

func TestPagesWithoutBranding(t *testing.T) {
	s, _ := newTestServer(t, nil)
	dash := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	for _, want := range []string{"<title>CruscottoPA</title>", favicon, `src="/static/img/logo-on-dark.svg"`, `src="/static/img/logo-on-light.svg"`} {
		if !strings.Contains(dash, want) {
			t.Errorf("plancia senza branding: manca %q", want)
		}
	}
	if strings.Contains(dash, "/uploads/branding/") {
		t.Error("plancia senza branding: non deve esserci il logo dell'ente")
	}
}

func TestPagesShowEnteBranding(t *testing.T) {
	s, _ := newTestServer(t, nil)
	logo := strings.Repeat("a", 32) + ".png"
	s.branding.Store(&database.Branding{EnteName: "Comune dell'Aquila <b>", LogoFile: logo})
	escaped := "Comune dell&#39;Aquila &lt;b&gt;"
	c := login(t, s)

	pages := map[string]string{
		"/":            do(t, s, "GET", "/", nil, nil, nil).Body.String(),
		"/avvisi":      do(t, s, "GET", "/avvisi", nil, nil, nil).Body.String(),
		"/admin/login": do(t, s, "GET", "/admin/login", nil, nil, nil).Body.String(),
		"/admin":       do(t, s, "GET", "/admin", nil, c, nil).Body.String(),
	}
	for path, body := range pages {
		if !strings.Contains(body, favicon) {
			t.Errorf("%s: manca la favicon", path)
		}
		if !strings.Contains(body, escaped) {
			t.Errorf("%s: manca il nome dell'ente escapato", path)
		}
		if strings.Contains(body, "Aquila <b>") {
			t.Errorf("%s: nome dell'ente non escapato", path)
		}
	}
	if !strings.Contains(pages["/"], "<title>CruscottoPA · "+escaped+"</title>") {
		t.Error("plancia: <title> senza il nome dell'ente")
	}
	for _, path := range []string{"/", "/admin/login"} {
		if !strings.Contains(pages[path], `src="/uploads/branding/`+logo+`"`) {
			t.Errorf("%s: manca il logo dell'ente", path)
		}
	}
	if strings.Contains(pages["/"], "logo-on-dark.svg") {
		t.Error("plancia con branding: in testata va il logo dell'ente, non quello di CruscottoPA")
	}
}

func TestFaviconServed(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(t, s, "GET", "/favicon.ico", nil, nil, nil)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/x-icon" || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("/favicon.ico: %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Cache-Control"))
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte{0, 0, 1, 0}) {
		t.Fatal("/favicon.ico non è un file ICO")
	}
}

// CruscottoPA è riusabile da altri enti: nessun ente scritto nei template.
func TestTemplatesHaveNoEnteHardcoded(t *testing.T) {
	files, _ := filepath.Glob("../../web/templates/*.html")
	if len(files) == 0 {
		t.Fatal("nessun template trovato")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(bytes.ToLower(data), []byte("montesilvano")) {
			t.Errorf("%s: nome dell'ente scritto nel template", filepath.Base(f))
		}
	}
}

// Con il nome scritto accanto, il logo è decorativo: un alt uguale al nome
// farebbe leggere il nome due volte agli screen reader.
func TestEnteLogoAlt(t *testing.T) {
	s, _ := newTestServer(t, nil)
	logo := strings.Repeat("a", 32) + ".png"
	img := `<img class="ente-logo" src="/uploads/branding/` + logo + `" alt=`

	s.branding.Store(&database.Branding{EnteName: "Comune di Esempio", LogoFile: logo})
	if dash := do(t, s, "GET", "/", nil, nil, nil).Body.String(); !strings.Contains(dash, img+`"">`) {
		t.Error("logo con nome accanto: atteso alt vuoto")
	}
	s.branding.Store(&database.Branding{LogoFile: logo})
	if dash := do(t, s, "GET", "/", nil, nil, nil).Body.String(); !strings.Contains(dash, img+`"Logo dell'ente">`) {
		t.Error("logo senza nome: atteso alt \"Logo dell'ente\"")
	}
}
