package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/calendar"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/markdown"
)

const inputTimeLayout = "2006-01-02T15:04" // <input type="datetime-local">

func (s *Server) funcs() template.FuncMap {
	return template.FuncMap{
		"monogram":      monogram,
		"ente":          s.ente,
		"errori":        s.errs.Enabled,
		"repoURL":       func() string { return repoURL },
		"levelLabel":    levelLabel,
		"webSearchName": webSearchName,
		"deliveryLabel": deliveryLabel,
		"readLabel":     readLabel,
		"md":            func(src string) template.HTML { return markdown.Render(src, markdown.Options{}) },
		"excerpt":       func(src string) string { return markdown.Plain(src, excerptRunes) },
		"needsMore":     needsMore,
		"ticketState":   stateLabel,
		"ticketStep":    ticketStep,
		"stateClass":    stateClass,
		"splitPC":       splitPC,
		"thero": func(title, meta string, sub, action bool) map[string]any {
			return map[string]any{"Title": title, "Meta": meta, "Sub": sub, "Action": action}
		},
		"ago":           func(t time.Time) string { return ago(t, s.now(), s.loc()) },
		"humanSize":     humanSize,
		"tileColor":     tileColor,
		"thousands":     thousands,
		"roughDuration": roughDuration,
		"plural":        plural,
		"alertVersion":  alertVersion,
		"alertSource":   alertSource,
		"shortDay":      shortDay,
		"occRange":      occRange,
		"kindLabel":     kindLabel,
		"guideKind":     guideKindLabel,
		"guideHref":     guideHref,
		"guideNewTab":   guideNewTab,
		"fmtDate":       func(t time.Time) string { return t.In(s.loc()).Format("02/01/2006 15:04") },
		"fmtDay":        func(t time.Time) string { return t.In(s.loc()).Format("02/01/2006") },
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
		// messaggio col nome del template e PC di chi chiama render: una issue
		// per template e per punto del codice.
		s.internalError(w, "errore template "+name, err, callerPC())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (s *Server) serverError(w http.ResponseWriter, err error) {
	// Il record porta il PC di chi chiama serverError: su GlitchTip ogni
	// punto del codice fa una issue sua invece di un unico «errore interno».
	s.internalError(w, "errore interno", err, callerPC())
}

// callerPC: PC di chi ha chiamato la funzione che chiama callerPC.
func callerPC() uintptr {
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	return pcs[0]
}

func (s *Server) internalError(w http.ResponseWriter, msg string, err error, pc uintptr) {
	rec := slog.NewRecord(time.Now(), slog.LevelError, msg, pc)
	rec.AddAttrs(slog.Any("err", err))
	if h := slog.Default().Handler(); h.Enabled(context.Background(), slog.LevelError) {
		_ = h.Handle(context.Background(), rec)
	}
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

// excerptRunes: lunghezza dell'estratto degli avvisi nel carosello.
const excerptRunes = 200

// needsMore: nel carosello l'estratto non basta (testo troncato, su più righe,
// con immagini, tabelle o link: l'estratto è testo semplice su una riga).
func needsMore(body string) bool {
	return utf8.RuneCountInString(markdown.Plain(body, 0)) > excerptRunes ||
		strings.Contains(strings.TrimSpace(body), "\n") || markdown.HasRich(body)
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

// tileColor restituisce il colore dell'app per la variabile CSS --c della
// tile: il pastello lo calcola plancia.css con color-mix (chiaro o scuro a
// seconda del tema). Colore non valido → grigio ardesia.
func tileColor(hexColor string) string {
	if !colorRe.MatchString(hexColor) {
		return "#64748b"
	}
	return hexColor
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

// deliveryLabel: esito di una consegna push in italiano.
func deliveryLabel(status string) string {
	switch status {
	case database.DeliverySent:
		return "inviata"
	case database.DeliveryFailed:
		return "non riuscita"
	case database.DeliveryGone:
		return "iscrizione scaduta"
	}
	return status
}

// readLabel: come è stato letto un avviso.
func readLabel(how string) string {
	switch how {
	case database.ReadConfirm:
		return "«Ho letto»"
	case database.ReadOpen:
		return "pagina aperta"
	case database.ReadPlancia:
		return "in plancia"
	}
	return how
}

// webSearchName: nome del motore per «Cerca … su Google».
func webSearchName(tmpl string) string {
	u, err := url.Parse(strings.Replace(tmpl, "%s", "x", 1))
	if err != nil {
		return "web"
	}
	h := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	switch {
	case strings.HasPrefix(h, "google."):
		return "Google"
	case strings.HasPrefix(h, "bing."):
		return "Bing"
	case strings.HasPrefix(h, "duckduckgo."):
		return "DuckDuckGo"
	case h == "":
		return "web"
	}
	return h
}
