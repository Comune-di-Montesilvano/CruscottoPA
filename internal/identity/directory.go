package identity

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/auth"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

// ErrUnknownUser: utente inesistente, disattivato o con nome non ammesso.
var ErrUnknownUser = errors.New("identity: utente non trovato in AD")

// Person è un utente di AD come serve alla plancia.
type Person struct {
	Username  string // sAMAccountName, minuscolo
	Name      string // displayName, "" se assente
	GivenName string // givenName, "" se assente
}

// ADGroup è un gruppo di Active Directory.
type ADGroup struct{ DN, Name string }

// Directory cerca utenti, gruppi e valori in AD.
type Directory interface {
	Lookup(username string) (Person, error)
	// Profile: attributi richiesti (chiavi minuscole) e gruppi, annidati compresi.
	Profile(username string, attrs []string) (audience.Profile, error)
	SearchGroups(q string) ([]ADGroup, error)
	SearchUsers(q string) ([]Person, error)
	AttributeValues(attr string) ([]string, error)
	// Members: utenti che soddisfano le regole (totale e primi 30).
	Members(rules []audience.Rule) (int, []Person, error)
}

// Stessa regola dello username del login admin.
var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,128}$`)

var attrNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)

// ValidAttrName: nome di attributo LDAP accettabile (finisce nei filtri).
func ValidAttrName(s string) bool { return attrNameRe.MatchString(s) }

const activeUsersFilter = `(objectCategory=person)(objectClass=user)(!(userAccountControl:1.2.840.113556.1.4.803:=2))`

const (
	searchLimit  = 20
	previewLimit = 30
	valuesTTL    = 6 * time.Hour
)

// userFilter cerca per sAMAccountName (il nome che NTLM trasmette), solo utenti attivi.
func userFilter(username string) (string, error) {
	if !usernameRe.MatchString(username) {
		return "", ErrUnknownUser
	}
	return fmt.Sprintf("(&%s(sAMAccountName=%s))", activeUsersFilter, ldap.EscapeFilter(username)), nil
}

// searchFilter: sottostringa q (escapata) in uno degli attributi indicati.
func searchFilter(q string, attrs ...string) string {
	esc := ldap.EscapeFilter(strings.TrimSpace(q))
	var b strings.Builder
	b.WriteString("(|")
	for _, a := range attrs {
		fmt.Fprintf(&b, "(%s=*%s*)", a, esc)
	}
	b.WriteString(")")
	return b.String()
}

// membersFilter traduce le regole in un filtro sugli utenti attivi; false se
// non c'è nessuna regola positiva (gruppo senza membri).
func membersFilter(rules []audience.Rule) (string, bool) {
	var or, not strings.Builder
	for _, r := range rules {
		v := ldap.EscapeFilter(strings.TrimSpace(r.Value))
		switch r.Kind {
		case audience.KindAttr:
			if ValidAttrName(r.Attr) {
				fmt.Fprintf(&or, "(%s=%s)", r.Attr, v)
			}
		case audience.KindADGroup:
			fmt.Fprintf(&or, "(memberOf:1.2.840.113556.1.4.1941:=%s)", v)
		case audience.KindUser:
			fmt.Fprintf(&or, "(sAMAccountName=%s)", v)
		case audience.KindExclude:
			fmt.Fprintf(&not, "(!(sAMAccountName=%s))", v)
		}
	}
	if or.Len() == 0 {
		return "", false
	}
	return "(&" + activeUsersFilter + "(|" + or.String() + ")" + not.String() + ")", true
}

var personAttrs = []string{"sAMAccountName", "displayName", "givenName"}

func personFromEntry(e *ldap.Entry) Person {
	return Person{
		Username:  strings.ToLower(e.GetAttributeValue("sAMAccountName")),
		Name:      strings.TrimSpace(e.GetAttributeValue("displayName")),
		GivenName: strings.TrimSpace(e.GetAttributeValue("givenName")),
	}
}

func sortPeople(p []Person) {
	sort.Slice(p, func(i, j int) bool {
		return strings.ToLower(p[i].Name+p[i].Username) < strings.ToLower(p[j].Name+p[j].Username)
	})
}

// LDAPDirectory interroga AD con l'account di servizio (LDAP_BIND_DN).
type LDAPDirectory struct {
	cfg config.LDAP

	values *valuesCache
}

func NewLDAPDirectory(cfg config.LDAP) *LDAPDirectory {
	return &LDAPDirectory{cfg: cfg, values: newValuesCache(time.Now)}
}

func (d *LDAPDirectory) conn() (*ldap.Conn, error) {
	conn, err := auth.Dial(d.cfg)
	if err != nil {
		return nil, err
	}
	if err := conn.Bind(d.cfg.BindDN, d.cfg.BindPassword); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ldap bind di servizio: %w", err)
	}
	return conn, nil
}

func (d *LDAPDirectory) search(conn *ldap.Conn, filter string, attrs []string, limit int) ([]*ldap.Entry, error) {
	req := ldap.NewSearchRequest(d.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, limit, 10, false, filter, attrs, nil)
	var res *ldap.SearchResult
	var err error
	if limit == 0 {
		res, err = conn.SearchWithPaging(req, 500)
	} else {
		res, err = conn.Search(req)
		if ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) && res != nil {
			err = nil // bastano i primi risultati
		}
	}
	if err != nil {
		return nil, fmt.Errorf("ldap search: %w", err)
	}
	return res.Entries, nil
}

func (d *LDAPDirectory) Lookup(username string) (Person, error) {
	filter, err := userFilter(username)
	if err != nil {
		return Person{}, err
	}
	conn, err := d.conn()
	if err != nil {
		return Person{}, err
	}
	defer conn.Close()
	entries, err := d.search(conn, filter, personAttrs, 1)
	if err != nil {
		return Person{}, err
	}
	if len(entries) == 0 {
		return Person{}, ErrUnknownUser
	}
	return personFromEntry(entries[0]), nil
}

func (d *LDAPDirectory) Profile(username string, attrs []string) (audience.Profile, error) {
	filter, err := userFilter(username)
	if err != nil {
		return audience.Profile{}, err
	}
	want := []string{"sAMAccountName"}
	for _, a := range attrs {
		if ValidAttrName(a) {
			want = append(want, a)
		}
	}
	conn, err := d.conn()
	if err != nil {
		return audience.Profile{}, err
	}
	defer conn.Close()
	entries, err := d.search(conn, filter, want, 1)
	if err != nil {
		return audience.Profile{}, err
	}
	if len(entries) == 0 {
		return audience.Profile{}, ErrUnknownUser
	}
	e := entries[0]
	p := audience.Profile{Username: strings.ToLower(e.GetAttributeValue("sAMAccountName")), Attrs: map[string][]string{}, Groups: []string{}}
	for _, a := range want[1:] {
		for _, v := range e.GetAttributeValues(a) {
			if v = strings.TrimSpace(v); v != "" {
				p.Attrs[strings.ToLower(a)] = append(p.Attrs[strings.ToLower(a)], v)
			}
		}
	}
	groups, err := d.search(conn, "(&(objectClass=group)(member:1.2.840.113556.1.4.1941:="+ldap.EscapeFilter(e.DN)+"))", []string{"dn"}, 0)
	if err != nil {
		return audience.Profile{}, err
	}
	for _, g := range groups {
		p.Groups = append(p.Groups, g.DN)
	}
	return p, nil
}

func (d *LDAPDirectory) SearchGroups(q string) ([]ADGroup, error) {
	if strings.TrimSpace(q) == "" {
		return []ADGroup{}, nil
	}
	conn, err := d.conn()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	entries, err := d.search(conn, "(&(objectClass=group)"+searchFilter(q, "cn")+")", []string{"cn"}, searchLimit)
	if err != nil {
		return nil, err
	}
	out := make([]ADGroup, 0, len(entries))
	for _, e := range entries {
		out = append(out, ADGroup{DN: e.DN, Name: e.GetAttributeValue("cn")})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func (d *LDAPDirectory) SearchUsers(q string) ([]Person, error) {
	if strings.TrimSpace(q) == "" {
		return []Person{}, nil
	}
	conn, err := d.conn()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	entries, err := d.search(conn, "(&"+activeUsersFilter+searchFilter(q, "sAMAccountName", "displayName")+")", personAttrs, searchLimit)
	if err != nil {
		return nil, err
	}
	out := make([]Person, 0, len(entries))
	for _, e := range entries {
		out = append(out, personFromEntry(e))
	}
	sortPeople(out)
	return out, nil
}

// AttributeValues: valori distinti tra gli utenti attivi (cache, vedi valuesCache).
func (d *LDAPDirectory) AttributeValues(attr string) ([]string, error) {
	if !ValidAttrName(attr) {
		return nil, fmt.Errorf("nome di attributo non valido: %q", attr)
	}
	return d.values.get(attr, func() ([]string, error) { return d.loadValues(attr) })
}

func (d *LDAPDirectory) loadValues(attr string) ([]string, error) {
	conn, err := d.conn()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	entries, err := d.search(conn, "(&"+activeUsersFilter+"("+attr+"=*))", []string{attr}, 0)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	list := []string{}
	for _, e := range entries {
		v := strings.TrimSpace(e.GetAttributeValue(attr))
		if k := strings.ToUpper(v); v != "" && !seen[k] {
			seen[k] = true
			list = append(list, v)
		}
	}
	sort.Strings(list)
	return list, nil
}

func (d *LDAPDirectory) Members(rules []audience.Rule) (int, []Person, error) {
	filter, ok := membersFilter(rules)
	if !ok {
		return 0, []Person{}, nil
	}
	conn, err := d.conn()
	if err != nil {
		return 0, nil, err
	}
	defer conn.Close()
	entries, err := d.search(conn, filter, personAttrs, 0)
	if err != nil {
		return 0, nil, err
	}
	people := make([]Person, 0, len(entries))
	for _, e := range entries {
		people = append(people, personFromEntry(e))
	}
	sortPeople(people)
	if len(people) > previewLimit {
		return len(entries), people[:previewLimit], nil
	}
	return len(entries), people, nil
}

// MockDirectory: per LDAP_HOST=mock (solo sviluppo).
type MockDirectory struct{}

var mockGroups = []ADGroup{
	{DN: "CN=Amministrativi Mock,OU=Mock,DC=mock", Name: "Amministrativi Mock"},
	{DN: "CN=Utenti Mock,OU=Mock,DC=mock", Name: "Utenti Mock"},
}

func (MockDirectory) Lookup(username string) (Person, error) {
	if !usernameRe.MatchString(username) {
		return Person{}, ErrUnknownUser
	}
	return Person{Username: strings.ToLower(username), Name: username}, nil
}

func (MockDirectory) Profile(username string, attrs []string) (audience.Profile, error) {
	if !usernameRe.MatchString(username) {
		return audience.Profile{}, ErrUnknownUser
	}
	p := audience.Profile{Username: strings.ToLower(username), Attrs: map[string][]string{}, Groups: []string{mockGroups[1].DN}}
	for _, a := range attrs {
		if strings.EqualFold(a, "physicalDeliveryOfficeName") {
			p.Attrs["physicaldeliveryofficename"] = []string{"INFORMATIZZAZIONE"}
		}
	}
	return p, nil
}

func (MockDirectory) SearchGroups(q string) ([]ADGroup, error) {
	out := []ADGroup{}
	for _, g := range mockGroups {
		if q != "" && strings.Contains(strings.ToLower(g.Name), strings.ToLower(q)) {
			out = append(out, g)
		}
	}
	return out, nil
}

func (MockDirectory) SearchUsers(q string) ([]Person, error) {
	if !usernameRe.MatchString(q) {
		return []Person{}, nil
	}
	return []Person{{Username: strings.ToLower(q), Name: q}}, nil
}

func (MockDirectory) AttributeValues(attr string) ([]string, error) {
	if !ValidAttrName(attr) {
		return nil, fmt.Errorf("nome di attributo non valido: %q", attr)
	}
	return []string{"AMMINISTRATIVO", "INFORMATIZZAZIONE", "TRIBUTI"}, nil
}

func (MockDirectory) Members(rules []audience.Rule) (int, []Person, error) {
	if _, ok := membersFilter(rules); !ok {
		return 0, []Person{}, nil
	}
	return 1, []Person{{Username: "mock", Name: "Utente Mock"}}, nil
}
