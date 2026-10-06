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

func TestSaveIconLimits(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if _, err := s.saveIcon(bytes.NewReader(make([]byte, maxIconBytes+1))); !errors.Is(err, errIconSize) {
		t.Fatalf("file > 512 KB: atteso errIconSize, ottenuto %v", err)
	}
	name, err := s.saveIcon(bytes.NewReader(pngBytes))
	if err != nil || !iconFileRe.MatchString(name) || !strings.HasSuffix(name, ".png") {
		t.Fatalf("saveIcon: %q %v", name, err)
	}
	if _, err := os.Stat(filepath.Join(s.iconDir(), name)); err != nil {
		t.Fatal("file non scritto")
	}
	s.removeIcon(name)
	if _, err := os.Stat(filepath.Join(s.iconDir(), name)); !os.IsNotExist(err) {
		t.Fatal("removeIcon non ha cancellato il file")
	}
	s.removeIcon("../../etc/passwd") // nome non conforme: ignorato senza panic
}

func TestServeIconSandboxed(t *testing.T) {
	s, _ := newTestServer(t, nil)
	name, _ := s.saveIcon(bytes.NewReader(svgScript))

	rec := do(t, s, "GET", "/uploads/icons/"+name, nil, nil, nil)
	if rec.Code != 200 {
		t.Fatalf("icona: %d", rec.Code)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") {
		t.Fatalf("SVG servito senza sandbox: %q", csp)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Fatalf("Content-Type: %q", ct)
	}
	for _, bad := range []string{"/uploads/icons/abc.png", "/uploads/icons/" + strings.Repeat("a", 32) + ".html", "/uploads/icons/..%2f..%2fgo.mod"} {
		if rec := do(t, s, "GET", bad, nil, nil, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: atteso 404, ottenuto %d", bad, rec.Code)
		}
	}
}
