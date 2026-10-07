// Package audience decide chi vede cosa in plancia: appartenenza ai gruppi
// della plancia e visibilità dei contenuti. È un filtro di PRESENTAZIONE, non
// una protezione: l'identità dell'utente è dichiarata (NTLM non verificato).
package audience

import "strings"

const (
	KindAttr    = "attr"    // valore di un attributo AD
	KindADGroup = "adgroup" // gruppo AD (DN), annidati compresi
	KindUser    = "user"    // utente incluso
	KindExclude = "exclude" // utente escluso
)

func ValidKind(k string) bool {
	switch k {
	case KindAttr, KindADGroup, KindUser, KindExclude:
		return true
	}
	return false
}

// Profile è ciò che serve sapere di un utente per i gruppi della plancia.
type Profile struct {
	Username string            // minuscolo
	Attrs    map[string]string // nome attributo minuscolo → valore
	Groups   []string          // DN dei gruppi AD, annidati compresi
}

type Rule struct{ Kind, Attr, Value string }

func norm(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Member: almeno una regola attr/adgroup/user soddisfatta e nessuna exclude.
func Member(p Profile, rules []Rule) bool {
	user := norm(p.Username)
	in := false
	for _, r := range rules {
		switch r.Kind {
		case KindExclude:
			if norm(r.Value) == user {
				return false
			}
		case KindUser:
			in = in || norm(r.Value) == user
		case KindAttr:
			v, ok := p.Attrs[norm(r.Attr)]
			in = in || (ok && norm(v) == norm(r.Value))
		case KindADGroup:
			for _, g := range p.Groups {
				if norm(g) == norm(r.Value) {
					in = true
				}
			}
		}
	}
	return in
}

type Mode string

const (
	ModePublic Mode = ""
	ModeOnly   Mode = "only"
	ModeHide   Mode = "hide"
)

func ValidMode(m string) bool {
	switch Mode(m) {
	case ModePublic, ModeOnly, ModeHide:
		return true
	}
	return false
}

// Visible: pubblico → sempre; utente non noto (anonimo o profilo non
// disponibile) → solo i pubblici; only → in almeno uno dei gruppi;
// hide → in nessuno.
func Visible(mode Mode, groups []int64, memberOf map[int64]bool, known bool) bool {
	if mode == ModePublic {
		return true
	}
	if !known {
		return false
	}
	inAny := false
	for _, g := range groups {
		if memberOf[g] {
			inAny = true
			break
		}
	}
	if mode == ModeOnly {
		return inAny
	}
	return !inAny
}
