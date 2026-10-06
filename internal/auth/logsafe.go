package auth

import "strings"

const maxLogValue = 64

// SafeLog prepara un valore digitato dall'utente per i log: niente a capo
// (righe di log contraffatte) e lunghezza limitata.
func SafeLog(s string) string {
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	if r := []rune(s); len(r) > maxLogValue {
		s = string(r[:maxLogValue])
	}
	return s
}
