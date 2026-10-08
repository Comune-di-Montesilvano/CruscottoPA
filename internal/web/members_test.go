package web

import (
	"errors"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

func TestMembersCache(t *testing.T) {
	now := time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC)
	c := newMembersCache(func() time.Time { return now })
	calls := 0
	load := func() (int, []identity.Person, error) { calls++; return 3, nil, nil }
	tributi := []audience.Rule{{Kind: audience.KindUser, Value: "a"}}

	c.get(tributi, nil, load)
	c.get(tributi, nil, load)
	if calls != 1 {
		t.Fatalf("stesse regole entro 30 s: %d chiamate, attesa 1", calls)
	}
	c.get(append(tributi, audience.Rule{Kind: audience.KindPresent, Attr: "mail"}), nil, load)
	c.get(tributi, []string{"mail"}, load)
	if calls != 3 {
		t.Fatalf("regole o colonne diverse devono ricaricare: %d chiamate", calls)
	}
	now = now.Add(membersTTL)
	c.get(tributi, nil, load)
	if calls != 4 {
		t.Fatalf("scaduto: %d chiamate, attese 4", calls)
	}
	if len(c.m) != 1 {
		t.Fatalf("voci scadute non tolte: %d", len(c.m))
	}

	boom := errors.New("AD giù")
	if _, _, err := c.get(nil, nil, func() (int, []identity.Person, error) { return 0, nil, boom }); err != boom {
		t.Fatalf("errore: %v", err)
	}
	if n, _, err := c.get(nil, nil, load); err != nil || n != 3 {
		t.Fatalf("un errore non va tenuto in cache: %d %v", n, err)
	}
}
