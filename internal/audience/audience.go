// Package audience decide chi vede cosa in plancia: appartenenza ai gruppi
// della plancia e visibilità dei contenuti. È un filtro di PRESENTAZIONE, non
// una protezione: l'identità dell'utente è dichiarata (NTLM non verificato).
package audience

import (
	"errors"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

const (
	KindAttr    = "attr"    // valore di un attributo AD
	KindADGroup = "adgroup" // gruppo AD (DN), annidati compresi
	KindUser    = "user"    // utente incluso
	KindExclude = "exclude" // utente escluso
	KindPresent = "present" // requisito: attributo compilato
	KindAbsent  = "absent"  // requisito: attributo vuoto
)

func ValidKind(k string) bool {
	switch k {
	case KindAttr, KindADGroup, KindUser, KindExclude, KindPresent, KindAbsent:
		return true
	}
	return false
}

// IsRequirement: regola che tutti i membri devono soddisfare (AND), non una
// via d'ingresso nel gruppo.
func IsRequirement(k string) bool { return k == KindPresent || k == KindAbsent }

// Profile è ciò che serve sapere di un utente per i gruppi della plancia.
type Profile struct {
	Username string              // minuscolo
	Attrs    map[string][]string // nome attributo minuscolo → valori
	Groups   []string            // DN dei gruppi AD, annidati compresi
}

type Rule struct{ Kind, Attr, Value string }

func norm(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Member: almeno una regola attr/adgroup/user soddisfatta, tutti i requisiti
// (present/absent) rispettati e nessuna exclude.
func Member(p Profile, rules []Rule) bool {
	user := norm(p.Username)
	in := false
	for _, r := range rules {
		switch r.Kind {
		case KindExclude:
			if norm(r.Value) == user {
				return false
			}
		case KindPresent, KindAbsent:
			if hasValue(p.Attrs[norm(r.Attr)]) != (r.Kind == KindPresent) {
				return false
			}
		case KindUser:
			in = in || norm(r.Value) == user
		case KindAttr:
			for _, v := range p.Attrs[norm(r.Attr)] {
				in = in || norm(v) == norm(r.Value)
			}
		case KindADGroup:
			for _, g := range p.Groups {
				in = in || sameDN(g, r.Value)
			}
		}
	}
	return in
}

func hasValue(vals []string) bool {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

// sameDN confronta due DN come fa AD (maiuscole e spazi non contano); se uno
// dei due non è un DN valido ripiega sul confronto del testo.
func sameDN(a, b string) bool {
	da, errA := ldap.ParseDN(a)
	db, errB := ldap.ParseDN(b)
	if errA != nil || errB != nil {
		return norm(a) == norm(b)
	}
	return da.EqualFold(db)
}

// NormalizeDN: forma canonica minuscola di un DN, per salvarlo e confrontarlo.
func NormalizeDN(s string) (string, error) {
	d, err := ldap.ParseDN(strings.TrimSpace(s))
	if err != nil || len(d.RDNs) == 0 {
		return "", ErrInvalidDN
	}
	return strings.ToLower(d.String()), nil
}

// ErrInvalidDN: testo che non è un DN LDAP.
var ErrInvalidDN = errors.New("audience: DN non valido")

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
