package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func postMultipart(t *testing.T, s *Server, target string, fields map[string]string, file []byte, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return postMultipartFile(t, s, target, fields, "icon_file", file, c)
}

func postMultipartFile(t *testing.T, s *Server, target string, fields map[string]string, fileField string, file []byte, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	if file != nil {
		fw, _ := mw.CreateFormFile(fileField, "file.bin")
		fw.Write(file)
	}
	mw.Close()
	req := httptest.NewRequest("POST", target, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func appFields(db *database.DB, overrides map[string]string) map[string]string {
	cats, _ := db.ListCategories()
	f := map[string]string{
		"title": "Sicraweb", "description": "Protocollo e atti", "url": "https://sicraweb.local",
		"category_id": itoa(cats[0].ID), "icon_kind": "pack", "icon_value_pack": "description",
		"icon_color": "#475569", "enabled": "1",
	}
	for k, v := range overrides {
		f[k] = v
	}
	return f
}

func TestCreateAppWithPackIcon(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	rec := postMultipart(t, s, "/admin/app", appFields(db, nil), nil, c)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Sicraweb") {
		t.Fatalf("create: %d\n%s", rec.Code, rec.Body)
	}
	apps, _ := db.ListApps()
	a := apps[len(apps)-1]
	if a.Title != "Sicraweb" || a.IconKind != "pack" || a.IconValue != "description" || !a.Enabled {
		t.Fatalf("app salvata: %+v", a)
	}
}

func TestCreateAppValidation(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	rec := postMultipart(t, s, "/admin/app", appFields(db, map[string]string{
		"title": "", "url": "www.comune.it", "icon_value_pack": "non_esiste_xyz", "category_id": "999",
	}), nil, c)
	body := rec.Body.String()
	if !invalid(rec) {
		t.Fatalf("atteso 422, ottenuto %d", rec.Code)
	}
	for _, want := range []string{"Campo obbligatorio.", "Inserisci l&#39;indirizzo completo", "Scegli un&#39;icona dal catalogo.", "Scegli una categoria."} {
		if !strings.Contains(body, want) {
			t.Errorf("manca il messaggio %q", want)
		}
	}
	if !strings.Contains(body, `value="www.comune.it"`) {
		t.Error("il form deve conservare i valori inseriti")
	}
}

func TestAppUploadLifecycle(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	apps, _ := db.ListApps()
	id := apps[0].ID
	path := "/admin/app/" + itoa(id)

	// 1) upload senza file → errore
	rec := postMultipart(t, s, path, appFields(db, map[string]string{"icon_kind": "upload"}), nil, c)
	if !invalid(rec) || !strings.Contains(rec.Body.String(), "Carica un file") {
		t.Fatalf("upload senza file: %d", rec.Code)
	}
	// 2) tipo non ammesso
	rec = postMultipart(t, s, path, appFields(db, map[string]string{"icon_kind": "upload"}), []byte("ciao"), c)
	if !invalid(rec) || !strings.Contains(rec.Body.String(), "Formato non ammesso") {
		t.Fatalf("tipo non ammesso: %d", rec.Code)
	}
	// 3) troppo grande
	big := append(append([]byte{}, pngBytes...), make([]byte, maxIconBytes)...)
	rec = postMultipart(t, s, path, appFields(db, map[string]string{"icon_kind": "upload"}), big, c)
	if !invalid(rec) || !strings.Contains(rec.Body.String(), "troppo grande") {
		t.Fatalf("file grande: %d", rec.Code)
	}
	// 4) PNG valido
	rec = postMultipart(t, s, path, appFields(db, map[string]string{"icon_kind": "upload"}), pngBytes, c)
	if rec.Code != 200 {
		t.Fatalf("upload PNG: %d\n%s", rec.Code, rec.Body)
	}
	a, _ := db.GetApp(id)
	file := filepath.Join(s.uploadDir(uploadIcons), a.IconValue)
	if a.IconKind != "upload" || !uploadFileRe.MatchString(a.IconValue) {
		t.Fatalf("icona caricata: %+v", a)
	}
	// File sparito o illeggibile: il JS ripiega sulle iniziali, in plancia e nell'admin.
	img := `src="/uploads/icons/` + a.IconValue + `" alt="" data-fallback="`
	if body := do(t, s, "GET", "/", nil, nil, nil).Body.String(); !strings.Contains(body, img) {
		t.Fatalf("ripiego sulle iniziali in plancia:\n%s", body)
	}
	if body := do(t, s, "GET", "/admin/app", nil, c, nil).Body.String(); !strings.Contains(body, img) {
		t.Fatalf("ripiego sulle iniziali nell'admin:\n%s", body)
	}
	if js, _ := os.ReadFile("../../web/static/js/admin.js"); !strings.Contains(string(js), "dataset.fallback") {
		t.Fatal("admin.js deve gestire il ripiego delle icone")
	}
	// 5) salvataggio senza nuovo file → mantiene l'icona
	postMultipart(t, s, path, appFields(db, map[string]string{"icon_kind": "upload", "title": "Rubrica 2"}), nil, c)
	if b, _ := db.GetApp(id); b.IconValue != a.IconValue {
		t.Fatal("senza nuovo file l'icona caricata va mantenuta")
	}
	// 6) passaggio a icona del pacchetto → file cancellato
	postMultipart(t, s, path, appFields(db, nil), nil, c)
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("il file della vecchia icona va cancellato")
	}
}

