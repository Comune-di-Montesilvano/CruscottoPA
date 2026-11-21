package errtrack

import "testing"

func TestScrub(t *testing.T) {
	cases := []struct{ in, want string }{
		{"mario.rossi@example.it", "<mail>"},
		{"dial 192.0.2.15:389", "dial <ip>:389"},
		{"host 2001:db8::1 down", "host <ip> down"},
		{`MOCK\mrossi`, "<utente>"},
		{"CN=Mario Rossi,OU=Uffici,DC=example,DC=it: invalid", "<dn>: invalid"},
		{`LDAP Result Code 49 "Invalid Credentials": cn=x,dc=it`, `LDAP Result Code 49 "Invalid Credentials": <dn>`},
		{"ticket: registro", "ticket: registro"},
		{"template plancia: line 3", "template plancia: line 3"},
		{"alle 10:00:00 ok", "alle 10:00:00 ok"},
		{"versione 0.19.0", "versione 0.19.0"},
		{"Mozilla/5.0 Chrome/131.0.0.0 Safari/537.36 Edg/131.0.2903.86", "Mozilla/5.0 Chrome/131.0.0.0 Safari/537.36 Edg/131.0.2903.86"},
		{"GET http://192.0.2.15/x", "GET http://<ip>/x"},
		{"chiamare 085 1234567", "chiamare <tel>"},
		{"cell. +39 333 1234567.", "cell. <tel>."},
		{"tel 085-4481111", "tel <tel>"},
		{"interno 4321, riga 12345", "interno 4321, riga 12345"},
		{"il 2026-11-21 10:00:00", "il 2026-11-21 10:00:00"},
	}
	for _, c := range cases {
		if got := Scrub(c.in); got != c.want {
			t.Errorf("Scrub(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/ticket/123", "/ticket/{id}"},
		{"/ticket/123/allegati/4/foto.png", "/ticket/{id}/allegati/{id}/{file}"},
		{"/uploads/icons/a1b2c3d4e5.png", "/uploads/icons/{file}"},
		{"/guide/7/pdf", "/guide/{id}/pdf"},
		{"/admin/media/9f8e7d6c5b4a/pezzo?n=2", "/admin/media/{id}/pezzo"},
		{"/", "/"},
		{"/avvisi", "/avvisi"},
		{"/avvisi#x", "/avvisi"},
		{"", "/"},
		{"/ticket/12/allegati/3/Mario_Rossi", "/ticket/{id}/allegati/{id}/{file}"},
		{"/uploads/guide/abc", "/uploads/guide/{file}"},
	}
	for _, c := range cases {
		if got := NormalizePath(c.in); got != c.want {
			t.Errorf("NormalizePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPersonalKey(t *testing.T) {
	for _, k := range []string{"user", "username", "utente", "mail", "email", "pc", "ip", "name", "nome", "remote", "User", "gruppo.mail", "ticket"} {
		if !PersonalKey(k) {
			t.Errorf("PersonalKey(%q) = false", k)
		}
	}
	for _, k := range []string{"err", "da", "stato"} {
		if PersonalKey(k) {
			t.Errorf("PersonalKey(%q) = true", k)
		}
	}
}
