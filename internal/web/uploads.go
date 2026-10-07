package web

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

const maxIconBytes = 512 << 10

var (
	errIconType = errors.New("Formato non ammesso: usa PNG, WebP o SVG.")
	errIconSize = errors.New("File troppo grande (massimo 512 KB).")
	// Nome generato da saveUpload: 16 byte casuali in hex + estensione.
	uploadFileRe = regexp.MustCompile(`^[0-9a-f]{32}\.(png|webp|svg)$`)
)

// Sottodirectory di UPLOAD_DIR per tipo di file caricato.
const (
	uploadIcons    = "icons"    // icone delle app
	uploadBranding = "branding" // logo dell'ente
)

func (s *Server) uploadDir(kind string) string { return filepath.Join(s.cfg.UploadDir, kind) }

// detectIconExt riconosce il tipo dal contenuto, mai dall'estensione dichiarata.
func detectIconExt(data []byte) (string, error) {
	switch http.DetectContentType(data) {
	case "image/png":
		return "png", nil
	case "image/webp":
		return "webp", nil
	}
	if isSVG(data) {
		return "svg", nil
	}
	return "", errIconType
}

// isSVG: documento XML il cui primo elemento è <svg>.
func isSVG(data []byte) bool {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local == "svg"
		}
	}
}

func (s *Server) saveUpload(kind string, r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxIconBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxIconBytes {
		return "", errIconSize
	}
	ext, err := detectIconExt(data)
	if err != nil {
		return "", err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	name := hex.EncodeToString(b) + "." + ext
	dir := s.uploadDir(kind)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o640); err != nil {
		return "", err
	}
	return name, nil
}

// removeUpload cancella un file caricato; ignora nomi non generati da saveUpload.
func (s *Server) removeUpload(kind, name string) {
	re := uploadFileRe
	if kind == uploadGuide {
		re = guideMediaRe
	}
	if !re.MatchString(name) {
		return
	}
	if err := os.Remove(filepath.Join(s.uploadDir(kind), name)); err != nil && !os.IsNotExist(err) {
		slog.Warn("rimozione file caricato", "dir", kind, "file", name, "err", err)
	}
}

// handleUploadFile serve i file caricati in sandbox: uno script dentro un SVG
// non viene eseguito nemmeno aprendo direttamente l'URL del file.
func (s *Server) handleUploadFile(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("file")
		if !uploadFileRe.MatchString(name) {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, filepath.Join(s.uploadDir(kind), name))
	}
}
