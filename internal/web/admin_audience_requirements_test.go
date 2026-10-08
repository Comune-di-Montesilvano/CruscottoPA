package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestRequirementRulesAdmin(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	db.CreateAudienceAttribute("mail", "Email")
	g, _ := db.CreateAudienceGroup("G")
	base := "/admin/gruppi/" + itoa(g)

	for _, rule := range []url.Values{
		{"kind": {"attr"}, "attr": {"physicalDeliveryOfficeName"}, "value": {"TRIBUTI"}},
		{"kind": {"present"}, "attr": {"MAIL"}},
		{"kind": {"exclude"}, "value": {"stagista1"}},
	} {
		if rec := do(t, s, "POST", base+"/regole", rule, c, hx); rec.Code != http.StatusOK {
			t.Fatalf("regola %v: %d\n%s", rule, rec.Code, rec.Body)
		}
	}
	rec := do(t, s, "POST", base+"/regole", url.Values{"kind": {"absent"}, "attr": {"department"}}, c, hx)
	if !invalid(rec) || !strings.Contains(rec.Body.String(), "Scegli un attributo configurato") {
		t.Fatalf("requisito su attributo non configurato: %d", rec.Code)
	}

	if prev := do(t, s, "POST", base+"/anteprima", nil, c, hx).Body.String(); !strings.Contains(prev, "<th>Ufficio</th>") || !strings.Contains(prev, "<th>Email</th>") {
		t.Errorf("colonne dell'anteprima: attributi delle regole e dei requisiti\n%s", prev)
	}
	page := do(t, s, "GET", base+"/modifica", nil, c, hx).Body.String()
	for _, want := range []string{
		"Chi entra", "Requisiti", "Esclusi",
		"Ufficio = TRIBUTI", "Email presente", "stagista1",
		// riepilogo in una frase
		"Ufficio = TRIBUTI</strong>, purché abbia <strong>Email</strong>; escluso <strong>stagista1</strong>",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("manca %q\n%s", want, page)
		}
	}
}

func TestRequirementDraftPreviewNeedsNoValue(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	db.CreateAudienceAttribute("mail", "Email")
	g, _ := db.CreateAudienceGroup("G")
	db.AddAudienceRule(ruleUser(g, "mrossi"))
	body := do(t, s, "GET", "/admin/gruppi/"+itoa(g)+"/bozza?kind=present&attr=mail", nil, c, hx).Body.String()
	if strings.Contains(body, "Scrivi un valore") || strings.Contains(body, "Questa regola") || !strings.Contains(body, "Il gruppo resterebbe a 1 utente") {
		t.Fatalf("bozza di un requisito:\n%s", body)
	}
}

func TestGroupListMembers(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	g, _ := db.CreateAudienceGroup("CED")
	vuoto, _ := db.CreateAudienceGroup("Vuoto")
	db.AddAudienceRule(ruleUser(g, "mrossi"))

	page := do(t, s, "GET", "/admin/gruppi", nil, c, nil).Body.String()
	for _, want := range []string{"<th>Membri</th>", `hx-get="/admin/gruppi/` + itoa(g) + `/membri"`, `hx-get="/admin/gruppi/` + itoa(g) + `/anteprima"`} {
		if !strings.Contains(page, want) {
			t.Errorf("elenco gruppi: manca %q", want)
		}
	}
	if body := do(t, s, "GET", "/admin/gruppi/"+itoa(g)+"/membri", nil, c, hx).Body.String(); strings.TrimSpace(body) != "1" {
		t.Fatalf("conteggio: %q", body)
	}
	if body := do(t, s, "GET", "/admin/gruppi/"+itoa(vuoto)+"/membri", nil, c, hx).Body.String(); strings.TrimSpace(body) != "0" {
		t.Fatalf("conteggio gruppo vuoto: %q", body)
	}
	if body := do(t, s, "GET", "/admin/gruppi/"+itoa(g)+"/anteprima", nil, c, hx).Body.String(); !strings.Contains(body, "Mario Rossi") {
		t.Fatalf("elenco membri:\n%s", body)
	}
	if rec := do(t, s, "GET", "/admin/gruppi/999/membri", nil, c, hx); rec.Code != http.StatusNotFound {
		t.Fatalf("gruppo inesistente: %d", rec.Code)
	}
}

func ruleUser(g int64, u string) database.AudienceRule {
	return database.AudienceRule{GroupID: g, Kind: audience.KindUser, Value: u}
}

func TestAttributeHeroAdmin(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	tel, _ := db.CreateAudienceAttribute("telephoneNumber", "Interno")
	off, _ := db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	if rec := do(t, s, "POST", "/admin/gruppi/attributi/"+itoa(off)+"/testata", url.Values{"hero_kind": {"text"}}, c, hx); rec.Code != 200 {
		t.Fatalf("testata: %d", rec.Code)
	}
	do(t, s, "POST", "/admin/gruppi/attributi/"+itoa(tel)+"/testata", url.Values{"hero_kind": {"phone"}}, c, hx)
	if rec := do(t, s, "POST", "/admin/gruppi/attributi/"+itoa(tel)+"/testata", url.Values{"hero_kind": {"boh"}}, c, hx); !invalid(rec) {
		t.Fatalf("formato non valido: %d", rec.Code)
	}
	do(t, s, "POST", "/admin/gruppi/attributi/"+itoa(tel)+"/sposta", url.Values{"dir": {"up"}}, c, hx)
	h, _ := db.HeroAttributes()
	if len(h) != 2 || h[0].ID != tel || h[0].HeroKind != "phone" {
		t.Fatalf("dopo lo spostamento: %+v", h)
	}
	page := do(t, s, "GET", "/admin/gruppi", nil, c, nil).Body.String()
	if !strings.Contains(page, `<option value="phone" selected>`) || !strings.Contains(page, "Sotto il saluto") {
		t.Fatalf("pagina gruppi:\n%s", page)
	}
	if rec := do(t, s, "POST", "/admin/gruppi/attributi/999/testata", url.Values{"hero_kind": {"text"}}, c, hx); rec.Code != http.StatusNotFound {
		t.Fatalf("inesistente: %d", rec.Code)
	}
}

// Nell'admin gli attributi «Sotto il saluto» compaiono per primi e nell'ordine
// della testata: le frecce si vedono funzionare.
func TestAttributeHeroAdminOrder(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	alfa, _ := db.CreateAudienceAttribute("description", "Alfa")
	beta, _ := db.CreateAudienceAttribute("mail", "Beta")
	db.CreateAudienceAttribute("department", "Aaa (non in testata)")
	db.SetAttributeHero(alfa, database.HeroText)
	db.SetAttributeHero(beta, database.HeroMail)
	db.MoveAttributeHero(beta, -1)
	page := do(t, s, "GET", "/admin/gruppi", nil, c, nil).Body.String()
	b, a, other := strings.Index(page, "<td>Beta</td>"), strings.Index(page, "<td>Alfa</td>"), strings.Index(page, "<td>Aaa (non in testata)</td>")
	if b < 0 || a < 0 || other < 0 || !(b < a && a < other) {
		t.Fatalf("ordine nell'admin: Beta=%d Alfa=%d altro=%d", b, a, other)
	}
}