func TestDeleteAppWarnsAboutGuides(t *testing.T) { // Review Focus #4
	s, db := newTestServer(t, nil)
	c := login(t, s)
	apps, _ := db.ListApps()
	rubrica := apps[0]
	rubrica.URL = "https://rubrica.local"
	db.UpdateApp(rubrica)
	db.CreateGuide(database.Guide{AppID: id64(rubrica.ID), Title: "Cercare un interno", Kind: "link", URL: "https://wiki/x", Enabled: true})

	page := do(t, s, "GET", "/admin/app", nil, c, nil).Body.String()
	if !strings.Contains(page, "La sua guida diventerà generale.") {
		t.Fatalf("la conferma di eliminazione deve avvisare delle guide collegate\n%s", page)
	}
	if rec := do(t, s, "POST", "/admin/app/"+itoa(rubrica.ID)+"/elimina", nil, c, hx); rec.Code != 200 {
		t.Fatalf("elimina: %d", rec.Code)
	}
	dash := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	if !strings.Contains(dash, `class="widget guides"`) || !strings.Contains(dash, "Cercare un interno") {
		t.Fatal("dopo l'eliminazione la guida deve comparire tra le generali")
	}
}

func TestAppEditMoveAndIconSearch(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	apps, _ := db.ListApps()
	webmail := apps[1]

	rec := do(t, s, "GET", "/admin/app?modifica="+itoa(webmail.ID), nil, c, nil)
	if !strings.Contains(rec.Body.String(), `value="Webmail"`) {
		t.Fatal("?modifica= deve aprire il form precompilato")
	}
	if rec := do(t, s, "GET", "/admin/app/"+itoa(webmail.ID)+"/modifica", nil, c, hx); !strings.Contains(rec.Body.String(), `hx-post="/admin/app/`+itoa(webmail.ID)+`"`) {
		t.Fatal("modifica: form non puntato all'app")
	}
	do(t, s, "POST", "/admin/app/"+itoa(webmail.ID)+"/sposta", url_("dir", "up"), c, hx)
	if apps, _ := db.ListApps(); apps[0].ID != webmail.ID {
		t.Fatal("sposta su non applicato")
	}

	rec = do(t, s, "GET", "/admin/icone?q=mail", nil, c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `data-icon="mail"`) {
		t.Fatalf("ricerca icone: %d\n%s", rec.Code, rec.Body)
	}
}

// Un'icona via http:// sarebbe bloccata dalla CSP (img-src https:): rifiutarla nel form.
func TestAppIconURLMustBeHTTPS(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	apps, _ := db.ListApps()
	rec := postMultipart(t, s, "/admin/app/"+itoa(apps[0].ID), appFields(db, map[string]string{"icon_kind": "url", "icon_value_url": "http://example.it/logo.png"}), nil, c)
	if !invalid(rec) || !strings.Contains(rec.Body.String(), "https://") {
		t.Fatalf("icona http: atteso 422, ottenuto %d", rec.Code)
	}
}

// Riquadro dell'icona colorato: serve per i loghi bianchi (es. PagoPA).
func TestAppIconBackground(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	rec := postMultipart(t, s, "/admin/app", appFields(db, map[string]string{"icon_bg_on": "1", "icon_bg": "#0066CC"}), nil, c)
	if rec.Code != 200 {
		t.Fatalf("create: %d\n%s", rec.Code, rec.Body)
	}
	apps, _ := db.ListApps()
	a := apps[len(apps)-1]
	if a.IconBg != "#0066cc" {
		t.Fatalf("sfondo del riquadro: %q", a.IconBg)
	}
	if body := do(t, s, "GET", "/", nil, nil, nil).Body.String(); !strings.Contains(body, `class="tile-icon" style="background:#0066cc`) {
		t.Fatalf("riquadro colorato in plancia:\n%s", body)
	}
	if body := do(t, s, "GET", "/admin/app/"+itoa(a.ID)+"/modifica", nil, c, hx).Body.String(); !strings.Contains(body, `name="icon_bg_on" value="1" checked`) || !strings.Contains(body, `name="icon_bg" value="#0066cc"`) {
		t.Fatalf("form di modifica:\n%s", body)
	}
	// Casella tolta: torna il riquadro bianco predefinito.
	postMultipart(t, s, "/admin/app/"+itoa(a.ID), appFields(db, map[string]string{"icon_bg": "#0066cc"}), nil, c)
	if got, _ := db.GetApp(a.ID); got.IconBg != "" {
		t.Fatalf("senza casella: %q", got.IconBg)
	}
	if rec := postMultipart(t, s, "/admin/app", appFields(db, map[string]string{"icon_bg_on": "1", "icon_bg": "rosso"}), nil, c); !invalid(rec) {
		t.Fatalf("colore non valido: %d", rec.Code)
	}
}
