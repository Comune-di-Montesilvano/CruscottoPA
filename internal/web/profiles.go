package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/auth"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

const (
	profileTTL      = 15 * time.Minute
	profileErrorTTL = time.Minute // AD giù: non interrogarlo a ogni pagina
)

type cachedProfile struct {
	p       audience.Profile
	ok      bool
	expires time.Time
}

// profileCache tiene i profili AD per username (in memoria, per processo).
type profileCache struct {
	mu  sync.Mutex
	m   map[string]cachedProfile
	now func() time.Time
}

func newProfileCache(now func() time.Time) *profileCache {
	return &profileCache{m: map[string]cachedProfile{}, now: now}
}

func (c *profileCache) get(username string, load func() (audience.Profile, error)) (audience.Profile, bool) {
	key := strings.ToLower(username)
	c.mu.Lock()
	e, hit := c.m[key]
	c.mu.Unlock()
	if hit && c.now().Before(e.expires) {
		return e.p, e.ok
	}
	p, err := load()
	e = cachedProfile{p: p, ok: err == nil, expires: c.now().Add(profileTTL)}
	if err != nil {
		e.expires = c.now().Add(profileErrorTTL)
		if !errors.Is(err, identity.ErrUnknownUser) {
			slog.Warn("profilo utente da AD", "err", err)
		}
	}
	c.mu.Lock()
	c.m[key] = e
	c.mu.Unlock()
	return e.p, e.ok
}

// viewerProfile: chi guarda e il suo profilo AD (dalla cache). ok=false se
// anonimo, senza cookie, senza directory o con AD non disponibile.
func (s *Server) viewerProfile(r *http.Request) (identity.User, audience.Profile, bool) {
	u, ok := s.viewer(r)
	if !ok || u.Anonymous || u.Username == "" || s.directory == nil {
		return u, audience.Profile{}, false
	}
	attrs, err := s.db.ListAudienceAttributes()
	if err != nil {
		slog.Warn("attributi dei gruppi", "err", err)
		return u, audience.Profile{}, false
	}
	names := make([]string, len(attrs))
	for i, a := range attrs {
		names[i] = a.Name
	}
	p, ok := s.profiles.get(u.Username, func() (audience.Profile, error) { return s.directory.Profile(u.Username, names) })
	return u, p, ok
}

// viewerGroups: gruppi della plancia di chi guarda. known=false → solo pubblici.
func (s *Server) viewerGroups(r *http.Request) (map[int64]bool, bool) {
	_, p, ok := s.viewerProfile(r)
	if !ok {
		return nil, false
	}
	rules, err := s.db.AllAudienceRules()
	if err != nil {
		slog.Warn("regole dei gruppi", "err", err)
		return nil, false
	}
	in := map[int64]bool{}
	for g, rs := range rules {
		if audience.Member(p, rs) {
			in[g] = true
		}
	}
	return in, true
}

// viewerIsAdmin decide se mostrare in plancia il link al pannello admin: utente
// in ADMIN_USERS o nel gruppo LDAP_ADMIN_GROUP (anche annidato). È solo una
// comodità: l'identità è dichiarata e /admin resta dietro il login LDAP.
func (s *Server) viewerIsAdmin(r *http.Request) bool {
	u, p, ok := s.viewerProfile(r)
	if u.Anonymous || u.Username == "" {
		return false
	}
	if auth.IsAdminUser(s.cfg.LDAP.AdminUsers, u.Username) {
		return true
	}
	if s.cfg.LDAP.Host == "mock" && len(s.cfg.LDAP.AdminUsers) == 0 {
		return true // stessa regola del login admin in mock
	}
	if !ok || s.cfg.LDAP.AdminGroup == "" {
		return false
	}
	for _, dn := range p.Groups {
		if cn, _, _ := strings.Cut(dn, ","); strings.EqualFold(strings.TrimPrefix(strings.TrimPrefix(cn, "CN="), "cn="), s.cfg.LDAP.AdminGroup) {
			return true
		}
	}
	return false
}
func (c *profileCache) reset() {
	c.mu.Lock()
	c.m = map[string]cachedProfile{}
	c.mu.Unlock()
}
