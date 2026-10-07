// Package auth verifica le credenziali admin su Active Directory (porting
// di internal/auth di GoPulley) e limita i tentativi di login.
package auth

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

// Authenticator verifica username/password e dice se l'utente è admin.
type Authenticator interface {
	Authenticate(username, password string) (ok, admin bool, err error)
}

// LDAP implementa Authenticator su Active Directory / OpenLDAP.
type LDAP struct {
	cfg config.LDAP
}

func NewLDAP(cfg config.LDAP) *LDAP { return &LDAP{cfg: cfg} }

// usernameRe limita i caratteri ammessi: lo username finisce nel DN del bind.
var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,128}$`)

// IsAdminUser indica se username è nella lista esplicita (case-insensitive).
func IsAdminUser(list []string, username string) bool {
	for _, u := range list {
		if strings.EqualFold(u, username) {
			return true
		}
	}
	return false
}

func (l *LDAP) Authenticate(username, password string) (bool, bool, error) {
	username = strings.TrimSpace(username)
	if !usernameRe.MatchString(username) || password == "" {
		return false, false, nil
	}
	isAdmin := IsAdminUser(l.cfg.AdminUsers, username)

	if l.cfg.Host == "mock" {
		return true, isAdmin || len(l.cfg.AdminUsers) == 0, nil
	}

	conn, err := Dial(l.cfg)
	if err != nil {
		return false, false, err
	}
	defer conn.Close()

	userDN := fmt.Sprintf(l.cfg.UserDNTemplate, username)
	if err := conn.Bind(userDN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("ldap bind: %w", err)
	}

	if l.cfg.RequiredGroup == "" && l.cfg.AdminGroup == "" {
		return true, isAdmin, nil
	}

	if l.cfg.BindDN != "" {
		if err := conn.Bind(l.cfg.BindDN, l.cfg.BindPassword); err != nil {
			return false, false, fmt.Errorf("ldap bind di servizio: %w", err)
		}
	}

	entry, err := l.findUser(conn, username)
	if err != nil {
		return false, false, err
	}
	memberOf := entry.GetAttributeValues("memberOf")

	hasRequired := l.cfg.RequiredGroup == "" ||
		inGroup(memberOf, l.cfg.RequiredGroup) ||
		l.nestedMember(conn, l.cfg.RequiredGroup, entry.DN)
	if !isAdmin && l.cfg.AdminGroup != "" {
		isAdmin = inGroup(memberOf, l.cfg.AdminGroup) || l.nestedMember(conn, l.cfg.AdminGroup, entry.DN)
	}
	if !hasRequired && !isAdmin {
		slog.Info("ldap: utente fuori dal gruppo richiesto", "user", SafeLog(username), "group", l.cfg.RequiredGroup)
		return false, false, nil
	}
	return true, isAdmin, nil
}

// Dial apre la connessione LDAP con le stesse regole TLS del login admin
// (StartTLS fallito = errore, nessun ripiego in chiaro).
func Dial(cfg config.LDAP) (*ldap.Conn, error) {
	tlsCfg := &tls.Config{
		InsecureSkipVerify: cfg.TLSSkipVerify, //nolint:gosec // opzione esplicita per CA interne
		ServerName:         ldapHostname(cfg.Host),
	}
	// Timeout di apertura breve: senza, un DC che non risponde blocca la
	// richiesta (anche la plancia, che legge il profilo) fino a 60 s.
	conn, err := ldap.DialURL(cfg.Host, ldap.DialWithTLSConfig(tlsCfg),
		ldap.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}))
	if err != nil {
		return nil, fmt.Errorf("ldap dial: %w", err)
	}
	conn.SetTimeout(5 * time.Second)

	if cfg.StartTLS && strings.HasPrefix(cfg.Host, "ldap://") {
		if err := conn.StartTLS(tlsCfg); err != nil {
			conn.Close()
			return nil, fmt.Errorf("ldap StartTLS: %w (LDAP_STARTTLS=false solo su rete fidata)", err)
		}
	}
	return conn, nil
}

func (l *LDAP) findUser(conn *ldap.Conn, username string) (*ldap.Entry, error) {
	plain := strings.SplitN(username, "@", 2)[0]
	filter := fmt.Sprintf("(|(sAMAccountName=%s)(userPrincipalName=%s)(uid=%s))",
		ldap.EscapeFilter(plain), ldap.EscapeFilter(username), ldap.EscapeFilter(plain))
	res, err := conn.Search(ldap.NewSearchRequest(
		l.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		filter, []string{"dn", "memberOf"}, nil,
	))
	if err != nil {
		return nil, fmt.Errorf("ldap search: %w", err)
	}
	if len(res.Entries) == 0 {
		return nil, errors.New("ldap: utente non trovato sotto LDAP_BASE_DN")
	}
	return res.Entries[0], nil
}

// nestedMember usa LDAP_MATCHING_RULE_IN_CHAIN (solo AD) per i gruppi annidati.
func (l *LDAP) nestedMember(conn *ldap.Conn, group, userDN string) bool {
	attr := "cn"
	if strings.Contains(group, "=") {
		attr = "distinguishedName"
	}
	filter := fmt.Sprintf("(&(objectClass=group)(%s=%s)(member:1.2.840.113556.1.4.1941:=%s))",
		attr, ldap.EscapeFilter(group), ldap.EscapeFilter(userDN))
	res, err := conn.Search(ldap.NewSearchRequest(
		l.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		filter, []string{"cn"}, nil,
	))
	if err != nil {
		slog.Warn("ldap: ricerca gruppi annidati fallita", "group", group, "err", err)
		return false
	}
	return len(res.Entries) > 0
}

// inGroup confronta memberOf con un CN ("CED Admin") o un DN completo.
func inGroup(memberOf []string, group string) bool {
	g := strings.ToLower(group)
	for _, dn := range memberOf {
		d := strings.ToLower(dn)
		if d == g || strings.HasPrefix(d, "cn="+g+",") {
			return true
		}
	}
	return false
}

func ldapHostname(ldapURL string) string {
	u, err := url.Parse(ldapURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
