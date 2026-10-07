package auth

import (
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

func TestMockAuthentication(t *testing.T) {
	open := NewLDAP(config.LDAP{Host: "mock"})
	if ok, admin, err := open.Authenticate("chiunque", "x"); !ok || !admin || err != nil {
		t.Fatalf("mock senza ADMIN_USERS: tutti admin, ottenuto %v %v %v", ok, admin, err)
	}

	restricted := NewLDAP(config.LDAP{Host: "mock", AdminUsers: []string{"mrossi"}})
	if ok, admin, _ := restricted.Authenticate("MRossi", "x"); !ok || !admin {
		t.Fatal("ADMIN_USERS deve ignorare maiuscole/minuscole")
	}
	if ok, admin, _ := restricted.Authenticate("gbianchi", "x"); !ok || admin {
		t.Fatal("utente non in ADMIN_USERS: autenticato ma non admin")
	}
}

func TestRejectsInvalidInput(t *testing.T) {
	a := NewLDAP(config.LDAP{Host: "mock"})
	for _, u := range []string{"", "  ", "mario rossi", "admin)(uid=*", "cn=x,dc=y", "ü"} {
		if ok, _, _ := a.Authenticate(u, "pw"); ok {
			t.Errorf("username %q dovrebbe essere rifiutato", u)
		}
	}
	if ok, _, _ := a.Authenticate("mrossi", ""); ok {
		t.Error("password vuota dovrebbe essere rifiutata")
	}
}

func TestInGroup(t *testing.T) {
	memberOf := []string{
		"CN=CED Admin,OU=Gruppi,DC=comune,DC=local",
		"CN=Tutti,OU=Gruppi,DC=comune,DC=local",
	}
	if !inGroup(memberOf, "ced admin") {
		t.Error("CN case-insensitive non riconosciuto")
	}
	if !inGroup(memberOf, "CN=Tutti,OU=Gruppi,DC=comune,DC=local") {
		t.Error("DN completo non riconosciuto")
	}
	if inGroup(memberOf, "Gruppi") || inGroup(memberOf, "CED") {
		t.Error("match parziale su OU o prefisso del CN non ammesso")
	}
}

func TestLDAPHostname(t *testing.T) {
	for in, want := range map[string]string{
		"ldaps://dc01.comune.local:636": "dc01.comune.local",
		"ldap://10.0.0.5":               "10.0.0.5",
	} {
		if got := ldapHostname(in); got != want {
			t.Errorf("ldapHostname(%q) = %q, atteso %q", in, got, want)
		}
	}
}

// Un domain controller che non risponde (pacchetti persi, non rifiutati) non
// deve bloccare la plancia per il timeout predefinito di go-ldap (60 s).
func TestDialTimesOut(t *testing.T) {
	start := time.Now()
	_, err := Dial(config.LDAP{Host: "ldap://10.255.255.1:389"}) // indirizzo non instradabile
	if err == nil {
		t.Skip("10.255.255.1 raggiungibile in questa rete: test non significativo")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("Dial ha impiegato %v: serve un timeout di connessione breve", elapsed)
	}
}

// ADMIN_USERS: "mrossi" e "mrossi@dominio" sono lo stesso utente.
func TestIsAdminUserIgnoresDomain(t *testing.T) {
	if !IsAdminUser([]string{"mrossi"}, "MRossi@intranet.local") || !IsAdminUser([]string{"mrossi@intranet.local"}, "mrossi") {
		t.Fatal("username con e senza dominio devono coincidere")
	}
	if IsAdminUser([]string{"mrossi"}, "mrossini") {
		t.Fatal("confronto per prefisso non ammesso")
	}
}
