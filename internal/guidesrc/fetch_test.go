package guidesrc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testFetcher(srv *httptest.Server) *Fetcher {
	f := NewFetcher()
	f.Client.Transport = srv.Client().Transport
	host := strings.TrimPrefix(srv.URL, "https://")
	f.allowHost = func(h string) bool { return h == host }
	return f
}

func TestFetch(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.md":
			w.Write([]byte("# Ciao"))
		case "/big.md":
			w.Write([]byte(strings.Repeat("a", 1<<20+1)))
		case "/bin.md":
			w.Write([]byte{0xff, 0xfe, 0x00})
		case "/redir.md":
			http.Redirect(w, r, "https://evil.example/x.md", http.StatusFound)
		case "/err.md":
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := testFetcher(srv)
	ctx := context.Background()
	if got, err := f.Fetch(ctx, srv.URL+"/ok.md"); err != nil || got != "# Ciao" {
		t.Fatalf("ok: %q %v", got, err)
	}
	if _, err := f.Fetch(ctx, srv.URL+"/no.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("404: %v", err)
	}
	if _, err := f.Fetch(ctx, srv.URL+"/big.md"); !errors.Is(err, ErrTooBig) {
		t.Errorf("grande: %v", err)
	}
	if _, err := f.Fetch(ctx, srv.URL+"/bin.md"); !errors.Is(err, ErrNotText) {
		t.Errorf("binario: %v", err)
	}
	if _, err := f.Fetch(ctx, srv.URL+"/redir.md"); err == nil || !strings.Contains(err.Error(), "redirect non ammesso") {
		t.Errorf("redirect esterno: %v", err)
	}
	if _, err := f.Fetch(ctx, srv.URL+"/err.md"); err == nil || !strings.Contains(err.Error(), "429") {
		t.Errorf("429: %v", err)
	}
}
