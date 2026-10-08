package audience

import "testing"

var mario = Profile{
	Username: "mario.rossi",
	Attrs:    map[string][]string{"physicaldeliveryofficename": {"INFORMATIZZAZIONE"}, "mail": {"mario.rossi@example.it"}, "pager": {" "}},
	Groups:   []string{"CN=SHARE_PNRR_RW,OU=Gruppi,DC=intranet,DC=local"},
}

func TestMember(t *testing.T) {
	cases := []struct {
		name  string
		rules []Rule
		want  bool
	}{
		{"nessuna regola", nil, false},
		{"attributo", []Rule{{Kind: KindAttr, Attr: "physicalDeliveryOfficeName", Value: " informatizzazione "}}, true},
		{"attributo diverso", []Rule{{Kind: KindAttr, Attr: "physicalDeliveryOfficeName", Value: "TRIBUTI"}}, false},
		{"attributo assente nel profilo", []Rule{{Kind: KindAttr, Attr: "department", Value: "X"}}, false},
		{"gruppo AD, DN con maiuscole diverse", []Rule{{Kind: KindADGroup, Value: "cn=share_pnrr_rw,ou=gruppi,dc=intranet,dc=local"}}, true},
		{"utente", []Rule{{Kind: KindUser, Value: "Mario.Rossi"}}, true},
		{"oppure tra regole", []Rule{{Kind: KindUser, Value: "altro"}, {Kind: KindAttr, Attr: "physicalDeliveryOfficeName", Value: "INFORMATIZZAZIONE"}}, true},
		{"esclusione vince", []Rule{{Kind: KindAttr, Attr: "physicalDeliveryOfficeName", Value: "INFORMATIZZAZIONE"}, {Kind: KindExclude, Value: "mario.rossi"}}, false},
		{"solo esclusioni", []Rule{{Kind: KindExclude, Value: "altro"}}, false},
		{"requisito presente soddisfatto", []Rule{{Kind: KindUser, Value: "mario.rossi"}, {Kind: KindPresent, Attr: "Mail"}}, true},
		{"requisito presente mancante", []Rule{{Kind: KindUser, Value: "mario.rossi"}, {Kind: KindPresent, Attr: "telephoneNumber"}}, false},
		{"requisito presente con valore vuoto", []Rule{{Kind: KindUser, Value: "mario.rossi"}, {Kind: KindPresent, Attr: "pager"}}, false},
		{"requisito assente soddisfatto", []Rule{{Kind: KindUser, Value: "mario.rossi"}, {Kind: KindAbsent, Attr: "telephoneNumber"}}, true},
		{"requisito assente violato", []Rule{{Kind: KindUser, Value: "mario.rossi"}, {Kind: KindAbsent, Attr: "mail"}}, false},
		{"tutti i requisiti", []Rule{{Kind: KindUser, Value: "mario.rossi"}, {Kind: KindPresent, Attr: "mail"}, {Kind: KindPresent, Attr: "telephoneNumber"}}, false},
		{"solo requisiti", []Rule{{Kind: KindPresent, Attr: "mail"}}, false},
	}
	for _, c := range cases {
		if got := Member(mario, c.rules); got != c.want {
			t.Errorf("%s: Member = %v, atteso %v", c.name, got, c.want)
		}
	}
}

func TestVisible(t *testing.T) {
	in := map[int64]bool{1: true}
	cases := []struct {
		name   string
		mode   Mode
		groups []int64
		known  bool
		want   bool
	}{
		{"pubblico, anonimo", ModePublic, nil, false, true},
		{"riservato a un mio gruppo", ModeOnly, []int64{2, 1}, true, true},
		{"riservato ad altri", ModeOnly, []int64{2}, true, false},
		{"nascosto a un mio gruppo", ModeHide, []int64{1}, true, false},
		{"nascosto ad altri", ModeHide, []int64{2}, true, true},
		{"nascosto ad altri, anonimo", ModeHide, []int64{2}, false, false},
		{"riservato, anonimo", ModeOnly, []int64{1}, false, false},
	}
	for _, c := range cases {
		if got := Visible(c.mode, c.groups, in, c.known); got != c.want {
			t.Errorf("%s: Visible = %v, atteso %v", c.name, got, c.want)
		}
	}
}

func TestValidKindAndMode(t *testing.T) {
	if !ValidKind("attr") || !ValidKind("exclude") || !ValidKind("present") || !ValidKind("absent") || ValidKind("altro") {
		t.Fatal("ValidKind")
	}
	if !ValidMode("") || !ValidMode("only") || !ValidMode("hide") || ValidMode("x") {
		t.Fatal("ValidMode")
	}
}

// Anteprima (LDAP) e plancia devono dare lo stesso esito: DN confrontati come
// DN, attributi a più valori confrontati su ogni valore.
func TestMemberDNAndMultiValued(t *testing.T) {
	p := Profile{
		Username: "anna",
		Attrs:    map[string][]string{"physicaldeliveryofficename": {"AMMINISTRATIVO", "TRIBUTI"}},
		Groups:   []string{"CN=Uff Tributi,OU=Gruppi,DC=intranet,DC=local"},
	}
	if !Member(p, []Rule{{Kind: KindADGroup, Value: "cn=uff tributi, ou=gruppi, dc=intranet, dc=local"}}) {
		t.Error("DN con spazi e maiuscole diverse non riconosciuto")
	}
	if !Member(p, []Rule{{Kind: KindAttr, Attr: "physicalDeliveryOfficeName", Value: "tributi"}}) {
		t.Error("attributo a più valori: deve bastare uno dei valori")
	}
}
