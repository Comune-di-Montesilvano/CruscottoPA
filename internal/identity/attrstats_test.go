package identity

import (
	"testing"

	"github.com/go-ldap/ldap/v3"
)

func TestAttrStats(t *testing.T) {
	entries := []*ldap.Entry{
		ldap.NewEntry("CN=a", map[string][]string{"physicalDeliveryOfficeName": {"TRIBUTI"}, "department": {"Ragioneria"}, "objectGUID": {"\x01\x02"}, "whenCreated": {"20200101"}, "sAMAccountName": {"a"}}),
		ldap.NewEntry("CN=b", map[string][]string{"physicalDeliveryOfficeName": {"LLPP"}, "objectGUID": {"\x03"}}),
		ldap.NewEntry("CN=c", map[string][]string{"physicalDeliveryOfficeName": {"TRIBUTI"}}),
	}
	got := attrStats(entries, "")
	if len(got) != 2 || got[0].Name != "physicalDeliveryOfficeName" || got[0].Count != 3 || got[1].Name != "department" {
		t.Fatalf("attrStats: %+v", got)
	}
	if ex := got[0].Examples; len(ex) != 2 || ex[0] != "TRIBUTI" {
		t.Fatalf("esempi (distinti, il più frequente per primo): %v", ex)
	}
	if f := attrStats(entries, "DEPART"); len(f) != 1 || f[0].Name != "department" {
		t.Fatalf("filtro per nome: %+v", f)
	}
}

func TestDefaultAttrLabel(t *testing.T) {
	if DefaultAttrLabel("physicalDeliveryOfficeName") != "Ufficio" || DefaultAttrLabel("extensionAttribute3") != "extensionAttribute3" {
		t.Fatal("etichette predefinite")
	}
}
