package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func postEnte(t *testing.T, s *Server, fields map[string]string, logo []byte, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return postMultipartFile(t, s, "/admin/ente", fields, "logo_file", logo, c)
}

func brandingFiles(t *testing.T, s *Server) []string {
	t.Helper()
	entries, _ := os.ReadDir(s.uploadDir(uploadBranding))
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestEnteRequiresAdmin(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if rec := do(t, s, "GET", "/admin/ente", nil, nil, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("GET senza sessione: atteso 303, ottenuto %d", rec.Code)
	}
	if rec := postEnte(t, s, map[string]string{"ente_name": "X"}, nil, nil); rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "/admin/login" {
		t.Fatalf("POST HTMX senza sessione: atteso 401, ottenuto %d", rec.Code)
	}
	if s.ente().EnteName != "" {
		t.Fatal("POST senza sessione ha modificato il branding")
	}
}

func TestEntePage(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	rec := do(t, s, "GET", "/admin/ente", nil, c, nil)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `name="ente_name"`) || !strings.Contains(body, `name="logo_file"`) || !strings.Contains(body, `href="/admin/ente" aria-current="page"`) {
		t.Fatalf("pagina ente: %d\n%s", rec.Code, body)
	}
}

func TestEnteSaveNameAndLogoLifecycle(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)

	// 1) solo il nome
	rec := postEnte(t, s, map[string]string{"ente_name": "  Comune di Esempio  "}, nil, c)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Salvato") {
		t.Fatalf("salva nome: %d\n%s", rec.Code, rec.Body)
	}
	b, _ := db.GetBranding()
	if b.EnteName != "Comune di Esempio" || b.UpdatedBy != "mrossi" || !b.UpdatedAt.Equal(fixedNow) || s.ente() != b {
		t.Fatalf("branding salvato: %+v, cache %+v", b, s.ente())
	}
	if !strings.Contains(do(t, s, "GET", "/", nil, nil, nil).Body.String(), "Comune di Esempio") {
		t.Fatal("la plancia non mostra il nuovo nome")
	}

	// 2) logo PNG
	rec = postEnte(t, s, map[string]string{"ente_name": "Comune di Esempio"}, pngBytes, c)
	first := s.ente().LogoFile
	if rec.Code != 200 || !uploadFileRe.MatchString(first) {
		t.Fatalf("upload logo: %d %q", rec.Code, first)
	}
	if _, err := os.Stat(filepath.Join(s.uploadDir(uploadBranding), first)); err != nil {
		t.Fatal("file del logo non scritto")
	}
	if !strings.Contains(do(t, s, "GET", "/", nil, nil, nil).Body.String(), `src="/uploads/branding/`+first+`"`) {
		t.Fatal("la plancia non mostra il logo")
	}

	// 3) solo il nome: il logo resta
	rec = postEnte(t, s, map[string]string{"ente_name": "Comune di Prova"}, nil, c)
	if rec.Code != 200 || s.ente().LogoFile != first || s.ente().EnteName != "Comune di Prova" {
		t.Fatalf("salvare il nome ha toccato il logo: %d %+v", rec.Code, s.ente())
	}
	if files := brandingFiles(t, s); len(files) != 1 {
		t.Fatalf("file in branding: %v", files)
	}

	// 4) "Rimuovi" + nuovo file: vince il nuovo, il vecchio è cancellato
	rec = postEnte(t, s, map[string]string{"ente_name": "Comune di Prova", "remove_logo": "1"}, pngBytes, c)
	second := s.ente().LogoFile
	if rec.Code != 200 || second == "" || second == first {
		t.Fatalf("sostituzione: %d %q", rec.Code, second)
	}
	if files := brandingFiles(t, s); len(files) != 1 || files[0] != second {
		t.Fatalf("dopo la sostituzione: %v", files)
	}

	// 5) "Rimuovi" senza file
	rec = postEnte(t, s, map[string]string{"ente_name": "Comune di Prova", "remove_logo": "1"}, nil, c)
	if rec.Code != 200 || s.ente().LogoFile != "" {
		t.Fatalf("rimozione: %d %+v", rec.Code, s.ente())
	}
	if files := brandingFiles(t, s); len(files) != 0 {
		t.Fatalf("dopo la rimozione: %v", files)
	}
}

