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
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Scegli un attributo configurato") {
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
