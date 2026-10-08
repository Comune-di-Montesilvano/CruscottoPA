package web

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// membersTTL: per quanto un conteggio dei membri resta buono. Corto: la
// pagina dei gruppi carica un conteggio per riga e lo rifà a ogni modifica.
const membersTTL = 30 * time.Second

var errNoDirectory = errors.New("directory non configurata")

type cachedMembers struct {
	count   int
	people  []identity.Person
	expires time.Time
}

// membersCache: risultati di Directory.Members per regole e colonne. La
// chiave è il contenuto delle regole, quindi un gruppo modificato non legge
// mai un risultato vecchio. Gli errori non si tengono.
type membersCache struct {
	mu  sync.Mutex
	m   map[string]cachedMembers
	now func() time.Time
}

func newMembersCache(now func() time.Time) *membersCache {
	return &membersCache{m: map[string]cachedMembers{}, now: now}
}

func (c *membersCache) get(rules []audience.Rule, attrs []string, load func() (int, []identity.Person, error)) (int, []identity.Person, error) {
	key := fmt.Sprintf("%q|%q", rules, attrs)
	now := c.now()
	c.mu.Lock()
	e, hit := c.m[key]
	c.mu.Unlock()
	if hit && now.Before(e.expires) {
		return e.count, e.people, nil
	}
	n, people, err := load()
	if err != nil {
		return 0, nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.m { // le anteprime delle bozze creano chiavi nuove a ogni tasto
		if !now.Before(v.expires) {
			delete(c.m, k)
		}
	}
	c.m[key] = cachedMembers{count: n, people: people, expires: now.Add(membersTTL)}
	return n, people, nil
}

// members: utenti AD che soddisfano le regole, con la cache di 30 secondi.
func (s *Server) members(rules []audience.Rule, attrs []string) (int, []identity.Person, error) {
	if s.directory == nil {
		return 0, nil, errNoDirectory
	}
	return s.membersCache.get(rules, attrs, func() (int, []identity.Person, error) {
		return s.directory.Members(rules, attrs)
	})
}
