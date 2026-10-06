package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func alertValues(overrides map[string]string) url.Values {
	v := url.Values{
		"title": {"Manutenzione Sicraweb"}, "body": {"Dalle 13 alle 15.\nSalvare il lavoro."},
		"level": {"maintenance"}, "starts_at": {"2026-10-06T09:00"}, "ends_at": {"2026-10-09T15:00"},
	}
	for k, val := range overrides {
		v.Set(k, val)
	}
	return v
}

func TestCreateAlertShowsInDashboard(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)

	page := do(t, s, "GET", "/admin/avvisi", nil, c, nil).Body.String()
	if !strings.Contains(page, `value="2026-10-06T10:00"`) {
		t.Fatal("nuovo avviso: inizio precompilato con l'ora corrente in Europe/Rome")
	}

	rec := do(t, s, "POST", "/admin/avvisi", alertValues(nil), c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Manutenzione Sicraweb") || !strings.Contains(rec.Body.String(), "Attivo") {
		t.Fatalf("crea: %d\n%s", rec.Code, rec.Body)
	}
	current, _, _ := db.ListAlertsForAdmin(fixedNow)
	if len(current) != 1 || current[0].CreatedBy != "mrossi" || current[0].Body != "Dalle 13 alle 15.\nSalvare il lavoro." {
		t.Fatalf("DB: %+v", current)
	}
	if !current[0].StartsAt.Equal(time.Date(2026, 10, 6, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("09:00 a Roma (CEST) = 07:00Z, ottenuto %v", current[0].StartsAt)
	}
	if dash := do(t, s, "GET", "/", nil, nil, nil).Body.String(); !strings.Contains(dash, "pill-maintenance") {
		t.Fatal("l'avviso attivo deve comparire in plancia")
	}
}

func TestAlertValidation(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	for name, tc := range map[string]struct {
		overrides map[string]string
		msg       string
	}{
		"titolo vuoto":       {map[string]string{"title": " "}, "Campo obbligatorio."},
		"livello":            {map[string]string{"level": "critico"}, "Livello non valido."},
		"inizio non data":    {map[string]string{"starts_at": "domani"}, "Data e ora non valide."},
		"fine prima":         {map[string]string{"ends_at": "2026-10-06T08:00"}, "La fine deve essere successiva all&#39;inizio."},
		"fine uguale":        {map[string]string{"ends_at": "2026-10-06T09:00"}, "La fine deve essere successiva all&#39;inizio."},
		"testo troppo lungo": {map[string]string{"body": strings.Repeat("a", 2001)}, "Massimo 2000 caratteri."},
	} {
		t.Run(name, func(t *testing.T) {
			rec := do(t, s, "POST", "/admin/avvisi", alertValues(tc.overrides), c, hx)
			if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), tc.msg) {
				t.Fatalf("atteso 422 con %q, ottenuto %d\n%s", tc.msg, rec.Code, rec.Body)
			}
		})
	}
}

func TestAlertDSTRoundTrip(t *testing.T) { // Review Focus #3
	s, db := newTestServer(t, nil)
	c := login(t, s)
	do(t, s, "POST", "/admin/avvisi", alertValues(map[string]string{"starts_at": "2026-03-29T10:00", "ends_at": ""}), c, hx)

	_, expired, _ := db.ListAlertsForAdmin(fixedNow)
	current, _, _ := db.ListAlertsForAdmin(fixedNow)
	all := append(current, expired...)
	if len(all) != 1 || !all[0].StartsAt.Equal(time.Date(2026, 3, 29, 8, 0, 0, 0, time.UTC)) || all[0].EndsAt != nil {
		t.Fatalf("29/03 10:00 Roma (già CEST) = 08:00Z senza fine, ottenuto %+v", all)
	}
	rec := do(t, s, "GET", "/admin/avvisi/"+itoa(all[0].ID)+"/modifica", nil, c, hx)
	if !strings.Contains(rec.Body.String(), `value="2026-03-29T10:00"`) {
		t.Fatalf("il form deve mostrare di nuovo 10:00\n%s", rec.Body)
	}
}

func TestExpiredAlertReactivation(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	end := fixedNow.Add(-time.Hour)
	id, _ := db.CreateAlert(database.Alert{Title: "Vecchio avviso", Level: "news",
		StartsAt: fixedNow.Add(-48 * time.Hour), EndsAt: &end, CreatedAt: fixedNow, CreatedBy: "gbianchi"})

	page := do(t, s, "GET", "/admin/avvisi", nil, c, nil).Body.String()
	if !strings.Contains(page, "Scaduti") || !strings.Contains(page, "Vecchio avviso") {
		t.Fatal("gli avvisi scaduti vanno elencati a parte")
	}
	rec := do(t, s, "POST", "/admin/avvisi/"+itoa(id), alertValues(map[string]string{"title": "Vecchio avviso", "level": "news", "starts_at": "2026-10-04T10:00", "ends_at": ""}), c, hx)
	if rec.Code != 200 {
		t.Fatalf("riattivazione: %d\n%s", rec.Code, rec.Body)
	}
	a, _ := db.GetAlert(id)
	if a.EndsAt != nil || a.CreatedBy != "gbianchi" {
		t.Fatalf("riattivato senza fine, autore invariato: %+v", a)
	}
	if rec := do(t, s, "POST", "/admin/avvisi/"+itoa(id)+"/elimina", nil, c, hx); rec.Code != 200 {
		t.Fatalf("elimina: %d", rec.Code)
	}
	if rec := do(t, s, "POST", "/admin/avvisi/"+itoa(id)+"/elimina", nil, c, hx); rec.Code != http.StatusNotFound {
		t.Fatalf("elimina due volte: %d", rec.Code)
	}
}

func TestAlertSourceField(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	rec := do(t, s, "POST", "/admin/avvisi", alertValues(map[string]string{"source": "Ufficio Stipendi"}), c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Ufficio Stipendi") {
		t.Fatalf("fonte: %d\n%s", rec.Code, rec.Body)
	}
	current, _, _ := db.ListAlertsForAdmin(fixedNow)
	if len(current) != 1 || current[0].Source != "Ufficio Stipendi" {
		t.Fatalf("DB: %+v", current)
	}
	rec = do(t, s, "POST", "/admin/avvisi", alertValues(map[string]string{"source": strings.Repeat("x", 61)}), c, hx)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Massimo 60 caratteri.") {
		t.Fatalf("fonte troppo lunga: %d", rec.Code)
	}
}
