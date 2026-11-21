package errtrack

import (
	"fmt"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time        { return c.t }
func (c *fakeClock) add(d time.Duration)   { c.t = c.t.Add(d) }
func newClock() *fakeClock                 { return &fakeClock{t: time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)} }
func newThrottleAt(c *fakeClock) *throttle { return newThrottle(c.now) }

func TestThrottleSameKey(t *testing.T) {
	c := newClock()
	th := newThrottleAt(c)
	if !th.allow("fp|u1") {
		t.Fatal("primo invio negato")
	}
	if th.allow("fp|u1") {
		t.Fatal("ripetizione immediata ammessa")
	}
	c.add(10*time.Minute + time.Second)
	if !th.allow("fp|u1") {
		t.Fatal("dopo 10 minuti negato")
	}
}

func TestThrottleDifferentUsers(t *testing.T) {
	th := newThrottleAt(newClock())
	if !th.allow("fp|u1") || !th.allow("fp|u2") {
		t.Fatal("utenti diversi devono passare entrambi")
	}
}

func TestThrottleGlobalCap(t *testing.T) {
	c := newClock()
	th := newThrottleAt(c)
	for i := 0; i < 60; i++ {
		if !th.allow(fmt.Sprint("k", i)) {
			t.Fatalf("chiave %d negata sotto il tetto", i)
		}
	}
	if th.allow("k60") {
		t.Fatal("61ª chiave nello stesso minuto ammessa")
	}
	c.add(time.Minute)
	if !th.allow("k61") {
		t.Fatal("dopo un minuto negata")
	}
}

func TestThrottleBrowserCannotStarveServer(t *testing.T) {
	th := newThrottleAt(newClock())
	for i := 0; i < 100; i++ {
		th.allowBrowser(fmt.Sprint("b", i))
	}
	if !th.allow("server") {
		t.Fatal("errori del browser hanno esaurito il tetto del server")
	}
	if th.allowBrowser("b-oltre") {
		t.Fatal("tetto del browser non applicato")
	}
}

func TestThrottleBounded(t *testing.T) {
	c := newClock()
	th := newThrottleAt(c)
	for i := 0; i < 6000; i++ {
		th.allow(fmt.Sprint("k", i))
		c.add(time.Second) // tutte ancora "fresche" (< 10 min) per le ultime 600
	}
	if n := len(th.seen); n > maxKeys {
		t.Fatalf("chiavi in memoria = %d, max %d", n, maxKeys)
	}
}
