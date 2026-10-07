package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

const adminLink = `<a class="foot-link" href="/admin">Amministrazione</a>`

func TestFooterLinksRepository(t *testing.T) {
	s, _ := newTestServer(t, nil)
	for _, path := range []string{"/", "/avvisi"} {
		if body := do(t, s, "GET", path, nil, nil, nil).Body.String(); !strings.Contains(body, `href="https://github.com/Comune-di-Montesilvano/CruscottoPA"`) {
			t.Errorf("%s: link al repository mancante nel footer", path)
		}
	}
}

func TestFooterAdminLink(t *testing.T) {
	mario := identity.User{Username: "mrossi", Name: "Mario Rossi"}
	cases := []struct {
		name string
		edit func(*Options)
		user *identity.User
		want bool
	}{
		{"nel gruppo admin (DN del profilo)", func(o *Options) { o.Config.LDAP.AdminGroup = "share_tributi_rw" }, &mario, true},
		{"in ADMIN_USERS", func(o *Options) { o.Config.LDAP.AdminUsers = []string{"MRossi"} }, &mario, true},
		{"utente normale", func(o *Options) { o.Config.LDAP.AdminGroup = "ALTRO" }, &mario, false},
		{"anonimo", func(o *Options) { o.Config.LDAP.AdminGroup = "share_tributi_rw" }, nil, false},
	}
	for _, c := range cases {
		s, _ := newTestServerWith(t, nil, c.edit)
		var cookie *http.Cookie
		if c.user != nil {
			cookie = viewerCookie(t, s, *c.user)
		}
		for _, path := range []string{"/", "/avvisi"} {
			body := do(t, s, "GET", path, nil, cookie, nil).Body.String()
			if got := strings.Contains(body, adminLink); got != c.want {
				t.Errorf("%s %s: link admin = %v, atteso %v", c.name, path, got, c.want)
			}
		}
	}
}

// Il link è solo una comodità: l'identità è dichiarata, l'admin resta dietro il login LDAP.
func TestAdminLinkDoesNotBypassLogin(t *testing.T) {
	s, _ := newTestServerWith(t, nil, func(o *Options) { o.Config.LDAP.AdminGroup = "share_tributi_rw" })
	if rec := do(t, s, "GET", "/admin", nil, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"}), nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("/admin con il solo cookie utente: atteso 303, ottenuto %d", rec.Code)
	}
}
