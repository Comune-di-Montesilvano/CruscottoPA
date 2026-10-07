package web

import (
	"bytes"
	"encoding/json"
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
