package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)


func TestAudienceAttributesAdmin(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	rec := do(t, s, "POST", "/admin/gruppi/attributi", url.Values{"name": {"physicalDeliveryOfficeName"}, "label": {"Ufficio"}}, c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Ufficio") {
		t.Fatalf("aggiungi attributo: %d\n%s", rec.Code, rec.Body)
	}
	if rec := do(t, s, "POST", "/admin/gruppi/attributi", url.Values{"name": {"a)(b"}, "label": {"X"}}, c, hx); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Nome di attributo LDAP non valido") {
		t.Fatalf("nome non valido: %d", rec.Code)
	}
	attrs, _ := db.ListAudienceAttributes()
	if len(attrs) != 1 {
		t.Fatalf("attributi: %v", attrs)
	}
}

func TestAudienceGroupLifecycle(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	if rec := do(t, s, "POST", "/admin/gruppi", url.Values{"name": {"CED"}}, c, hx); rec.Code != 200 {
		t.Fatalf("nuovo gruppo: %d", rec.Code)
	}
	groups, _ := db.ListAudienceGroups()
	g := itoa(groups[0].ID)

	for _, rule := range []url.Values{
		{"kind": {"attr"}, "attr": {"physicalDeliveryOfficeName"}, "value": {"TRIBUTI"}},
		{"kind": {"adgroup"}, "value": {"CN=SHARE_TRIBUTI_RW,DC=test"}, "label": {"SHARE_TRIBUTI_RW"}},
		{"kind": {"user"}, "value": {"Mario.Rossi"}},
		{"kind": {"exclude"}, "value": {"stagista1"}},
	} {
		if rec := do(t, s, "POST", "/admin/gruppi/"+g+"/regole", rule, c, hx); rec.Code != 200 {
			t.Fatalf("regola %v: %d\n%s", rule, rec.Code, rec.Body)
		}
	}
	page := do(t, s, "GET", "/admin/gruppi/"+g+"/modifica", nil, c, hx).Body.String()
	for _, want := range []string{"Ufficio = TRIBUTI", "Gruppo AD SHARE_TRIBUTI_RW", "Utente mario.rossi", "Escludi stagista1"} {
		if !strings.Contains(page, want) {
			t.Errorf("manca %q", want)
		}
	}
	if rec := do(t, s, "POST", "/admin/gruppi/"+g+"/regole", url.Values{"kind": {"attr"}, "attr": {"department"}, "value": {"X"}}, c, hx); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("attributo non configurato accettato: %d", rec.Code)
	}
	prev := do(t, s, "POST", "/admin/gruppi/"+g+"/anteprima", nil, c, hx).Body.String()
	if !strings.Contains(prev, "1 utente") || !strings.Contains(prev, "Mario Rossi") {
		t.Fatalf("anteprima:\n%s", prev)
	}

	apps, _ := db.ListApps()
	db.SetContentAudience(database.ContentApp, apps[0].ID, database.ContentAudience{Mode: "only", Groups: []int64{groups[0].ID}})
	rec := do(t, s, "POST", "/admin/gruppi/"+g+"/elimina", nil, c, hx)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), apps[0].Title) {
		t.Fatalf("gruppo in uso eliminato: %d\n%s", rec.Code, rec.Body)
	}
}

func TestADSuggestions(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	if body := do(t, s, "GET", "/admin/ad/gruppi?q=TRIB", nil, c, hx).Body.String(); !strings.Contains(body, "SHARE_TRIBUTI_RW") {
		t.Fatalf("suggerimenti gruppi:\n%s", body)
	}
	if body := do(t, s, "GET", "/admin/ad/valori?attr=physicalDeliveryOfficeName&q=tri", nil, c, hx).Body.String(); !strings.Contains(body, "TRIBUTI") || strings.Contains(body, "LLPP") {
		t.Fatalf("suggerimenti valori (filtrati per q):\n%s", body)
	}
	if rec := do(t, s, "GET", "/admin/ad/utenti?q=x", nil, nil, hx); rec.Code != http.StatusUnauthorized {
		t.Fatalf("suggerimenti senza sessione: %d", rec.Code)
	}
}

func TestADUnavailableInAdmin(t *testing.T) {
	s, _ := newTestServerWith(t, nil, func(o *Options) { o.Directory = fakeDirectory{err: errors.New("giù")} })
	c := login(t, s)
	if body := do(t, s, "GET", "/admin/ad/gruppi?q=a", nil, c, hx).Body.String(); !strings.Contains(body, "AD non disponibile") {
		t.Fatalf("AD giù:\n%s", body)
	}
}
