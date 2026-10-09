package web

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/guidesrc"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/markdown"
)

// pdfCSP: niente sandbox, il viewer PDF di Chrome/Edge è un plugin (object-src)
// e un documento in sandbox rischia di essere scaricato invece che mostrato.
const pdfCSP = "default-src 'none'; object-src 'self'; frame-ancestors 'none'"

type guidaView struct {
	Guide   *database.Guide
	HTML    template.HTML
	Source  string // link al file su GitHub
	Version string
	Admin   bool
}

// planciaGuide: guida abilitata, di un'app visibile e destinata a chi guarda;
// nil se non c'è (id non valido compreso).
func (s *Server) planciaGuide(r *http.Request) (*database.Guide, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, nil
	}
	g, err := s.db.GetPlanciaGuide(id)
	if errors.Is(err, database.ErrNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	f, err := s.contentFilterFor(r)
	if err != nil {
		return nil, err
	}
	if !f.guideVisible(g) {
		return nil, nil
	}
	return &g, nil
}

// adminGuide: qualsiasi guida, anche disattivata o riservata (anteprima
// dall'admin, dietro requireAdmin); nil se non c'è.
func (s *Server) adminGuide(r *http.Request) (*database.Guide, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, nil
	}
	g, err := s.db.GetGuide(id)
	if errors.Is(err, database.ErrNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &g, nil
}

// handleGuidePage: guide Markdown e GitHub. Guida non disponibile → 200 con
// messaggio (il proxy riscriverebbe un 404).
func (s *Server) handleGuidePage(w http.ResponseWriter, r *http.Request) {
	g, err := s.planciaGuide(r)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.showGuide(w, r, g, "/guide/%d/pdf", s.viewerIsAdmin(r))
}

// handleAdminGuidePreview: come /guide/{id}, per ogni guida (l'admin la vede
// prima di attivarla). Il PDF passa da /admin/guide/{id}/pdf.
func (s *Server) handleAdminGuidePreview(w http.ResponseWriter, r *http.Request) {
	g, err := s.adminGuide(r)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.showGuide(w, r, g, "/admin/guide/%d/pdf", true)
}

func (s *Server) showGuide(w http.ResponseWriter, r *http.Request, g *database.Guide, pdfPath string, admin bool) {
	view := guidaView{Version: s.version, Admin: admin}
	if g != nil {
		switch g.Kind {
		case database.GuideKindMarkdown:
			view.Guide, view.HTML = g, markdown.Render(g.Body, markdown.Options{})
		case database.GuideKindGitHub:
			src, _ := guidesrc.ParseGitHubURL(g.SourceURL)
			view.Guide, view.Source = g, g.SourceURL
			view.HTML = markdown.Render(g.Body, markdown.Options{LinkBase: src.LinkBase, ImageBase: src.ImageBase, LinkRoot: src.LinkRoot, ImageRoot: src.ImageRoot})
		case database.GuideKindLink:
			http.Redirect(w, r, g.URL, http.StatusSeeOther)
			return
		case database.GuideKindPDF:
			http.Redirect(w, r, fmt.Sprintf(pdfPath, g.ID), http.StatusSeeOther)
			return
		}
	}
	s.render(w, http.StatusOK, "guida.html", view)
}

func (s *Server) handleGuidePDF(w http.ResponseWriter, r *http.Request) {
	g, err := s.planciaGuide(r)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.serveGuidePDF(w, r, g, s.viewerIsAdmin(r))
}

func (s *Server) handleAdminGuidePDF(w http.ResponseWriter, r *http.Request) {
	g, err := s.adminGuide(r)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.serveGuidePDF(w, r, g, true)
}

func (s *Server) serveGuidePDF(w http.ResponseWriter, r *http.Request, g *database.Guide, admin bool) {
	if g == nil || g.Kind != database.GuideKindPDF || !guideMediaRe.MatchString(g.File) || !strings.HasSuffix(g.File, ".pdf") {
		s.render(w, http.StatusOK, "guida.html", guidaView{Version: s.version, Admin: admin})
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/pdf")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", pdfCSP)
	h.Set("Content-Disposition", pdfDisposition(g.Title))
	http.ServeFile(w, r, filepath.Join(s.uploadDir(uploadGuide), g.File))
}

// pdfDisposition: nome del file dal titolo. filename* (RFC 6266) tiene le
// lettere accentate; filename è il ripiego ASCII, con gli accenti tolti.
func pdfDisposition(title string) string {
	var full, ascii strings.Builder
	for _, r := range title {
		switch {
		case r == '-' || r == '_' || (r >= '0' && r <= '9'):
			full.WriteRune(r)
			ascii.WriteRune(r)
		case unicode.IsLetter(r):
			full.WriteRune(r)
			if r < utf8.RuneSelf {
				ascii.WriteRune(r)
			} else if b, ok := unaccent[unicode.ToLower(r)]; ok {
				if unicode.IsUpper(r) {
					b = unicode.ToUpper(b)
				}
				ascii.WriteRune(b)
			}
		case r == ' ' || r == '\'':
			full.WriteByte('-')
			ascii.WriteByte('-')
		}
	}
	name := func(b *strings.Builder) string {
		s := strings.Trim(b.String(), "-")
		if s == "" {
			return "guida.pdf"
		}
		return s + ".pdf"
	}
	return `inline; filename="` + name(&ascii) + `"; filename*=UTF-8''` + url.PathEscape(name(&full))
}

// unaccent: lettere accentate più comuni nei titoli → lettera base.
var unaccent = map[rune]rune{
	'à': 'a', 'á': 'a', 'â': 'a', 'ä': 'a', 'è': 'e', 'é': 'e', 'ê': 'e', 'ë': 'e',
	'ì': 'i', 'í': 'i', 'î': 'i', 'ï': 'i', 'ò': 'o', 'ó': 'o', 'ô': 'o', 'ö': 'o',
	'ù': 'u', 'ú': 'u', 'û': 'u', 'ü': 'u', 'ç': 'c', 'ñ': 'n',
}

// guideHref: dove porta una guida dalla plancia.
func guideHref(g database.Guide) string {
	switch g.Kind {
	case database.GuideKindLink:
		return g.URL
	case database.GuideKindPDF:
		return fmt.Sprintf("/guide/%d/pdf", g.ID)
	}
	return fmt.Sprintf("/guide/%d", g.ID)
}

// guideNewTab: link esterni e PDF in una nuova scheda; le pagine guida no.
func guideNewTab(g database.Guide) bool {
	return g.Kind == database.GuideKindLink || g.Kind == database.GuideKindPDF
}
