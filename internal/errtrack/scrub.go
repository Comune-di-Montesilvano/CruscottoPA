package errtrack

import (
	"regexp"
	"strings"
)

// Testo libero (messaggi, errori di LDAP/OTRS/SQLite, stack): niente che
// identifichi una persona deve arrivare a GlitchTip, che è di un altro ente.
var (
	reDN       = regexp.MustCompile(`(?i)\b(?:CN|OU|DC|UID)=[^:;"\n]*`)
	reMail     = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	reDomUser  = regexp.MustCompile(`\b[A-Za-z0-9-]+\\[A-Za-z0-9._$-]+`)
	reIPv4     = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	reIPv6     = regexp.MustCompile(`(?i)[0-9a-f:]*[0-9a-f]::[0-9a-f:]*|[0-9a-f:]*::[0-9a-f]+[0-9a-f:]*|\b(?:[0-9a-f]{1,4}:){5,7}[0-9a-f]{1,4}\b`)
	rePathID   = regexp.MustCompile(`^(?:\d+|[0-9A-Fa-f-]{8,})$`)
	personalKs = map[string]bool{
		"user": true, "username": true, "utente": true, "mail": true, "email": true,
		"pc": true, "ip": true, "name": true, "nome": true, "remote": true,
	}
)

// Scrub sostituisce DN LDAP, mail, DOMINIO\utente e indirizzi IP con segnaposto.
func Scrub(s string) string {
	if s == "" {
		return s
	}
	s = reDN.ReplaceAllString(s, "<dn>")
	s = reMail.ReplaceAllString(s, "<mail>")
	s = reDomUser.ReplaceAllString(s, "<utente>")
	s = reIPv4.ReplaceAllString(s, "<ip>")
	s = reIPv6.ReplaceAllString(s, "<ip>")
	return s
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
		case i == last && strings.Contains(seg, "."):
			parts[i] = "{file}"
		case rePathID.MatchString(seg):
			parts[i] = "{id}"
		}
	}
	return strings.Join(parts, "/")
}

// PersonalKey: attributi di slog che non vanno mai inviati (anche dentro un gruppo).
func PersonalKey(k string) bool {
	if i := strings.LastIndexByte(k, '.'); i >= 0 {
		k = k[i+1:]
	}
	return personalKs[strings.ToLower(k)]
}
