package identity

import (
	"errors"
	"testing"
	"time"
)

func TestValuesCache(t *testing.T) {
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	c := newValuesCache(func() time.Time { return now })
	calls := 0
	ok := func() ([]string, error) { calls++; return []string{"A"}, nil }
	fail := func() ([]string, error) { calls++; return nil, errors.New("AD giù") }

	if v, err := c.get("x", ok); err != nil || len(v) != 1 || calls != 1 {
		t.Fatal("primo caricamento")
	}
	now = now.Add(5 * time.Hour)
	c.get("x", ok)
	if calls != 1 {
		t.Fatal("entro 6 ore non si ricarica")
	}
	now = now.Add(2 * time.Hour)
	if v, err := c.get("x", fail); err != nil || len(v) != 1 || calls != 2 {
		t.Fatal("scaduto e AD giù: ultimo elenco valido")
	}
	now = now.Add(30 * time.Second)
	c.get("x", fail)
	if calls != 2 {
		t.Fatal("dopo un errore non si riprova prima di 1 minuto")
	}

	if _, err := c.get("y", fail); err == nil || calls != 3 {
		t.Fatal("senza elenco precedente: errore")
	}
	if _, err := c.get("y", fail); err == nil || calls != 3 {
		t.Fatal("errore ricordato per 1 minuto")
	}
}
