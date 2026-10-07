package identity

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-ldap/ldap/v3"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
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

func TestPersonFromEntry(t *testing.T) {
	e := ldap.NewEntry("CN=x", map[string][]string{"sAMAccountName": {"MRossi"}, "displayName": {" Mario Rossi "}, "givenName": {" Mario "}})
	if p := personFromEntry(e); p != (Person{Username: "mrossi", Name: "Mario Rossi", GivenName: "Mario"}) {
		t.Fatalf("personFromEntry = %+v", p)
	}
}
func TestValidAttrName(t *testing.T) {
	for _, ok := range []string{"physicalDeliveryOfficeName", "department", "extensionAttribute1", "x-y"} {
		if !ValidAttrName(ok) {
			t.Errorf("%q rifiutato", ok)
		}
	}
	for _, bad := range []string{"", "1abc", "a b", "cn=*", "a)(b", strings.Repeat("a", 65)} {
		if ValidAttrName(bad) {
			t.Errorf("%q accettato", bad)
		}
	}
}

func TestSearchFilterEscapes(t *testing.T) {
	f := searchFilter(`a*)(cn=\`, "cn")
	if strings.Contains(f, "a*)(cn=") || !strings.Contains(f, `a\2a\29\28cn=\5c`) {
		t.Fatalf("query non escapata: %q", f)
	}
}

func TestMembersFilter(t *testing.T) {
	if _, ok := membersFilter([]audience.Rule{{Kind: audience.KindExclude, Value: "x"}}); ok {
		t.Fatal("senza regole positive non deve esserci un filtro")
	}
	f, ok := membersFilter([]audience.Rule{
		{Kind: audience.KindAttr, Attr: "physicalDeliveryOfficeName", Value: "INFO*"},
		{Kind: audience.KindADGroup, Value: "CN=G,DC=x"},
		{Kind: audience.KindUser, Value: "mrossi"},
		{Kind: audience.KindExclude, Value: "stagista1"},
	})
	for _, want := range []string{
		`(physicalDeliveryOfficeName=INFO\2a)`,
		`(memberOf:1.2.840.113556.1.4.1941:=CN=G,DC=x)`,
		`(sAMAccountName=mrossi)`,
		`(!(sAMAccountName=stagista1))`,
	} {
		if !ok || !strings.Contains(f, want) {
			t.Errorf("manca %q in %q", want, f)
		}
	}
}

func TestMockDirectoryProfile(t *testing.T) {
	p, err := MockDirectory{}.Profile("MRossi", []string{"physicalDeliveryOfficeName"})
	if err != nil || p.Username != "mrossi" || len(p.Attrs["physicaldeliveryofficename"]) == 0 || len(p.Groups) == 0 {
		t.Fatalf("Profile: %+v %v", p, err)
	}
}
