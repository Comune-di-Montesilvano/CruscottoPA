package errtrack

import (
	"sync"
	"time"
)

const (
	dedupWindow      = 10 * time.Minute // stessa chiave (fingerprint + utente) al massimo una volta
	perMinute        = 60               // tetto degli errori del server (slog, panic)
	browserPerMinute = 30               // tetto a parte per /errori: non può zittire il server
	maxKeys          = 5000             // memoria limitata
)

// bucket: token bucket di n eventi al minuto.
type bucket struct {
	tokens, rate float64
	last         time.Time
}

func (b *bucket) take(now time.Time) bool {
	b.tokens += now.Sub(b.last).Seconds() * b.rate / 60
	if b.tokens > b.rate {
		b.tokens = b.rate
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// throttle: anti-pioggia comune a errori del server, panic ed errori del browser.
type throttle struct {
	mu              sync.Mutex
	seen            map[string]time.Time
	server, browser bucket
	now             func() time.Time
}

func newThrottle(now func() time.Time) *throttle {
	t0 := now()
	return &throttle{
		seen:    map[string]time.Time{},
		server:  bucket{tokens: perMinute, rate: perMinute, last: t0},
		browser: bucket{tokens: browserPerMinute, rate: browserPerMinute, last: t0},
		now:     now,
	}
}

// allow: errori del server.
func (t *throttle) allow(key string) bool { return t.check(key, &t.server) }

// allowBrowser: errori arrivati da /errori.
func (t *throttle) allowBrowser(key string) bool { return t.check(key, &t.browser) }

func (t *throttle) check(key string, b *bucket) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if at, ok := t.seen[key]; ok && now.Sub(at) < dedupWindow {
		return false
	}
	if !b.take(now) {
		return false
	}
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
