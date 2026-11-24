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
	Office    string // department, altrimenti physicalDeliveryOfficeName (omonimi nella chat)
	// Attrs: valori degli attributi richiesti (es. nella tabella dell'anteprima).
	Attrs map[string]string
}

// ADGroup è un gruppo di Active Directory.
type ADGroup struct{ DN, Name string }

// Directory cerca utenti, gruppi e valori in AD.
type Directory interface {
	Lookup(username string) (Person, error)
	// Profile: attributi richiesti (chiavi minuscole) e gruppi, annidati compresi.
	Profile(username string, attrs []string) (audience.Profile, error)
	SearchGroups(q string) ([]ADGroup, error)
	// SearchOUs: unità organizzative per nome (Name = nome dell'OU).
	SearchOUs(q string) ([]ADGroup, error)
	SearchUsers(q string) ([]Person, error)
	AttributeValues(attr string) ([]string, error)
	// Members: utenti che soddisfano le regole (totale e primi 30), con i
	// valori degli attributi attrs.
	Members(rules []audience.Rule, attrs []string) (int, []Person, error)
	// AttributeStats: attributi compilati sugli utenti, filtrati per nome.
	AttributeStats(q string) ([]AttrStat, error)
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

// userFilter cerca per sAMAccountName (il nome che NTLM trasmette), solo utenti
// attivi. Con una @ è un UPN (accesso come utente@dominio): si cerca per
// userPrincipalName, e AD stesso dice se il suffisso è dell'ente.
func userFilter(username string) (string, error) {
	if !usernameRe.MatchString(username) {
		return "", ErrUnknownUser
	}
	attr := "sAMAccountName"
	if strings.Contains(username, "@") {
		attr = "userPrincipalName"
	}
	return fmt.Sprintf("(&%s(%s=%s))", activeUsersFilter, attr, ldap.EscapeFilter(username)), nil
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
// non c'è nessuna regola positiva (gruppo senza membri). I requisiti vanno in
// AND con le esclusioni.
// membersQuery: come cercare i membri. AD non filtra per OU (niente :dn:):
// ogni OU da includere è una ricerca a parte con quella base, le OU escluse si
// tolgono dai risultati.
type membersQuery struct {
	filter     string   // ricerca su tutta la base ("" = nessuna)
	bases      []string // OU da includere
	baseFilter string   // filtro delle ricerche nelle OU (attivi, esclusioni, requisiti)
	excludeOUs []string
}

func (q membersQuery) keep(dn string) bool {
	for _, ou := range q.excludeOUs {
		if audience.InOU(dn, ou) {
			return false
		}
	}
	return true
}

func membersPlan(rules []audience.Rule) (membersQuery, bool) {
	var q membersQuery
	for _, r := range rules {
		switch r.Kind {
		case audience.KindOU:
			q.bases = append(q.bases, strings.TrimSpace(r.Value))
		case audience.KindExcludeOU:
			q.excludeOUs = append(q.excludeOUs, strings.TrimSpace(r.Value))
		}
	}
	filter, not, ok := membersFilterParts(rules)
	if ok {
		q.filter = filter
	}
	q.baseFilter = "(&" + activeUsersFilter + not + ")"
	return q, ok || len(q.bases) > 0
}

func membersFilter(rules []audience.Rule) (string, bool) {
	f, _, ok := membersFilterParts(rules)
	return f, ok
}

func membersFilterParts(rules []audience.Rule) (string, string, bool) {
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
		case audience.KindPresent:
			if ValidAttrName(r.Attr) {
				fmt.Fprintf(&not, "(%s=*)", r.Attr)
			}
		case audience.KindAbsent:
			if ValidAttrName(r.Attr) {
				fmt.Fprintf(&not, "(!(%s=*))", r.Attr)
			}
		}
	}
	if or.Len() == 0 {
		return "", not.String(), false
	}
	return "(&" + activeUsersFilter + "(|" + or.String() + ")" + not.String() + ")", not.String(), true
}

var personAttrs = []string{"sAMAccountName", "displayName", "givenName", "department", "physicalDeliveryOfficeName"}

func personFromEntry(e *ldap.Entry) Person {
	return Person{
		Username:  strings.ToLower(e.GetAttributeValue("sAMAccountName")),
		Name:      strings.TrimSpace(e.GetAttributeValue("displayName")),
		GivenName: strings.TrimSpace(e.GetAttributeValue("givenName")),
		Office:    personOffice(e.GetAttributeValue("department"), e.GetAttributeValue("physicalDeliveryOfficeName")),
	}
}