func TestEnteValidation(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	big := append(append([]byte{}, pngBytes...), make([]byte, maxIconBytes)...)
	for _, tc := range []struct {
		name string
		logo []byte
		msg  string
	}{
		{"Comune di Esempio", []byte("GIF89a......"), "Formato non ammesso"},
		{"Comune di Esempio", big, "troppo grande"},
		{strings.Repeat("x", 121), pngBytes, "Massimo 120 caratteri"},
	} {
		rec := postEnte(t, s, map[string]string{"ente_name": tc.name}, tc.logo, c)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), tc.msg) {
			t.Fatalf("%q: atteso 422, ottenuto %d\n%s", tc.msg, rec.Code, rec.Body)
		}
	}
	if b, _ := db.GetBranding(); b.EnteName != "" || s.ente().EnteName != "" {
		t.Fatalf("con errori non si salva nulla: %+v", b)
	}
	if files := brandingFiles(t, s); len(files) != 0 {
		t.Fatalf("file orfani dopo errori: %v", files)
	}
}

// Motore di ricerca e sito dell'ente per la ricerca della plancia.
func TestEnteWebSearch(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	if !strings.Contains(body, `data-web="https://www.google.com/search?q=%s"`) || !strings.Contains(body, `data-web-name="Google"`) || strings.Contains(body, "data-site-search=") {
		t.Fatalf("predefiniti in plancia:\n%s", body)
	}
	rec := postMultipart(t, s, "/admin/ente", map[string]string{"ente_name": "Comune", "web_search": "https://www.bing.com/search?q=%s", "site_search": "https://www.comune.example.it/content/search?SearchText=%s"}, nil, c)
	if rec.Code != 200 {
		t.Fatalf("salva: %d\n%s", rec.Code, rec.Body)
	}
	if b, _ := db.GetBranding(); b.WebSearch != "https://www.bing.com/search?q=%s" || b.SiteSearch != "https://www.comune.example.it/content/search?SearchText=%s" {
		t.Fatalf("salvati: %+v", b)
	}
	body = do(t, s, "GET", "/", nil, nil, nil).Body.String()
	if !strings.Contains(body, `data-web-name="Bing"`) || !strings.Contains(body, `data-site-search="https://www.comune.example.it/content/search?SearchText=%s"`) {
		t.Fatalf("plancia dopo il salvataggio:\n%s", body)
	}
	for _, bad := range []map[string]string{
		{"web_search": "https://www.google.com/search"},                                                             // manca %s
		{"web_search": "javascript:alert('%s')"},                                                                    // non https
		{"web_search": "https://x.example/?q=%s&r=%s"},                                                              // %s due volte
		{"web_search": "https://www.google.com/search?q=%s", "site_search": "https://www.comune.example.it/search"}, // ricerca del sito senza %s
	} {
		bad["ente_name"] = "Comune"
		if rec := postMultipart(t, s, "/admin/ente", bad, nil, c); rec.Code != 422 {
			t.Errorf("%v: atteso 422, %d", bad, rec.Code)
		}
	}
	// Campo assente (form vecchio): resta Google.
	postMultipart(t, s, "/admin/ente", map[string]string{"ente_name": "Comune"}, nil, c)
	if b, _ := db.GetBranding(); b.WebSearch != database.DefaultWebSearch {
		t.Fatalf("senza campo: %q", b.WebSearch)
	}
	js, _ := os.ReadFile("../../web/static/js/dashboard.js")
	for _, want := range []string{"input.dataset.web", "input.dataset.siteSearch", "encodeURIComponent(", "sul sito dell'ente"} {
		if !strings.Contains(string(js), want) {
			t.Errorf("dashboard.js: manca %q", want)
		}
	}
}

// Il testo della barra dice che si cerca anche sul web.
func TestSearchPlaceholderMentionsWeb(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if body := do(t, s, "GET", "/", nil, nil, nil).Body.String(); !strings.Contains(body, `placeholder="Cerca in plancia o sul web…"`) {
		t.Fatal("placeholder senza ricerca sul web")
	}
}
