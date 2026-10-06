package auth

import (
	"testing"
	"time"
)

func newTestLimiter() (*RateLimiter, *time.Time) {
	clock := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	r := NewRateLimiter(5, 15*time.Minute)
	r.now = func() time.Time { return clock }
	return r, &clock
}

func TestRateLimiterBlocksWithBackoff(t *testing.T) {
	r, clock := newTestLimiter()
	for i := 0; i < 4; i++ {
		r.Fail("u:mrossi")
	}
	if _, ok := r.Allow("u:mrossi"); !ok {
		t.Fatal("4 fallimenti: ancora consentito")
	}
	r.Fail("u:mrossi")
	if wait, ok := r.Allow("u:mrossi"); ok || wait != 30*time.Second {
		t.Fatalf("5° fallimento: atteso blocco 30s, ottenuto %v %v", wait, ok)
	}

	*clock = clock.Add(31 * time.Second)
	if _, ok := r.Allow("u:mrossi"); !ok {
		t.Fatal("dopo 30s: deve essere di nuovo consentito")
	}
	for i := 0; i < 5; i++ {
		r.Fail("u:mrossi")
	}
	if wait, _ := r.Allow("u:mrossi"); wait != 60*time.Second {
		t.Fatalf("secondo blocco: attesi 60s, ottenuti %v", wait)
	}
}

func TestRateLimiterWindowAndSuccess(t *testing.T) {
	r, clock := newTestLimiter()
	for i := 0; i < 4; i++ {
		r.Fail("ip:10.0.0.1")
	}
	*clock = clock.Add(16 * time.Minute) // fuori finestra
	r.Fail("ip:10.0.0.1")
	if _, ok := r.Allow("ip:10.0.0.1"); !ok {
		t.Fatal("i fallimenti fuori dalla finestra non devono contare")
	}

	for i := 0; i < 4; i++ {
		r.Fail("u:x")
	}
	r.Success("u:x")
	r.Fail("u:x")
	if _, ok := r.Allow("u:x"); !ok {
		t.Fatal("Success deve azzerare il contatore")
	}
}

func TestRateLimiterMultipleKeys(t *testing.T) {
	r, _ := newTestLimiter()
	for i := 0; i < 5; i++ {
		r.Fail("ip:10.0.0.9")
	}
	if _, ok := r.Allow("u:nuovo", "ip:10.0.0.9"); ok {
		t.Fatal("basta una chiave bloccata per bloccare la richiesta")
	}
}

func TestRateLimiterMaxBlock(t *testing.T) {
	r, clock := newTestLimiter()
	var wait time.Duration
	for round := 0; round < 12; round++ {
		for i := 0; i < 5; i++ {
			r.Fail("u:x")
		}
		wait, _ = r.Allow("u:x")
		*clock = clock.Add(wait + time.Second)
	}
	if wait != 15*time.Minute {
		t.Fatalf("blocco massimo: attesi 15m, ottenuti %v", wait)
	}
}
