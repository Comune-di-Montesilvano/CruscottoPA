package web

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	pngBytes  = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	svgScript = []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
)

func TestDetectIconExt(t *testing.T) {
	for _, tc := range []struct {
		data []byte
		ext  string
		err  error
	}{
		{pngBytes, "png", nil},
		{append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 32)...), "webp", nil},
		{svgScript, "svg", nil},
		{[]byte(`<!DOCTYPE svg><svg></svg>`), "svg", nil},
		{[]byte(`<html><body>ciao</body></html>`), "", errIconType},
		{[]byte("GIF89a......"), "", errIconType},
		{[]byte("testo qualsiasi"), "", errIconType},
	} {
		ext, err := detectIconExt(tc.data)
		if ext != tc.ext || !errors.Is(err, tc.err) {
			t.Errorf("detectIconExt(%.20q) = %q, %v; atteso %q, %v", tc.data, ext, err, tc.ext, tc.err)
		}
	}
}

func TestSaveUploadLimits(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if _, err := s.saveUpload(uploadIcons, bytes.NewReader(make([]byte, maxIconBytes+1))); !errors.Is(err, errIconSize) {
		t.Fatalf("file > 512 KB: atteso errIconSize, ottenuto %v", err)
	}
	name, err := s.saveUpload(uploadIcons, bytes.NewReader(pngBytes))
	if err != nil || !uploadFileRe.MatchString(name) || !strings.HasSuffix(name, ".png") {
		t.Fatalf("saveUpload: %q %v", name, err)
	}
	if _, err := os.Stat(filepath.Join(s.uploadDir(uploadIcons), name)); err != nil {
		t.Fatal("file non scritto")
	}
	s.removeUpload(uploadIcons, name)
	if _, err := os.Stat(filepath.Join(s.uploadDir(uploadIcons), name)); !os.IsNotExist(err) {
		t.Fatal("removeUpload non ha cancellato il file")
	}
	s.removeUpload(uploadIcons, "../../etc/passwd") // nome non conforme: ignorato senza panic
}

func TestServeUploadsSandboxed(t *testing.T) {
	s, _ := newTestServer(t, nil)
	for _, kind := range []string{uploadIcons, uploadBranding} {
		name, err := s.saveUpload(kind, bytes.NewReader(svgScript))
		if err != nil {
			t.Fatal(err)
		}
		rec := do(t, s, "GET", "/uploads/"+kind+"/"+name, nil, nil, nil)
		if rec.Code != 200 {
			t.Fatalf("%s: %d", kind, rec.Code)
		}
		if csp := rec.Header().Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") {
			t.Fatalf("%s: SVG servito senza sandbox: %q", kind, csp)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "image/svg+xml" {
			t.Fatalf("%s: Content-Type %q", kind, ct)
		}
		for _, bad := range []string{"abc.png", strings.Repeat("a", 32) + ".html", "..%2f..%2fgo.mod"} {
			if rec := do(t, s, "GET", "/uploads/"+kind+"/"+bad, nil, nil, nil); rec.Code != http.StatusNotFound {
				t.Errorf("/uploads/%s/%s: atteso 404, ottenuto %d", kind, bad, rec.Code)
			}
		}
	}
	// Un file delle icone non è raggiungibile dal percorso del branding.
	name, _ := s.saveUpload(uploadIcons, bytes.NewReader(pngBytes))
	if rec := do(t, s, "GET", "/uploads/branding/"+name, nil, nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("icona servita da /uploads/branding: %d", rec.Code)
	}
}
