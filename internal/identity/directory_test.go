package identity

import (
	"errors"
	"strings"
	"testing"
)

func TestUserFilter(t *testing.T) {
	f, err := userFilter("mirko.daddiego")
	if err != nil || !strings.Contains(f, "(sAMAccountName=mirko.daddiego)") || !strings.Contains(f, "userAccountControl:1.2.840.113556.1.4.803:=2") {
		t.Fatalf("filtro: %q %v", f, err)
	}
	for _, bad := range []string{"", "a*)(cn=*", "x y", strings.Repeat("a", 129)} {
		if _, err := userFilter(bad); !errors.Is(err, ErrUnknownUser) {
			t.Errorf("userFilter(%q): atteso ErrUnknownUser, ottenuto %v", bad, err)
		}
	}
}

func TestMockDirectory(t *testing.T) {
	var d Directory = MockDirectory{}
	p, err := d.Lookup("MRossi")
	if err != nil || p != (Person{Username: "mrossi", Name: "MRossi"}) {
		t.Fatalf("Lookup: %+v %v", p, err)
	}
	if _, err := d.Lookup("a b"); !errors.Is(err, ErrUnknownUser) {
		t.Fatal("username non valido accettato")
	}
}
