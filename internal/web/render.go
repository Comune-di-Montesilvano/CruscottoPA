package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/calendar"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

const inputTimeLayout = "2006-01-02T15:04" // <input type="datetime-local">

func (s *Server) funcs() template.FuncMap {
	return template.FuncMap{
		"monogram":     monogram,
		"levelLabel":   levelLabel,
		"paragraphs":   paragraphs,
		"humanSize":    humanSize,
		"tint":         tint,
		"alertVersion": alertVersion,
		"alertSource":  alertSource,
		"shortDay":     shortDay,
		"occRange":     occRange,
		"kindLabel":    kindLabel,
		"fmtDate":      func(t time.Time) string { return t.In(s.loc()).Format("02/01/2006 15:04") },
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

// linkRe trova URL http/https oppure indirizzi email. Gli URL vengono prima
// nell'alternanza: una @ dentro un URL (https://utente@host) resta parte dell'URL.
var (
	linkRe      = regexp.MustCompile(`https?://[^\s<>"']+|[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
	paragraphRe = regexp.MustCompile(`\n[ \t]*\n\s*`)
)

// linkify escapa il testo e rende cliccabili solo gli URL http/https e gli
// indirizzi email. La punteggiatura finale ("vedi https://x.it.") resta fuori dal link.
func linkify(text string) template.HTML {
	var b strings.Builder
	last := 0
	for _, m := range linkRe.FindAllStringIndex(text, -1) {
		end := m[1]
		for end > m[0] && strings.ContainsRune(".,;:!?)", rune(text[end-1])) {
			end--
		}
		b.WriteString(template.HTMLEscapeString(text[last:m[0]]))
		u := template.HTMLEscapeString(text[m[0]:end])
		if strings.HasPrefix(u, "http") {
			fmt.Fprintf(&b, `<a href="%s" target="_blank" rel="noopener">%s</a>`, u, u)
		} else {
			fmt.Fprintf(&b, `<a href="mailto:%s">%s</a>`, u, u)
		}
		last = end
	}
	b.WriteString(template.HTMLEscapeString(text[last:]))
	return template.HTML(b.String()) //nolint:gosec // testo escapato sopra
}

// paragraphs divide il testo in <p> sulle righe vuote; dentro un paragrafo gli
// a capo singoli restano (li mostra il CSS con white-space: pre-line).
func paragraphs(text string) template.HTML {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var b strings.Builder
	for _, p := range paragraphRe.Split(text, -1) {
		if p = strings.TrimSpace(p); p != "" {
			fmt.Fprintf(&b, "<p>%s</p>", linkify(p))
		}
	}
	return template.HTML(b.String()) //nolint:gosec // contenuto escapato da linkify
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 2; m /= unit {
		div *= unit
		exp++
	}
	s := fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMG"[exp])
	return strings.Replace(s, ".", ",", 1)
}

func kindLabel(kind string) string {
	switch kind {
	case "auto":
		return "Automatico"
	case "manuale":
		return "Manuale"
	default:
		return "Pre-ripristino"
	}
}

// tint schiarisce un colore #rrggbb mescolandolo all'85% con il bianco
// (sfondo pastello delle tile). Colore non valido → grigio chiaro.
func tint(hexColor string) string {
	if !colorRe.MatchString(hexColor) {
		return "#eef1f5"
	}
	var c [3]int64
	for i := range c {
		v, _ := strconv.ParseInt(hexColor[1+2*i:3+2*i], 16, 64)
		c[i] = v + int64(math.Round(float64(255-v)*0.85))
	}
	return fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2])
}

// alertVersion identifica il contenuto di un avviso: cambia se cambiano
// titolo, testo, livello o date (non la fonte), così un urgente modificato
// si ripresenta anche a chi l'aveva già letto.
func alertVersion(a database.Alert) string {
	end := ""
	if a.EndsAt != nil {
		end = a.EndsAt.UTC().Format(time.RFC3339)
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{a.Title, a.Body, a.Level, a.StartsAt.UTC().Format(time.RFC3339), end}, "\x00")))
	return hex.EncodeToString(sum[:4])
}

func alertSource(s string) string {
	if strings.TrimSpace(s) == "" {
		return "Servizio informatico"
	}
	return s
}

var (
	shortWeekdays = [...]string{"dom", "lun", "mar", "mer", "gio", "ven", "sab"}
	shortMonths   = [...]string{"gen", "feb", "mar", "apr", "mag", "giu", "lug", "ago", "set", "ott", "nov", "dic"}
)

func shortDay(t time.Time) string {
	return fmt.Sprintf("%s %d %s", shortWeekdays[t.Weekday()], t.Day(), shortMonths[t.Month()-1])
}

func occRange(o calendar.Occurrence) string {
	if o.Start.Equal(o.End) {
		return shortDay(o.Start)
	}
	return shortDay(o.Start) + " – " + shortDay(o.End)
}
