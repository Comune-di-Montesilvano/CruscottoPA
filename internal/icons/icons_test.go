package icons

import (
	"slices"
	"testing"
)

func TestCatalog(t *testing.T) {
	if Count() < 2000 {
		t.Fatalf("catalogo troppo piccolo: %d", Count())
	}
	for _, n := range []string{"mail", "contacts", "account_balance", "gavel", "payments"} {
		if !Valid(n) {
			t.Errorf("%q dovrebbe essere valida", n)
		}
	}
	for _, n := range []string{"", "MAIL", "mail ", "<script>", "non_esiste_xyz"} {
		if Valid(n) {
			t.Errorf("%q non dovrebbe essere valida", n)
		}
	}
}

func TestSearch(t *testing.T) {
	got := Search("mail", 10)
	if len(got) == 0 || got[0] != "mail" {
		t.Fatalf("Search(mail): il match esatto/prefisso va per primo, ottenuto %v", got)
	}
	if !slices.Contains(Search("account bal", 10), "account_balance") {
		t.Fatal("Search: gli spazi devono valere come underscore")
	}
	if !slices.Contains(Search("balance", 60), "account_balance") {
		t.Fatal("Search: deve trovare anche le sottostringhe")
	}
	if n := len(Search("", 7)); n != 7 {
		t.Fatalf("Search vuota: attesi 7 risultati, ottenuti %d", n)
	}
	if n := len(Search("a", 60)); n != 60 {
		t.Fatalf("limite non rispettato: %d", n)
	}
}
