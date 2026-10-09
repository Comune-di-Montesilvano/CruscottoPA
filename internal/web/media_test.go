package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

var pdfBytes = []byte("%PDF-1.7\n%%EOF")

func mediaPost(t *testing.T, s *Server, c *http.Cookie, target, ctype string, body []byte) map[string]any {
	t.Helper()
	req := httptest.NewRequest("POST", target, bytes.NewReader(body))
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%s: status %d (deve essere sempre 200)", target, rec.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: JSON non valido: %s", target, rec.Body)
	}
	return out
}

// uploadMedia carica data a pezzi come fa admin.js; restituisce la risposta
// finale (o il primo errore).
func uploadMedia(t *testing.T, s *Server, c *http.Cookie, tipo string, data []byte) map[string]any {
	t.Helper()
	start := mediaPost(t, s, c, "/admin/media", "application/x-www-form-urlencoded", []byte("tipo="+tipo))
	if start["ok"] != true {
		t.Fatalf("avvio: %v", start)
	}
	id := start["id"].(string)
	chunk := int(start["chunk"].(float64))
	for n, off := 0, 0; off < len(data); n, off = n+1, off+chunk {
		end := min(off+chunk, len(data))
		r := mediaPost(t, s, c, "/admin/media/"+id+"/pezzo?n="+itoa(int64(n)), "application/octet-stream", data[off:end])
		if r["ok"] != true {
			return r
		}
	}
	return mediaPost(t, s, c, "/admin/media/"+id+"/fine", "application/x-www-form-urlencoded", nil)
}

func TestMediaUploadImage(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	big := append(append([]byte{}, pngBytes...), make([]byte, 700<<10)...) // più di un pezzo
	out := uploadMedia(t, s, c, "immagine", big)
	u, _ := out["url"].(string)
	if out["ok"] != true || !strings.HasPrefix(u, "/uploads/guide/") || !strings.HasSuffix(u, ".png") {
		t.Fatalf("upload: %v", out)
	}
	rec := do(t, s, "GET", u, nil, nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") || rec.Body.Len() != len(big) {
		t.Fatalf("servizio: %d %q %d", rec.Code, rec.Header().Get("Content-Security-Policy"), rec.Body.Len())
	}
	if entries, _ := os.ReadDir(filepath.Join(s.cfg.UploadDir, ".tmp")); len(entries) != 0 {
		t.Errorf("file temporanei rimasti: %d", len(entries))
	}
}

func TestMediaUploadRejects(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)
	if out := uploadMedia(t, s, c, "immagine", svg); out["ok"] != false {
		t.Errorf("SVG come immagine: %v", out)
	}
	if out := uploadMedia(t, s, c, "immagine", append(append([]byte{}, pngBytes...), make([]byte, 2<<20)...)); out["ok"] != false {
		t.Errorf("immagine oltre 2 MB: %v", out)
	}
	if out := uploadMedia(t, s, c, "pdf", pngBytes); out["ok"] != false {
		t.Errorf("PNG come PDF: %v", out)
	}
	out := uploadMedia(t, s, c, "pdf", pdfBytes)
	name, _ := out["name"].(string)
	if out["ok"] != true || !strings.HasSuffix(name, ".pdf") {
		t.Fatalf("PDF valido: %v", out)
	}
	// Un PDF non si serve da /uploads/guide (solo da /guide/{id}/pdf, con la visibilità).
	if rec := do(t, s, "GET", "/uploads/guide/"+name, nil, nil, nil); rec.Code == 200 {
		t.Error("PDF servito da /uploads/guide")
	}
	if out := mediaPost(t, s, c, "/admin/media/nonesiste/pezzo?n=0", "application/octet-stream", []byte("x")); out["ok"] != false {
		t.Errorf("upload inesistente: %v", out)
	}
	start := mediaPost(t, s, c, "/admin/media", "application/x-www-form-urlencoded", []byte("tipo=immagine"))
	id := start["id"].(string)
	if out := mediaPost(t, s, c, "/admin/media/"+id+"/pezzo?n=3", "application/octet-stream", pngBytes); out["ok"] != false {
		t.Errorf("pezzo fuori ordine: %v", out)
	}
}

func TestMediaRequiresAdmin(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(t, s, "POST", "/admin/media", url.Values{"tipo": {"pdf"}}, nil, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatal("upload senza login")
	}
}

