package web

import (
	"net/url"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestAlertVisibilitySaved(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	ced, _ := db.CreateAudienceGroup("CED")
	page := do(t, s, "GET", "/admin/avvisi", nil, c, nil).Body.String()
	if !strings.Contains(page, `name="visibilita"`) || !strings.Contains(page, "CED") || !strings.Contains(page, "non una protezione") {
		t.Fatal("fieldset Visibilità mancante")
	}
	form := url.Values{"title": {"Solo CED"}, "level": {"news"}, "starts_at": {"2026-10-06T09:00"}, "visibilita": {"only"}, "gruppi": {itoa(ced)}}
	rec := do(t, s, "POST", "/admin/avvisi", form, c, map[string]string{"HX-Request": "true"})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Riservato: CED") {
		t.Fatalf("salvataggio: %d\n%s", rec.Code, rec.Body)
	}
	all, _ := db.ListActiveAlerts(fixedNow)
	if ca, _ := db.GetContentAudience(database.ContentAlert, all[0].ID); ca.Mode != audience.ModeOnly || len(ca.Groups) != 1 {
		t.Fatalf("visibilità salvata: %+v", ca)
	}
}

func TestVisibilityNeedsGroup(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	form := url.Values{"title": {"x"}, "level": {"news"}, "starts_at": {"2026-10-06T09:00"}, "visibilita": {"hide"}}
	if rec := do(t, s, "POST", "/admin/avvisi", form, c, map[string]string{"HX-Request": "true"}); !invalid(rec) || !strings.Contains(rec.Body.String(), "Scegli almeno un gruppo") {
		t.Fatalf("Nascosto a senza gruppi: %d", rec.Code)
	}
}

func TestAppVisibilityEditAndList(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	g, _ := db.CreateAudienceGroup("Polizia Locale")
	apps, _ := db.ListApps()
	id := apps[0].ID
	db.SetContentAudience(database.ContentApp, id, database.ContentAudience{Mode: audience.ModeHide, Groups: []int64{g}})
	edit := do(t, s, "GET", "/admin/app/"+itoa(id)+"/modifica", nil, c, map[string]string{"HX-Request": "true"}).Body.String()
	if !strings.Contains(edit, `value="hide" checked`) {
		t.Fatal("la modalità salvata deve risultare selezionata")
	}
	if list := do(t, s, "GET", "/admin/app", nil, c, nil).Body.String(); !strings.Contains(list, "Nascosto a: Polizia Locale") {
		t.Fatal("etichetta negli elenchi mancante")
	}
	rec := postMultipart(t, s, "/admin/app/"+itoa(id), appFields(db, map[string]string{"visibilita": ""}), nil, c)
	if ca, _ := db.GetContentAudience(database.ContentApp, id); rec.Code != 200 || ca.Mode != audience.ModePublic {
		t.Fatalf("ritorno a pubblico: %d %+v", rec.Code, ca)
	}
}
