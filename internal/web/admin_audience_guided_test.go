package web

import (
	"net/url"
	"strings"
	"testing"
)

func TestGroupEditorEmptyAttributes(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	g, _ := db.CreateAudienceGroup("G")
	body := do(t, s, "GET", "/admin/gruppi/"+itoa(g)+"/modifica", nil, c, hx).Body.String()
	if !strings.Contains(body, "Nessun attributo configurato") || !strings.Contains(body, `<option value="adgroup" selected>`) {
		t.Fatalf("senza attributi: messaggio e tipo Gruppo AD preselezionato attesi\n%s", body)
	}
}

func TestAttributeNameSuggestions(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	body := do(t, s, "GET", "/admin/ad/attributi?name=phys", nil, c, hx).Body.String()
	for _, want := range []string{`data-fill="physicalDeliveryOfficeName"`, `data-label="Ufficio"`, "218 utenti", "TRIBUTI"} {
		if !strings.Contains(body, want) {
			t.Errorf("suggerimenti attributi: manca %q\n%s", want, body)
		}
	}
}

func TestDraftPreviewDoesNotSave(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	g, _ := db.CreateAudienceGroup("G")
	q := url.Values{"kind": {"attr"}, "attr": {"physicalDeliveryOfficeName"}, "value": {"TRIBUTI"}}
	body := do(t, s, "GET", "/admin/gruppi/"+itoa(g)+"/bozza?"+q.Encode(), nil, c, hx).Body.String()
	if !strings.Contains(body, "Questa regola: 1 utente") || !strings.Contains(body, "Il gruppo passerebbe da 0 a 1 utente") {
		t.Fatalf("anteprima in bozza:\n%s", body)
	}
	if rules, _ := db.ListAudienceRules(g); len(rules) != 0 {
		t.Fatal("l'anteprima in bozza non deve salvare la regola")
	}
	empty := do(t, s, "GET", "/admin/gruppi/"+itoa(g)+"/bozza?kind=attr&attr=physicalDeliveryOfficeName&value=", nil, c, hx).Body.String()
	if !strings.Contains(empty, "Scrivi un valore") {
		t.Fatalf("bozza senza valore:\n%s", empty)
	}
}

func TestSavedPreviewShowsAttributeValues(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	g, _ := db.CreateAudienceGroup("G")
	do(t, s, "POST", "/admin/gruppi/"+itoa(g)+"/regole", url.Values{"kind": {"attr"}, "attr": {"physicalDeliveryOfficeName"}, "value": {"TRIBUTI"}}, c, hx)
	body := do(t, s, "POST", "/admin/gruppi/"+itoa(g)+"/anteprima", nil, c, hx).Body.String()
	if !strings.Contains(body, "<th>Ufficio</th>") || !strings.Contains(body, "<td>TRIBUTI</td>") {
		t.Fatalf("tabella dell'anteprima con i valori dell'attributo:\n%s", body)
	}
}

// L'anteprima in bozza sta dentro il form (hx-target="#section"): deve
// aggiornare solo sé stessa, non sostituire la sezione.
func TestDraftPreviewTargetsItself(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	g, _ := db.CreateAudienceGroup("G")
	body := do(t, s, "GET", "/admin/gruppi/"+itoa(g)+"/modifica", nil, c, hx).Body.String()
	if !strings.Contains(body, `class="draft-preview" aria-live="polite" hx-target="this"`) {
		t.Fatal("l'anteprima in bozza deve avere hx-target=\"this\"")
	}
}