func TestCleanMediaKeepsRecentAndReferenced(t *testing.T) {
	s, db := newTestServer(t, nil)
	dir := s.uploadDir(uploadGuide)
	os.MkdirAll(dir, 0o750)
	old := fixedNow.Add(-25 * time.Hour)
	mk := func(name string, mod time.Time) {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("x"), 0o640)
		os.Chtimes(p, mod, mod)
	}
	orphan := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png"
	recent := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.png"
	used := "cccccccccccccccccccccccccccccccc.png"
	other := "not-ours.png"
	mk(orphan, old)
	mk(recent, fixedNow.Add(-time.Hour))
	mk(used, old)
	mk(other, old)
	db.CreateGuide(database.Guide{Title: "G", Kind: database.GuideKindMarkdown, Body: "![x](/uploads/guide/" + used + ")", Enabled: true})
	s.cleanMedia()
	for name, want := range map[string]bool{orphan: false, recent: true, used: true, other: true} {
		_, err := os.Stat(filepath.Join(dir, name))
		if (err == nil) != want {
			t.Errorf("%s: presente=%v, atteso %v", name, err == nil, want)
		}
	}
}

func TestAlertDeleteCleansMedia(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	dir := s.uploadDir(uploadGuide)
	os.MkdirAll(dir, 0o750)
	name := "dddddddddddddddddddddddddddddddd.png"
	p := filepath.Join(dir, name)
	os.WriteFile(p, []byte("x"), 0o640)
	old := fixedNow.Add(-25 * time.Hour)
	os.Chtimes(p, old, old)
	id, _ := db.CreateAlert(database.Alert{Title: "A", Body: "![x](/uploads/guide/" + name + ")", Level: "news", StartsAt: fixedNow, CreatedAt: fixedNow})
	do(t, s, "POST", "/admin/avvisi/"+itoa(id)+"/elimina", nil, c, hx)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("immagine dell'avviso eliminato ancora presente")
	}
}

// Review I2: un pezzo rifiutato libera subito il posto e il file temporaneo,
// altrimenti 8 immagini troppo grandi bloccherebbero gli upload per un'ora.
func TestMediaFailedUploadFreesSlot(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	big := append(append([]byte{}, pngBytes...), make([]byte, 3<<20)...)
	for i := 0; i < maxMediaUploads+1; i++ {
		if out := uploadMedia(t, s, c, "immagine", big); out["ok"] != false {
			t.Fatalf("immagine troppo grande accettata: %v", out)
		}
	}
	if out := uploadMedia(t, s, c, "pdf", pdfBytes); out["ok"] != true {
		t.Fatalf("dopo upload falliti: %v", out)
	}
	if entries, _ := os.ReadDir(filepath.Join(s.cfg.UploadDir, ".tmp")); len(entries) != 0 {
		t.Errorf("file temporanei rimasti: %d", len(entries))
	}
}

// Un pezzo che arriva lento (rete) non blocca gli altri caricamenti: il lock
// non resta tenuto mentre si legge il corpo.
func TestMediaSlowChunkDoesNotBlock(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	start := mediaPost(t, s, c, "/admin/media", "application/x-www-form-urlencoded", []byte("tipo=immagine"))
	id := start["id"].(string)
	pr, pw := io.Pipe()
	slow := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest("POST", "/admin/media/"+id+"/pezzo?n=0", pr)
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.AddCookie(c)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		slow <- rec
	}()
	pw.Write(pngBytes[:4]) // il pezzo è iniziato e il server lo sta leggendo

	done := make(chan map[string]any, 1)
	go func() { done <- uploadMedia(t, s, c, "immagine", pngBytes) }()
	select {
	case out := <-done:
		if out["ok"] != true {
			t.Fatalf("altro caricamento: %v", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("un pezzo lento blocca gli altri caricamenti")
	}
	// Mentre il pezzo è in lettura, "fine" sullo stesso caricamento non lo chiude.
	if out := mediaPost(t, s, c, "/admin/media/"+id+"/fine", "application/x-www-form-urlencoded", nil); out["ok"] == true {
		t.Fatalf("fine durante un pezzo: %v", out)
	}
	pw.Write(pngBytes[4:])
	pw.Close()
	if rec := <-slow; !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("pezzo lento: %s", rec.Body)
	}
	if out := mediaPost(t, s, c, "/admin/media/"+id+"/fine", "application/x-www-form-urlencoded", nil); out["ok"] != true {
		t.Fatalf("fine dopo il pezzo: %v", out)
	}
}

// All'avvio la cartella temporanea si svuota: un caricamento interrotto da un
// riavvio non verrebbe mai più completato.
func TestStartupClearsTmp(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "uploads")
	for _, p := range []string{".tmp/0123456789abcdef0123456789abcdef", ".tmp/ticket/x"} {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o750)
		os.WriteFile(filepath.Join(dir, p), []byte("x"), 0o640)
	}
	newTestServerWith(t, nil, func(o *Options) { o.Config.UploadDir = dir })
	if entries, err := os.ReadDir(filepath.Join(dir, ".tmp")); err == nil && len(entries) != 0 {
		t.Fatalf("temporanei rimasti: %d", len(entries))
	}
}
