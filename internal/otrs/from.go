package otrs

import "strings"

// fromHeader scrive il mittente degli articoli come `"Nome" <mail>` con il
// nome in UTF-8 in chiaro. Il web service di OTRS salva il From così com'è:
// mail.Address.String() codificherebbe in RFC 2047 i nomi con lettere
// accentate («Niccolò» → "=?utf-8?q?Niccol=C3=B2?=") e l'operatore vedrebbe
// la forma codificata. A capo e caratteri di controllo diventano spazi.
func fromHeader(name, email string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" {
		return "<" + email + ">"
	}
	name = strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name)
	return `"` + name + `" <` + email + `>`
}
