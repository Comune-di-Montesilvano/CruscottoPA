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
	// Nome generato da saveIcon: 16 byte casuali in hex + estensione.
	iconFileRe = regexp.MustCompile(`^[0-9a-f]{32}\.(png|webp|svg)$`)
)

func (s *Server) iconDir() string { return filepath.Join(s.cfg.UploadDir, "icons") }

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

func (s *Server) saveIcon(r io.Reader) (string, error) {
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
	if err := os.MkdirAll(s.iconDir(), 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(s.iconDir(), name), data, 0o640); err != nil {
		return "", err
	}
	return name, nil
}

// removeIcon cancella un file caricato; ignora nomi non generati da saveIcon.
func (s *Server) removeIcon(name string) {
	if !iconFileRe.MatchString(name) {
		return
	}
	if err := os.Remove(filepath.Join(s.iconDir(), name)); err != nil && !os.IsNotExist(err) {
		slog.Warn("rimozione icona", "file", name, "err", err)
	}
}

// handleIconFile serve le icone caricate in sandbox: uno script dentro un SVG
// non viene eseguito nemmeno aprendo direttamente l'URL del file.
func (s *Server) handleIconFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !iconFileRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, filepath.Join(s.iconDir(), name))
}
