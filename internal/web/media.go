package web

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// Immagini e PDF di avvisi e guide, caricati a pezzi (il proxy taglia le
// richieste oltre 1 MB). Le risposte sono sempre 200 + JSON: un 4xx lo
// sostituirebbe il proxy.
const (
	uploadGuide     = "guide"
	mediaChunk      = 512 << 10
	maxImageBytes   = 2 << 20
	maxPDFBytes     = 20 << 20
	maxMediaUploads = 8
	mediaUploadTTL  = time.Hour
	mediaOrphanAge  = 24 * time.Hour
)

var (
	guideMediaRe = regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpg|webp|pdf)$`)
	guideImageRe = regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpg|webp)$`)
)

// mediaUpload: un caricamento in corso, scritto in UPLOAD_DIR/.tmp (escluso dal backup).
type mediaUpload struct {
	path    string
	pdf     bool
	next    int
	size    int64
	started time.Time
}

type mediaUploads struct {
	mu   sync.Mutex
	byID map[string]*mediaUpload
}

func mediaJSON(w http.ResponseWriter, v map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func mediaFail(w http.ResponseWriter, msg string) {
	mediaJSON(w, map[string]any{"ok": false, "error": msg})
}

func randomHex() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) handleMediaStart(w http.ResponseWriter, r *http.Request) {
	pdf := r.FormValue("tipo") == "pdf"
	s.media.mu.Lock()
	defer s.media.mu.Unlock()
	now := s.now()
	for id, u := range s.media.byID { // abbandonati
		if now.Sub(u.started) > mediaUploadTTL {
			os.Remove(u.path)
			delete(s.media.byID, id)
		}
	}
	if len(s.media.byID) >= maxMediaUploads {
		mediaFail(w, "Troppi caricamenti in corso, riprova fra poco.")
		return
	}
	dir := filepath.Join(s.cfg.UploadDir, ".tmp")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		slog.Error("media: cartella temporanea", "err", err)
		mediaFail(w, "Caricamento non riuscito.")
		return
	}
	id := randomHex()
	s.media.byID[id] = &mediaUpload{path: filepath.Join(dir, id), pdf: pdf, started: now}
	mediaJSON(w, map[string]any{"ok": true, "id": id, "chunk": mediaChunk})
}

func (s *Server) handleMediaChunk(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.URL.Query().Get("n"))
	s.media.mu.Lock()
	defer s.media.mu.Unlock()
	u := s.media.byID[r.PathValue("id")]
	switch {
	case u == nil:
		mediaFail(w, "Caricamento scaduto, riprova.")
		return
	case err != nil || n != u.next:
		mediaFail(w, "Caricamento interrotto, riprova.")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, mediaChunk+1))
	limit := int64(maxImageBytes)
	if u.pdf {
		limit = maxPDFBytes
	}
	if err != nil || len(data) > mediaChunk || u.size+int64(len(data)) > limit {
		mediaFail(w, tooBigMsg(u.pdf))
		return
	}
	f, err := os.OpenFile(u.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err == nil {
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		slog.Error("media: scrittura pezzo", "err", err)
		mediaFail(w, "Caricamento non riuscito.")
		return
	}
	u.next++
	u.size += int64(len(data))
	mediaJSON(w, map[string]any{"ok": true})
}

func tooBigMsg(pdf bool) string {
	if pdf {
		return "File troppo grande (massimo 20 MB)."
	}
	return "Immagine troppo grande (massimo 2 MB)."
}

func (s *Server) handleMediaFinish(w http.ResponseWriter, r *http.Request) {
	s.media.mu.Lock()
	id := r.PathValue("id")
	u := s.media.byID[id]
	delete(s.media.byID, id)
	s.media.mu.Unlock()
	if u == nil {
		mediaFail(w, "Caricamento scaduto, riprova.")
		return
	}
	defer os.Remove(u.path) // dopo il rename non c'è più: errore ignorato
	head := make([]byte, 512)
	k := 0
	if f, err := os.Open(u.path); err == nil {
		k, _ = io.ReadFull(f, head)
		f.Close()
	}
	ext, ok := mediaExt(head[:k], u.pdf)
	if !ok {
		if u.pdf {
			mediaFail(w, "Il file non è un PDF.")
		} else {
			mediaFail(w, "Formato non ammesso: usa PNG, JPEG o WebP.")
		}
		return
	}
	name := randomHex() + "." + ext
	dir := s.uploadDir(uploadGuide)
	err := os.MkdirAll(dir, 0o750)
	if err == nil {
		err = os.Rename(u.path, filepath.Join(dir, name))
	}
	if err != nil {
		slog.Error("media: salvataggio", "err", err)
		mediaFail(w, "Caricamento non riuscito.")
		return
	}
	mediaJSON(w, map[string]any{"ok": true, "name": name, "url": "/uploads/guide/" + name})
}

// mediaExt: tipo dal contenuto, mai dal nome dichiarato. Niente SVG.
func mediaExt(head []byte, pdf bool) (string, bool) {
	if pdf {
		return "pdf", bytes.HasPrefix(head, []byte("%PDF-"))
	}
	switch http.DetectContentType(head) {
	case "image/png":
		return "png", true
	case "image/jpeg":
		return "jpg", true
	case "image/webp":
		return "webp", true
	}
	return "", false
}

// handleGuideImage: solo immagini, in sandbox come le icone. I PDF passano da
// /guide/{id}/pdf, che controlla la visibilità della guida.
func (s *Server) handleGuideImage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !guideImageRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, filepath.Join(s.uploadDir(uploadGuide), name))
}

// cleanMedia cancella i file non più usati da avvisi e guide. Solo quelli più
// vecchi di 24 ore: un'immagine appena caricata in un form non ancora salvato
// non è ancora referenziata.
func (s *Server) cleanMedia() {
	dir := s.uploadDir(uploadGuide)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !guideMediaRe.MatchString(name) {
			continue
		}
		info, err := e.Info()
		if err != nil || s.now().Sub(info.ModTime()) < mediaOrphanAge {
			continue
		}
		used, err := s.db.MediaReferenced(name)
		if err != nil {
			slog.Warn("media: verifica riferimenti", "err", err)
			return
		}
		if !used {
			if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				slog.Warn("media: rimozione file non usato", "file", name, "err", err)
			}
		}
	}
}
