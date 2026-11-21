package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/errtrack"
)

const maxErroriBody = 16 << 10

// erroreBrowser: quanto manda web/static/js/errori.js.
type erroreBrowser struct {
	Message string `json:"message"`
	Stack   string `json:"stack"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Page    string `json:"page"`
	UA      string `json:"ua"`
}

var extensionSchemes = []string{"chrome-extension://", "moz-extension://", "safari-extension://", "safari-web-extension://", "edge-extension://"}

// handleErrori inoltra a GlitchTip un errore JavaScript. Sempre 200: il proxy
// sostituirebbe le risposte di errore. Scrub e anti-pioggia in errtrack.
func (s *Server) handleErrori(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	ok := s.reportBrowserError(w, r)
	if ok {
		w.Write([]byte(`{"ok":true}` + "\n"))
	} else {
		w.Write([]byte(`{"ok":false}` + "\n"))
	}
}

func (s *Server) reportBrowserError(w http.ResponseWriter, r *http.Request) bool {
	var e erroreBrowser
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxErroriBody)).Decode(&e); err != nil {
		return false
	}
	e.Message = strings.TrimSpace(e.Message)
	if e.Message == "" {
		return false
	}
	if !s.errs.Enabled() {
		return true
	}
	file, own := ownFile(e.File, siteHost(r))
	if !own || (e.Message == "Script error." && file == "") || hasExtension(e.Stack) || hasExtension(e.File) {
		return true // non è nostro: scartato in silenzio
	}
	s.errs.CaptureBrowser(errtrack.BrowserError{
		Message:   e.Message,
		Stack:     e.Stack,
		File:      file,
		Line:      e.Line,
		Col:       e.Col,
		Page:      e.Page,
		UserAgent: e.UA,
		Personal:  s.reportPersonal(r),
	}, s.reportUser(r))
	return true
}

// reportPersonal: username, nome e PC di chi scrive, da togliere dal testo
// libero dell'errore (la pagina li mostra, uno script può riportarli).
func (s *Server) reportPersonal(r *http.Request) []string {
	// Il cookie dell'admin ha Path=/admin e non arriva qui: resta il riconoscimento.
	if u, ok := s.viewer(r); ok && !u.Anonymous {
		return []string{u.Username, u.Name, u.PC}
	}
	return nil
}

// siteHost: host del sito visto dal browser. Dietro il proxy r.Host può essere
// il nome dell'upstream; Origin (mandato da sendBeacon e fetch) no.
func siteHost(r *http.Request) string {
	if o := r.Header.Get("Origin"); o != "" {
		if u, err := url.Parse(o); err == nil && u.Host != "" {
			return u.Host
		}
	}
	return r.Host
}

// ownFile: percorso dello script se è del sito (relativo, o assoluto sullo
// stesso host), senza query. "" (es. unhandledrejection) è ammesso.
func ownFile(f, host string) (string, bool) {
	f = strings.TrimSpace(f)
	if f == "" {
		return "", true
	}
	if strings.HasPrefix(f, "/") && !strings.HasPrefix(f, "//") {
		u, err := url.Parse(f)
		if err != nil {
			return "", false
		}
		return u.Path, true
	}
	u, err := url.Parse(f)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Host, host) {
		return "", false
	}
	return u.Path, true
}

func hasExtension(s string) bool {
	for _, p := range extensionSchemes {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
