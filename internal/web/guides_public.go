package web

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

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

// handleGuidePage: guide Markdown e GitHub. Guida non disponibile → 200 con
// messaggio (il proxy riscriverebbe un 404).
func (s *Server) handleGuidePage(w http.ResponseWriter, r *http.Request) {
	g, err := s.planciaGuide(r)
	if err != nil {
		s.serverError(w, err)
		return
	}
	view := guidaView{Version: s.version, Admin: s.viewerIsAdmin(r)}
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
			http.Redirect(w, r, fmt.Sprintf("/guide/%d/pdf", g.ID), http.StatusSeeOther)
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
	if g == nil || g.Kind != database.GuideKindPDF || !guideMediaRe.MatchString(g.File) || !strings.HasSuffix(g.File, ".pdf") {
		s.render(w, http.StatusOK, "guida.html", guidaView{Version: s.version, Admin: s.viewerIsAdmin(r)})
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/pdf")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", pdfCSP)
	h.Set("Content-Disposition", `inline; filename="`+pdfFilename(g.Title)+`"`)
	http.ServeFile(w, r, filepath.Join(s.uploadDir(uploadGuide), g.File))
}

// pdfFilename: titolo ridotto a caratteri sicuri per l'header.
func pdfFilename(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '\'':
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "guida.pdf"
	}
	return b.String() + ".pdf"
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
