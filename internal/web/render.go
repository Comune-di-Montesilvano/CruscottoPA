package web

import (
	"bytes"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

const inputTimeLayout = "2006-01-02T15:04" // <input type="datetime-local">

func (s *Server) funcs() template.FuncMap {
	return template.FuncMap{
		"monogram":   monogram,
		"levelLabel": levelLabel,
		"linkify":    linkify,
		"fmtDate":    func(t time.Time) string { return t.In(s.loc()).Format("02/01/2006 15:04") },
		"fmtDatePtr": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.In(s.loc()).Format("02/01/2006 15:04")
		},
		"inputTime": func(t time.Time) string { return t.In(s.loc()).Format(inputTimeLayout) },
		"derefID": func(p *int64) int64 {
			if p == nil {
				return 0
			}
			return *p
		},
	}
}

// render esegue il template in un buffer: un errore a metà non produce mai
// una pagina troncata con status 200.
func (s *Server) render(w http.ResponseWriter, status int, name string, data any) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		s.serverError(w, fmt.Errorf("template %s: %w", name, err))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (s *Server) serverError(w http.ResponseWriter, err error) {
	slog.Error("errore interno", "err", err)
	http.Error(w, "Si è verificato un errore. Riprova più tardi.", http.StatusInternalServerError)
}

// monogram: iniziali delle prime due parole, oppure prime due lettere se una sola parola.
func monogram(title string) string {
	words := strings.FieldsFunc(title, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var significant []string
	for _, w := range words {
		if len([]rune(w)) > 2 || len(words) == 1 {
			significant = append(significant, w)
		}
	}
	switch {
	case len(significant) == 0:
		return "?"
	case len(significant) == 1:
		r := []rune(significant[0])
		if len(r) == 1 {
			return strings.ToUpper(string(r))
		}
		return strings.ToUpper(string(r[0])) + strings.ToLower(string(r[1]))
	default:
		a, b := []rune(significant[0]), []rune(significant[1])
		return strings.ToUpper(string(a[0]) + string(b[0]))
	}
}

func levelLabel(level string) string {
	switch level {
	case database.LevelUrgent:
		return "Urgente"
	case database.LevelMaintenance:
		return "Manutenzione"
	default:
		return "Novità"
	}
}

var urlRe = regexp.MustCompile(`https?://[^\s<>"']+`)

// linkify escapa il testo e rende cliccabili solo gli URL http/https.
func linkify(text string) template.HTML {
	var b strings.Builder
	last := 0
	for _, m := range urlRe.FindAllStringIndex(text, -1) {
		b.WriteString(template.HTMLEscapeString(text[last:m[0]]))
		u := template.HTMLEscapeString(text[m[0]:m[1]])
		fmt.Fprintf(&b, `<a href="%s" target="_blank" rel="noopener">%s</a>`, u, u)
		last = m[1]
	}
	b.WriteString(template.HTMLEscapeString(text[last:]))
	return template.HTML(b.String()) //nolint:gosec // testo escapato sopra
}
