package errtrack

import (
	"regexp"
	"strings"
)

// Testo libero (messaggi, errori di LDAP/OTRS/SQLite, stack): niente che
// identifichi una persona deve arrivare a GlitchTip, che è di un altro ente.
var (
	reDN      = regexp.MustCompile(`(?i)\b(?:CN|OU|DC|UID)=[^:;"\n]*`)
	reMail    = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	reDomUser = regexp.MustCompile(`\b[A-Za-z0-9-]+\\[A-Za-z0-9._$-]+`)
	reIPv4    = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	// reTel: almeno 8 cifre con separatori singoli e una serie di 5 di fila
	// (date e versioni ne hanno al massimo 4).
	reTel      = regexp.MustCompile(`(?:\+\d{1,3}[ .-]?)?\b\d(?:[ .-]?\d){7,}\b`)
	reDigits5  = regexp.MustCompile(`\d{5}`)
	reIPv6     = regexp.MustCompile(`(?i)[0-9a-f:]*[0-9a-f]::[0-9a-f:]*|[0-9a-f:]*::[0-9a-f]+[0-9a-f:]*|\b(?:[0-9a-f]{1,4}:){5,7}[0-9a-f]{1,4}\b`)
	rePathID   = regexp.MustCompile(`^(?:\d+|[0-9A-Fa-f-]{8,})$`)
	personalKs = map[string]bool{
		"user": true, "username": true, "utente": true, "mail": true, "email": true,
		"pc": true, "ip": true, "name": true, "nome": true, "remote": true,
		"ticket": true, // numero OTRS: porta dritto al ticket di una persona
	}
)

// Scrub sostituisce DN LDAP, mail, DOMINIO\utente, indirizzi IP e numeri di
// telefono con segnaposto.
func Scrub(s string) string {
	if s == "" {
		return s
	}
	s = reDN.ReplaceAllString(s, "<dn>")
	s = reMail.ReplaceAllString(s, "<mail>")
	s = reDomUser.ReplaceAllString(s, "<utente>")
	s = replaceIPv4(s)
	s = reIPv6.ReplaceAllString(s, "<ip>")
	s = reTel.ReplaceAllStringFunc(s, func(m string) string {
		if reDigits5.MatchString(m) {
			return "<tel>"
		}
		return m
	})
	return s
}

// replaceIPv4 lascia stare le versioni dei prodotti (Chrome/131.0.0.0 nello
// user agent): quattro numeri subito dopo «lettera/» non sono un indirizzo.
func replaceIPv4(s string) string {
	idx := reIPv4.FindAllStringIndex(s, -1)
	if idx == nil {
		return s
	}
	var b strings.Builder
	prev := 0
	for _, m := range idx {
		if m[0] >= 2 && s[m[0]-1] == '/' && isLetter(s[m[0]-2]) {
			continue
		}
		b.WriteString(s[prev:m[0]])
		b.WriteString("<ip>")
		prev = m[1]
	}
	b.WriteString(s[prev:])
	return b.String()
}

func isLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// scrubKnown toglie dal testo i dati di chi ha generato l'errore (username,
// nome e cognome anche separati, PC), senza distinguere maiuscole.
func scrubKnown(s string, known []string) string {
	if s == "" {
		return s
	}
	var words []string
	for _, k := range known {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		words = append(words, regexp.QuoteMeta(k))
		if parts := strings.Fields(k); len(parts) > 1 {
			for _, w := range parts {
				if len([]rune(w)) >= 3 {
					words = append(words, regexp.QuoteMeta(w))
				}
			}
		}
	}
	if len(words) == 0 {
		return s
	}
	// le parole più lunghe prima: «Mario Rossi» intero, non «Mario» e poi «Rossi».
	re := regexp.MustCompile(`(?i)` + strings.Join(byLength(words), "|"))
	return re.ReplaceAllString(s, "<utente>")
}

func byLength(w []string) []string {
	out := append([]string(nil), w...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && len(out[j]) > len(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// NormalizePath toglie query e frammento e sostituisce ID e nomi di file,
// così le issue si raggruppano per pagina e gli URL non portano dati.
func NormalizePath(p string) string {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if p == "" || p == "/" {
		return "/"
	}
	parts := strings.Split(p, "/")
	last := len(parts) - 1
	for i, seg := range parts {
		switch {
		case seg == "":
		case i == last && (strings.Contains(seg, ".") || userFile(parts, i)):
			parts[i] = "{file}"
		case rePathID.MatchString(seg):
			parts[i] = "{id}"
		}
	}
	return strings.Join(parts, "/")
}

// userFile: nomi scelti dagli utenti anche senza estensione (allegati dei
// ticket, file caricati).
func userFile(parts []string, i int) bool {
	return (i >= 2 && parts[i-2] == "allegati") || (len(parts) > 3 && parts[1] == "uploads" && i >= 3)
}

// PersonalKey: attributi di slog che non vanno mai inviati (anche dentro un gruppo).
func PersonalKey(k string) bool {
	if i := strings.LastIndexByte(k, '.'); i >= 0 {
		k = k[i+1:]
	}
	return personalKs[strings.ToLower(k)]
}
