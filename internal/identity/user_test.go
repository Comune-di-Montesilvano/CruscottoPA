package identity

import (
	"strings"
	"testing"
	"time"
)

func TestCookieRoundTrip(t *testing.T) {
	c := NewCookieCodec(strings.Repeat("s", 32))
	u := User{Username: "mrossi", Name: "Mario Rossi"}
	v, err := c.Encode(u)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := c.Decode(v); !ok || got != u {
		t.Fatalf("Decode = %+v, %v", got, ok)
	}
	if _, ok := c.Decode(v[:len(v)-2] + "xx"); ok {
		t.Fatal("cookie manomesso accettato")
	}
	if _, ok := NewCookieCodec(strings.Repeat("t", 32)).Decode(v); ok {
		t.Fatal("cookie firmato con un altro segreto accettato")
	}
	if _, ok := c.Decode(""); ok {
		t.Fatal("cookie vuoto accettato")
	}
}

func TestFirstName(t *testing.T) {
	for in, want := range map[string]string{"Mirko D'Addiego": "Mirko", "  Anna  Maria Bianchi": "Anna", "": ""} {
		if got := (User{Name: in}).FirstName(); got != want {
			t.Errorf("FirstName(%q) = %q, atteso %q", in, got, want)
		}
	}
}

// Il nome di battesimo viene da givenName quando c'è: displayName può essere
// "Cognome Nome" o contenere un nome composto.
func TestFirstNamePrefersGivenName(t *testing.T) {
	if got := (User{GivenName: "Anna Maria", Name: "Bianchi Anna Maria"}).FirstName(); got != "Anna Maria" {
		t.Fatalf("FirstName = %q", got)
	}
}

// Il cookie anonimo vale 24 ore anche lato server, non solo nel browser.
func TestAnonymousCookieExpiresServerSide(t *testing.T) {
	c := NewCookieCodec(strings.Repeat("s", 32))
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	v, _ := c.Encode(User{Anonymous: true})
	now = now.Add(25 * time.Hour)
	if _, ok := c.Decode(v); ok {
		t.Fatal("cookie anonimo accettato dopo 24 ore")
	}
	u, _ := c.Encode(User{Username: "mrossi", Name: "Mario"})
	now = now.Add(29 * 24 * time.Hour)
	if _, ok := c.Decode(u); !ok {
		t.Fatal("cookie riconosciuto valido entro 30 giorni")
	}
}
