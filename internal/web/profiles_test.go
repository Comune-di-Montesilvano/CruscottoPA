package web

import (
	"errors"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
)

func TestProfileCache(t *testing.T) {
	now := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	c := newProfileCache(func() time.Time { return now })
	calls := 0
	ok := func() (audience.Profile, error) { calls++; return audience.Profile{Username: "mrossi"}, nil }
	fail := func() (audience.Profile, error) { calls++; return audience.Profile{}, errors.New("AD giù") }

	if p, found, _ := c.get("mrossi", ok); !found || p.Username != "mrossi" || calls != 1 {
		t.Fatal("primo caricamento")
	}
	now = now.Add(14 * time.Minute)
	c.get("mrossi", ok)
	if calls != 1 {
		t.Fatal("entro 15 minuti non si ricarica")
	}
	now = now.Add(2 * time.Minute)
	c.get("mrossi", ok)
	if calls != 2 {
		t.Fatal("dopo 15 minuti si ricarica")
	}

	if _, found, down := c.get("giu", fail); found || !down || calls != 3 {
		t.Fatal("errore: nessun profilo")
	}
	now = now.Add(30 * time.Second)
	c.get("giu", fail)
	if calls != 3 {
		t.Fatal("cache negativa: entro 1 minuto non si riprova")
	}
	now = now.Add(31 * time.Second)
	c.get("giu", fail)
	if calls != 4 {
		t.Fatal("dopo 1 minuto si riprova")
	}
}

// Un profilo caricato mentre gli attributi cambiano (reset) non va tenuto:
// conterrebbe la vecchia lista di attributi per 15 minuti.
func TestProfileCacheResetDuringLoad(t *testing.T) {
	c := newProfileCache(time.Now)
	calls := 0
	c.get("mrossi", func() (audience.Profile, error) {
		calls++
		c.reset() // gli attributi cambiano durante il caricamento
		return audience.Profile{Username: "mrossi"}, nil
	})
	c.get("mrossi", func() (audience.Profile, error) { calls++; return audience.Profile{Username: "mrossi"}, nil })
	if calls != 2 {
		t.Fatalf("il profilo caricato durante il reset è stato tenuto in cache (%d caricamenti)", calls)
	}
}
