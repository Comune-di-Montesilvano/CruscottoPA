package auth

import (
	"sync"
	"time"
)

const (
	baseBlock  = 30 * time.Second
	maxBlock   = 15 * time.Minute
	maxEntries = 10000
)

// RateLimiter blocca una chiave (es. "u:mrossi", "ip:10.0.0.1") dopo max
// fallimenti entro window, con attesa che raddoppia a ogni blocco (max 15 min).
// Stato solo in memoria: si azzera al riavvio.
type RateLimiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	now     func() time.Time
	entries map[string]*limitEntry
}

type limitEntry struct {
	failures     []time.Time
	strikes      int
	blockedUntil time.Time
}

func NewRateLimiter(max int, window time.Duration) *RateLimiter {
	return &RateLimiter{max: max, window: window, now: time.Now, entries: map[string]*limitEntry{}}
}

// Allow restituisce l'attesa più lunga tra le chiavi bloccate; ok=false se c'è.
func (r *RateLimiter) Allow(keys ...string) (time.Duration, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	var wait time.Duration
	for _, k := range keys {
		if e := r.entries[k]; e != nil && now.Before(e.blockedUntil) {
			if d := e.blockedUntil.Sub(now); d > wait {
				wait = d
			}
		}
	}
	return wait, wait == 0
}

func (r *RateLimiter) Fail(keys ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if len(r.entries) > maxEntries {
		r.prune(now)
	}
	for _, k := range keys {
		e := r.entries[k]
		if e == nil {
			e = &limitEntry{}
			r.entries[k] = e
		}
		recent := e.failures[:0]
		for _, f := range e.failures {
			if now.Sub(f) < r.window {
				recent = append(recent, f)
			}
		}
		e.failures = append(recent, now)
		if len(e.failures) >= r.max {
			if e.strikes < 10 {
				e.strikes++
			}
			block := baseBlock << (e.strikes - 1)
			if block > maxBlock {
				block = maxBlock
			}
			e.blockedUntil = now.Add(block)
			e.failures = nil
		}
	}
}

func (r *RateLimiter) Success(keys ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range keys {
		delete(r.entries, k)
	}
}

// prune elimina le chiavi non bloccate e senza fallimenti recenti.
func (r *RateLimiter) prune(now time.Time) {
	for k, e := range r.entries {
		if now.After(e.blockedUntil) && (len(e.failures) == 0 || now.Sub(e.failures[len(e.failures)-1]) > r.window) {
			delete(r.entries, k)
		}
	}
}
