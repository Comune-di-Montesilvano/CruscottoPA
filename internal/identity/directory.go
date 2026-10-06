package identity

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-ldap/ldap/v3"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/auth"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

// ErrUnknownUser: utente inesistente, disattivato o con nome non ammesso.
var ErrUnknownUser = errors.New("identity: utente non trovato in AD")

// Person è un utente di AD come serve alla plancia.
type Person struct {
	Username string // sAMAccountName, minuscolo
	Name     string // displayName, "" se assente
}

// Directory cerca gli utenti riconosciuti via NTLM.
type Directory interface {
	Lookup(username string) (Person, error)
}

// Stessa regola dello username del login admin.
var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,128}$`)

// userFilter cerca per sAMAccountName (il nome che NTLM trasmette), solo utenti attivi.
func userFilter(username string) (string, error) {
	if !usernameRe.MatchString(username) {
		return "", ErrUnknownUser
	}
	return fmt.Sprintf("(&(objectCategory=person)(objectClass=user)(!(userAccountControl:1.2.840.113556.1.4.803:=2))(sAMAccountName=%s))",
		ldap.EscapeFilter(username)), nil
}

// LDAPDirectory interroga AD con l'account di servizio (LDAP_BIND_DN).
type LDAPDirectory struct{ cfg config.LDAP }

func NewLDAPDirectory(cfg config.LDAP) *LDAPDirectory { return &LDAPDirectory{cfg: cfg} }

func (d *LDAPDirectory) Lookup(username string) (Person, error) {
	filter, err := userFilter(username)
	if err != nil {
		return Person{}, err
	}
	conn, err := auth.Dial(d.cfg)
	if err != nil {
		return Person{}, err
	}
	defer conn.Close()
	if err := conn.Bind(d.cfg.BindDN, d.cfg.BindPassword); err != nil {
		return Person{}, fmt.Errorf("ldap bind di servizio: %w", err)
	}
	res, err := conn.Search(ldap.NewSearchRequest(d.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, 5, false,
		filter, []string{"sAMAccountName", "displayName"}, nil))
	if err != nil {
		return Person{}, fmt.Errorf("ldap search: %w", err)
	}
	if len(res.Entries) == 0 {
		return Person{}, ErrUnknownUser
	}
	e := res.Entries[0]
	return Person{
		Username: strings.ToLower(e.GetAttributeValue("sAMAccountName")),
		Name:     strings.TrimSpace(e.GetAttributeValue("displayName")),
	}, nil
}

// MockDirectory: per LDAP_HOST=mock (solo sviluppo).
type MockDirectory struct{}

func (MockDirectory) Lookup(username string) (Person, error) {
	if !usernameRe.MatchString(username) {
		return Person{}, ErrUnknownUser
	}
	return Person{Username: strings.ToLower(username), Name: username}, nil
}