func personOffice(department, office string) string {
	if d := strings.TrimSpace(department); d != "" {
		return d
	}
	return strings.TrimSpace(office)
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
	stats  *ttlCache[[]AttrStat]
}

func NewLDAPDirectory(cfg config.LDAP) *LDAPDirectory {
	return &LDAPDirectory{cfg: cfg, values: newValuesCache(time.Now), stats: newTTLCache[[]AttrStat](time.Now)}
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
	return d.searchIn(conn, d.cfg.BaseDN, filter, attrs, limit)
}

func (d *LDAPDirectory) searchIn(conn *ldap.Conn, base, filter string, attrs []string, limit int) ([]*ldap.Entry, error) {
	req := ldap.NewSearchRequest(base, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, limit, 10, false, filter, attrs, nil)
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
	p := audience.Profile{Username: strings.ToLower(e.GetAttributeValue("sAMAccountName")), Attrs: map[string][]string{}, Groups: []string{}, DN: e.DN}
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

func (d *LDAPDirectory) Members(rules []audience.Rule, attrs []string) (int, []Person, error) {
	plan, ok := membersPlan(rules)
	if !ok {
		return 0, []Person{}, nil
	}
	conn, err := d.conn()
	if err != nil {
		return 0, nil, err
	}
	defer conn.Close()
	want := append([]string{}, personAttrs...)
	for _, a := range attrs {
		if ValidAttrName(a) {
			want = append(want, a)
		}
	}
	var found []*ldap.Entry
	if plan.filter != "" {
		es, err := d.search(conn, plan.filter, want, 0)
		if err != nil {
			return 0, nil, err
		}
		found = append(found, es...)
	}
	for _, base := range plan.bases {
		es, err := d.searchIn(conn, base, plan.baseFilter, want, 0)
		if ldap.IsErrorWithCode(errors.Unwrap(err), ldap.LDAPResultNoSuchObject) {
			continue // OU cancellata o spostata: nessun membro da lì
		} else if err != nil {
			return 0, nil, err
		}
		found = append(found, es...)
	}
	seen := map[string]bool{}
	entries := found[:0]
	for _, e := range found {
		if k := strings.ToLower(e.DN); !seen[k] && plan.keep(e.DN) {
			seen[k] = true
			entries = append(entries, e)
		}
	}
	people := make([]Person, 0, len(entries))
	for _, e := range entries {
		p := personFromEntry(e)
		p.Attrs = map[string]string{}
		for _, a := range attrs {
			p.Attrs[a] = strings.Join(e.GetAttributeValues(a), ", ")
		}
		people = append(people, p)
	}
	sortPeople(people)
	if len(people) > previewLimit {
		return len(entries), people[:previewLimit], nil
	}
	return len(entries), people, nil
}

// AttributeStats: attributi compilati sugli utenti attivi (cache di 6 ore).
func (d *LDAPDirectory) AttributeStats(q string) ([]AttrStat, error) {
	all, err := d.stats.get("all", func() ([]AttrStat, error) {
		conn, err := d.conn()
		if err != nil {
			return nil, err
		}
		defer conn.Close()
		entries, err := d.search(conn, "(&"+activeUsersFilter+")", []string{"*"}, 0)
		if err != nil {
			return nil, err
		}
		return computeStats(entries), nil
	})
	if err != nil {
		return nil, err
	}
	return filterStats(all, q), nil
}

// MockDirectory: per LDAP_HOST=mock (solo sviluppo).
type MockDirectory struct{}

var mockGroups = []ADGroup{
	{DN: "CN=Amministrativi Mock,OU=Mock,DC=mock", Name: "Amministrativi Mock"},
	{DN: "CN=Utenti Mock,OU=Mock,DC=mock", Name: "Utenti Mock"},
}

// mockPeople: colleghi di esempio con nome e cognome (ricerca della chat).
// Gli altri username restano validi, con il nome uguale allo username.
var mockPeople = []Person{
	{Username: "mrossi", Name: "Mario Rossi"},
	{Username: "abianchi", Name: "Anna Bianchi"},
	{Username: "lverdi", Name: "Luca Verdi"},
	{Username: "gesposito", Name: "Giulia Esposito"},
}

func mockName(username string) string {
	for _, p := range mockPeople {
		if p.Username == strings.ToLower(username) {
			return p.Name
		}
	}
	return username
}

func (MockDirectory) Lookup(username string) (Person, error) {
	if !usernameRe.MatchString(username) {
		return Person{}, ErrUnknownUser
	}
	username, _, _ = strings.Cut(username, "@") // UPN: il mock usa la parte prima della @
	return Person{Username: strings.ToLower(username), Name: mockName(username)}, nil
}

func (MockDirectory) Profile(username string, attrs []string) (audience.Profile, error) {
	if !usernameRe.MatchString(username) {
		return audience.Profile{}, ErrUnknownUser
	}
	p := audience.Profile{Username: strings.ToLower(username), Attrs: map[string][]string{}, Groups: []string{mockGroups[1].DN},
		DN: "CN=" + ldap.EscapeDN(username) + ",OU=Settore 2,DC=mock"}
	mock := map[string]string{
		"physicaldeliveryofficename": "INFORMATIZZAZIONE",
		"description":                "CED",
		"mail":                       strings.ToLower(username) + "@example.it",
		"telephonenumber":            "731",
		"displayname":                mockName(username),
	}
	for _, a := range attrs {
		if v, ok := mock[strings.ToLower(a)]; ok {
			p.Attrs[strings.ToLower(a)] = []string{v}
		}
	}
	return p, nil
}

var mockOUs = []ADGroup{
	{DN: "OU=Settore 2,DC=mock", Name: "Settore 2"},
	{DN: "OU=Esterni,OU=Settore 2,DC=mock", Name: "Esterni"},
}

func (MockDirectory) SearchOUs(q string) ([]ADGroup, error) {
	out := []ADGroup{}
	for _, o := range mockOUs {
		if q != "" && strings.Contains(strings.ToLower(o.Name), strings.ToLower(q)) {
			out = append(out, o)
		}
	}
	return out, nil
}

func (d *LDAPDirectory) SearchOUs(q string) ([]ADGroup, error) {
	if strings.TrimSpace(q) == "" {
		return []ADGroup{}, nil
	}
	conn, err := d.conn()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	entries, err := d.search(conn, "(&(objectClass=organizationalUnit)"+searchFilter(q, "ou")+")", []string{"ou"}, searchLimit)
	if err != nil {
		return nil, err
	}
	out := make([]ADGroup, 0, len(entries))
	for _, e := range entries {
		out = append(out, ADGroup{DN: e.DN, Name: e.GetAttributeValue("ou")})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].DN) < strings.ToLower(out[j].DN) })
	return out, nil
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
	out := []Person{}
	lq := strings.ToLower(strings.TrimSpace(q))
	for _, p := range mockPeople {
		if lq != "" && (strings.Contains(strings.ToLower(p.Name), lq) || strings.HasPrefix(p.Username, lq)) {
			p.Office = "INFORMATIZZAZIONE"
			out = append(out, p)
		}
	}
	if len(out) == 0 && usernameRe.MatchString(q) {
		out = append(out, Person{Username: strings.ToLower(q), Name: q})
	}
	return out, nil
}

