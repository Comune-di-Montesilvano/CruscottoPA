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
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

const (
	profileTTL      = 15 * time.Minute
	profileErrorTTL = time.Minute // AD giù: non interrogarlo a ogni pagina
)

type cachedProfile struct {
	p       audience.Profile
	ok      bool
	down    bool // AD non disponibile (diverso da "utente sconosciuto")
	expires time.Time
}

// profileCache tiene i profili AD per username (in memoria, per processo).
type profileCache struct {
	mu  sync.Mutex
	m   map[string]cachedProfile
	gen int // incrementato da reset: un caricamento iniziato prima non va salvato
	now func() time.Time
}

func newProfileCache(now func() time.Time) *profileCache {
	return &profileCache{m: map[string]cachedProfile{}, now: now}
}

// get: profilo dalla cache o da load. down = errore diverso da ErrUnknownUser.
func (c *profileCache) get(username string, load func() (audience.Profile, error)) (p audience.Profile, ok, down bool) {
	key := strings.ToLower(username)
	c.mu.Lock()
	e, hit := c.m[key]
	gen := c.gen
	c.mu.Unlock()
	if hit && c.now().Before(e.expires) {
		return e.p, e.ok, e.down
	}
	loaded, err := load()
	e = cachedProfile{p: loaded, ok: err == nil, expires: c.now().Add(profileTTL)}
	if err != nil {
		e.expires = c.now().Add(profileErrorTTL)
		if !errors.Is(err, identity.ErrUnknownUser) {
			e.down = true
			slog.Warn("profilo utente da AD", "err", err)
		}
	}
	c.mu.Lock()
	if c.gen == gen {
		c.m[key] = e
	}
	c.mu.Unlock()
	return e.p, e.ok, e.down
}

// profileFor: profilo AD di username (dalla cache). ok=false se username vuoto,
// directory assente, utente sconosciuto o AD non disponibile (down=true).
func (s *Server) profileFor(username string) (p audience.Profile, ok, down bool) {
	if username == "" || s.directory == nil {
		return audience.Profile{}, false, false
	}
	attrs, err := s.db.ListAudienceAttributes()
	if err != nil {
		slog.Warn("attributi dei gruppi", "err", err)
		return audience.Profile{}, false, true
	}
	names := make([]string, len(attrs))
	for i, a := range attrs {
		names[i] = a.Name
	}
	// mail e telefono servono al modulo ticket anche se non sono attributi dei gruppi.
	names = append(names, "mail", "telephoneNumber")
	return s.profiles.get(username, func() (audience.Profile, error) { return s.directory.Profile(username, names) })
}

// groupsFor: gruppi della plancia di username. known=false → solo pubblici;
// down=true se il motivo è AD (o il DB) non disponibile.
func (s *Server) groupsFor(username string) (memberOf map[int64]bool, known, down bool) {
	p, ok, down := s.profileFor(username)
	if !ok {
		return nil, false, down
	}
	rules, err := s.db.AllAudienceRules()
	if err != nil {
		slog.Warn("regole dei gruppi", "err", err)
		return nil, false, true
	}
	in := map[int64]bool{}
	for g, rs := range rules {
		if audience.Member(p, rs) {
			in[g] = true
		}
	}
	return in, true, false
}

// alertVisibleTo: stessa regola della plancia, per un utente (o "" = anonimo).
// unsure = avviso riservato e destinatario non verificabile ora (AD giù).
func (s *Server) alertVisibleTo(username string, alertID int64) (visible, unsure bool) {
	ca, err := s.db.GetContentAudience(database.ContentAlert, alertID)
	if err != nil {
		return false, true
	}
	if len(ca.Groups) == 0 {
		return true, false // pubblico
	}
	memberOf, known, down := s.groupsFor(username)
	return audience.Visible(ca.Mode, ca.Groups, memberOf, known), down
}

// viewerProfile: chi guarda e il suo profilo AD (dalla cache). ok=false se
// anonimo, senza cookie, senza directory o con AD non disponibile.
func (s *Server) viewerProfile(r *http.Request) (identity.User, audience.Profile, bool) {
	u, ok := s.viewer(r)
	if !ok || u.Anonymous {
		return u, audience.Profile{}, false
	}
	p, ok, _ := s.profileFor(u.Username)
	return u, p, ok
}

// viewerGroups: gruppi della plancia di chi guarda. known=false → solo pubblici.
func (s *Server) viewerGroups(r *http.Request) (map[int64]bool, bool) {
	u, ok := s.viewer(r)
	if !ok || u.Anonymous {
		return nil, false
	}
	in, known, _ := s.groupsFor(u.Username)
	return in, known
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
	c.gen++
	c.mu.Unlock()
}
