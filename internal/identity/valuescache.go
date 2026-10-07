package identity

import (
	"strings"
	"sync"
	"time"
)

const valuesErrorTTL = time.Minute

// valuesCache tiene i valori distinti di un attributo AD per 6 ore. Il
// caricamento avviene senza tenere il lock (AD lento non blocca le altre
// richieste) e un errore si ricorda per 1 minuto: con AD giù ogni tasto
// premuto nei suggerimenti non aspetta un nuovo timeout.
type valuesCache struct {
	mu  sync.Mutex
	m   map[string]valuesEntry
	now func() time.Time
}

type valuesEntry struct {
	list   []string
	have   bool      // list contiene un elenco valido
	until  time.Time // fino a quando non si ricarica
	errMsg error     // ultimo errore, se il caricamento è fallito
}

func newValuesCache(now func() time.Time) *valuesCache {
	return &valuesCache{m: map[string]valuesEntry{}, now: now}
}

func (c *valuesCache) get(key string, load func() ([]string, error)) ([]string, error) {
	key = strings.ToLower(key)
	c.mu.Lock()
	e := c.m[key]
	c.mu.Unlock()
	if c.now().Before(e.until) {
		if e.have {
			return e.list, nil
		}
		return nil, e.errMsg
	}
	list, err := load()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		e.until, e.errMsg = c.now().Add(valuesErrorTTL), err
		c.m[key] = e
		if e.have {
			return e.list, nil
		}
		return nil, err
	}
	c.m[key] = valuesEntry{list: list, have: true, until: c.now().Add(valuesTTL)}
	return list, nil
}
