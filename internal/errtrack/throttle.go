package errtrack

import (
	"sync"
	"time"
)

const (
	dedupWindow = 10 * time.Minute // stessa chiave (fingerprint + utente) al massimo una volta
	perMinute   = 60               // tetto globale di eventi
	maxKeys     = 5000             // memoria limitata
)

// throttle: anti-pioggia comune a errori del server, panic ed errori del browser.
type throttle struct {
	mu     sync.Mutex
	seen   map[string]time.Time
	tokens float64
	last   time.Time
	now    func() time.Time
}

func newThrottle(now func() time.Time) *throttle {
	return &throttle{seen: map[string]time.Time{}, tokens: perMinute, last: now(), now: now}
}

func (t *throttle) allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if at, ok := t.seen[key]; ok && now.Sub(at) < dedupWindow {
		return false
	}
	t.tokens += now.Sub(t.last).Seconds() * perMinute / 60
	if t.tokens > perMinute {
		t.tokens = perMinute
	}
	t.last = now
	if t.tokens < 1 {
		return false
	}
	t.tokens--
	if _, ok := t.seen[key]; !ok && len(t.seen) >= maxKeys {
		t.evict(now)
	}
	t.seen[key] = now
	return true
}

// evict toglie le chiavi scadute; se non basta, la più vecchia.
func (t *throttle) evict(now time.Time) {
	var oldestK string
	var oldest time.Time
	for k, at := range t.seen {
		if now.Sub(at) >= dedupWindow {
			delete(t.seen, k)
			continue
		}
		if oldestK == "" || at.Before(oldest) {
			oldestK, oldest = k, at
		}
	}
	if len(t.seen) >= maxKeys && oldestK != "" {
		delete(t.seen, oldestK)
	}
}