func (MockDirectory) AttributeValues(attr string) ([]string, error) {
	if !ValidAttrName(attr) {
		return nil, fmt.Errorf("nome di attributo non valido: %q", attr)
	}
	return []string{"AMMINISTRATIVO", "INFORMATIZZAZIONE", "TRIBUTI"}, nil
}

func (MockDirectory) Members(rules []audience.Rule, attrs []string) (int, []Person, error) {
	if plan, ok := membersPlan(rules); !ok || !plan.keep("CN=mock,OU=Settore 2,DC=mock") {
		return 0, []Person{}, nil
	}
	p := Person{Username: "mock", Name: "Utente Mock", Attrs: map[string]string{}}
	for _, a := range attrs {
		if strings.EqualFold(a, "physicalDeliveryOfficeName") {
			p.Attrs[a] = "INFORMATIZZAZIONE"
		}
	}
	return 1, []Person{p}, nil
}

func (MockDirectory) AttributeStats(q string) ([]AttrStat, error) {
	return filterStats([]AttrStat{
		{Name: "physicalDeliveryOfficeName", Count: 3, Examples: []string{"INFORMATIZZAZIONE", "TRIBUTI"}},
		{Name: "department", Count: 1, Examples: []string{"Ragioneria"}},
	}, q), nil
}
