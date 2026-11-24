package web

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// chatDirectory: tre colleghi noti ad AD.
func chatDirectory() fakeDirectory {
	people := map[string]identity.Person{
		"mrossi":   {Username: "mrossi", Name: "Mario Rossi", Office: "Ragioneria"},
		"abianchi": {Username: "abianchi", Name: "Anna Bianchi", Office: "Ragioneria"},
		"lverdi":   {Username: "lverdi", Name: "Luca Verdi", Office: "Ragioneria"},
	}
	profiles := map[string]audience.Profile{}
	members := []identity.Person{}
	for u, p := range people {
		profiles[u] = audience.Profile{Username: u, Attrs: map[string][]string{"displayname": {p.Name}, "department": {p.Office}}}
		members = append(members, p)
	}
	return fakeDirectory{people: people, profiles: profiles, members: members}
}

func newChatServer(t *testing.T, edit func(*Options)) (*Server, *database.DB) {
	t.Helper()
	return newTestServerWith(t, nil, func(o *Options) {
		o.Config.ChatEnabled = true
		o.Directory = chatDirectory()
		if edit != nil {
			edit(o)
		}
	})
}

// userCookie: cookie dell'identità dichiarata (NTLM).
func userCookie(t *testing.T, s *Server, username, name string) *http.Cookie {
	t.Helper()
	v, err := s.cookies.Encode(identity.User{Username: username, Name: name, PC: "PC-PROVA-001"})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: identity.CookieName, Value: v}
}

// chatCookie: cookie della chat verificata.
func chatCookie(t *testing.T, s *Server, username string) *http.Cookie {
	t.Helper()
	v, err := s.chatCookies.Encode(identity.ChatSession{Username: username})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: identity.ChatCookieName, Value: v}
}

// withCookies: header Cookie con più cookie (do ne accetta uno solo).
func withCookies(extra map[string]string, cs ...*http.Cookie) map[string]string {
	h := map[string]string{}
	for k, v := range extra {
		h[k] = v
	}
	parts := []string{}
	for _, c := range cs {
		parts = append(parts, c.Name+"="+c.Value)
	}
	h["Cookie"] = strings.Join(parts, "; ")
	return h
}

// chatGroup: gruppo della plancia con dentro solo users.
func chatGroup(t *testing.T, db *database.DB, name string, users ...string) int64 {
	t.Helper()
	g, err := db.CreateAudienceGroup(name)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if _, err := db.AddAudienceRule(database.AudienceRule{GroupID: g, Kind: audience.KindUser, Value: u}); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

var errAD = errors.New("AD non raggiungibile")
