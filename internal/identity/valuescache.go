package identity

import (
	"strings"
	"sync"
	"time"
)

const valuesErrorTTL = time.Minute

// ttlCache tiene risultati letti da AD (valori di un attributo, statistiche) per 6 ore. Il
// caricamento avviene senza tenere il lock (AD lento non blocca le altre
// richieste) e un errore si ricorda per 1 minuto: con AD giù ogni tasto
// premuto nei suggerimenti non aspetta un nuovo timeout.
type ttlCache[T any] struct {
	mu  sync.Mutex
	m   map[string]ttlEntry[T]
	now func() time.Time
}

type ttlEntry[T any] struct {
	list   T
	have   bool      // list contiene un valore valido
	until  time.Time // fino a quando non si ricarica
	errMsg error     // ultimo errore, se il caricamento è fallito
}

type valuesCache = ttlCache[[]string]

func newValuesCache(now func() time.Time) *valuesCache { return newTTLCache[[]string](now) }

func newTTLCache[T any](now func() time.Time) *ttlCache[T] {
	return &ttlCache[T]{m: map[string]ttlEntry[T]{}, now: now}
}

func (c *ttlCache[T]) get(key string, load func() (T, error)) (T, error) {
	key = strings.ToLower(key)
	c.mu.Lock()
	e := c.m[key]
	c.mu.Unlock()
	if c.now().Before(e.until) {
		if e.have {
			return e.list, nil
		}
		var zero T
		return zero, e.errMsg
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
		var zero T
		return zero, err
	}
	c.m[key] = ttlEntry[T]{list: list, have: true, until: c.now().Add(valuesTTL)}
	return list, nil
}
