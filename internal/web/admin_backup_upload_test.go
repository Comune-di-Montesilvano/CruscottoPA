package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/backup"
)

func postRaw(t *testing.T, s *Server, target string, body []byte, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func startUpload(t *testing.T, s *Server, c *http.Cookie) (string, int) {
	t.Helper()
	rec := postRaw(t, s, "/admin/backup/upload", nil, c)
	var out struct {
		ID    string `json:"id"`
		Chunk int    `json:"chunk"`
	}
	if rec.Code != 200 || json.NewDecoder(rec.Body).Decode(&out) != nil || out.ID == "" {
		t.Fatalf("avvio upload: %d %s", rec.Code, rec.Body)
	}
	return out.ID, out.Chunk
}

func TestUploadRestoreFlow(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	db.CreateCategory("Prima")
	info, _ := s.backup.Create(backup.KindManual)
	p, _ := s.backup.Path(info.Name)
	archive, _ := os.ReadFile(p)
	db.CreateCategory("Dopo")

	// conferma sbagliata: upload scartato, nessun ripristino
	id, chunk := startUpload(t, s, c)
	if chunk != backup.ChunkSize {
		t.Fatalf("chunk annunciato %d", chunk)
	}
	if rec := postRaw(t, s, "/admin/backup/upload/"+id+"/chunk?n=1", archive, c); rec.Code != http.StatusConflict {
		t.Fatalf("pezzo fuori sequenza: %d", rec.Code)
	}
	if rec := postRaw(t, s, "/admin/backup/upload/"+id+"/chunk?n=0", archive, c); rec.Code != http.StatusNoContent {
		t.Fatalf("pezzo 0: %d %s", rec.Code, rec.Body)
	}
	rec := do(t, s, "POST", "/admin/backup/upload/"+id+"/fine", url.Values{"conferma": {"no"}}, c, nil)
	if !invalid(rec) || !strings.Contains(rec.Body.String(), "digita RIPRISTINA") {
		t.Fatalf("conferma errata: %d\n%s", rec.Code, rec.Body)
	}

	// flusso completo
	id, _ = startUpload(t, s, c)
	for n := 0; n*backup.ChunkSize < len(archive); n++ {
		end := min((n+1)*backup.ChunkSize, len(archive))
		if rec := postRaw(t, s, "/admin/backup/upload/"+id+"/chunk?n="+strconv.Itoa(n), archive[n*backup.ChunkSize:end], c); rec.Code != http.StatusNoContent {
			t.Fatalf("pezzo %d: %d", n, rec.Code)
		}
	}
	rec = do(t, s, "POST", "/admin/backup/upload/"+id+"/fine", url.Values{"conferma": {"RIPRISTINA"}}, c, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "data-restarting") {
		t.Fatalf("fine: %d\n%s", rec.Code, rec.Body)
	}
	waitExit(t, s)
	if names := categoryNames(t, s.cfg.DBPath); !strings.Contains(names, "Prima") || strings.Contains(names, "Dopo") {
		t.Fatalf("dati non ripristinati: %s", names)
	}
}

func TestUploadRejectsInvalidArchive(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	id, _ := startUpload(t, s, c)
	postRaw(t, s, "/admin/backup/upload/"+id+"/chunk?n=0", []byte("non è un backup"), c)
	rec := do(t, s, "POST", "/admin/backup/upload/"+id+"/fine", url.Values{"conferma": {"RIPRISTINA"}}, c, nil)
	if !invalid(rec) || !strings.Contains(rec.Body.String(), "Archivio rifiutato") {
		t.Fatalf("archivio non valido: %d\n%s", rec.Code, rec.Body)
	}
	select {
	case <-serverExits[s]:
		t.Fatal("exit chiamato per un archivio rifiutato")
	default:
	}
}

func TestUploadChunkErrors(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	if rec := postRaw(t, s, "/admin/backup/upload/sconosciuto/chunk?n=0", []byte("x"), c); rec.Code != http.StatusNotFound {
		t.Fatalf("id sconosciuto: %d", rec.Code)
	}
	id, _ := startUpload(t, s, c)
	t.Cleanup(func() { // Windows non elimina file aperti: chiude la sessione prima della TempDir
		if p, err := s.backup.FinishUpload(id); err == nil {
			os.Remove(p)
		}
	})
	if rec := postRaw(t, s, "/admin/backup/upload/"+id+"/chunk?n=zero", []byte("x"), c); rec.Code != http.StatusBadRequest {
		t.Fatalf("n non numerico: %d", rec.Code)
	}
	if rec := postRaw(t, s, "/admin/backup/upload/"+id+"/chunk?n=0", make([]byte, backup.ChunkSize+1), c); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("pezzo troppo grande: %d", rec.Code)
	}
}
