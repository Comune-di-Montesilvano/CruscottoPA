package web

import (
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func id64(v int64) *int64 { return &v }

func TestDashboardFreshInstall(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(t, s, "GET", "/", nil, nil, nil)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Nessun applicativo configurato") {
		t.Fatalf("installazione nuova: atteso messaggio vuoto, ottenuto %d\n%s", rec.Code, body)
	}
	if strings.Contains(body, `class="guides"`) || strings.Contains(body, `class="ticker"`) {
		t.Fatal("colonna guide e striscia avvisi non devono esserci se vuote")
	}
	if !strings.Contains(body, "Buongiorno") || !strings.Contains(body, "martedì 6 ottobre 2026") {
		t.Fatal("saluto/data iniziali mancanti (10:00 Europe/Rome)")
	}
	if !strings.Contains(body, "vtest") {
		t.Fatal("versione nel footer mancante")
	}
}

func TestDashboardTilesGuidesAlerts(t *testing.T) {
	s, db := newTestServer(t, nil)
	apps, _ := db.ListApps()
	rubrica, webmail := apps[0], apps[1]
	rubrica.URL, webmail.URL = "https://rubrica.local", "https://mail.local"
	db.UpdateApp(rubrica)
	db.UpdateApp(webmail)
	db.CreateGuide(database.Guide{AppID: id64(rubrica.ID), Title: "Cercare un interno", Kind: "link", URL: "https://wiki/interno", Enabled: true})
	db.CreateGuide(database.Guide{Title: "VPN da casa", Kind: "link", URL: "https://wiki/vpn", Enabled: true})
	db.CreateAlert(database.Alert{Title: "Phishing via PEC", Body: "Dettagli su https://cert.local", Level: "urgent",
		StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})

	rec := do(t, s, "GET", "/", nil, nil, nil)
	body := rec.Body.String()

	for _, want := range []string{
		`href="https://rubrica.local"`, `href="https://mail.local"`,
		`popovertarget="guides-` + itoa(rubrica.ID) + `"`, "Cercare un interno",
		`class="guides"`, "VPN da casa",
		`class="pill pill-urgent"`, "Phishing via PEC",
		`<a href="https://cert.local" target="_blank" rel="noopener">`,
		`<span class="material-icons" aria-hidden="true">contacts</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	if strings.Contains(body, `popovertarget="guides-`+itoa(webmail.ID)+`"`) {
		t.Error("Webmail non ha guide: niente segmento")
	}
}

func TestDashboardUnsafeURLNeutralized(t *testing.T) {
	s, db := newTestServer(t, nil)
	apps, _ := db.ListApps()
	a := apps[0]
	a.URL = "javascript:alert(1)" // scritto direttamente in DB, aggirando la validazione admin
	db.UpdateApp(a)
	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	if strings.Contains(body, "javascript:alert") {
		t.Fatal("URL javascript: deve essere neutralizzato da html/template")
	}
}

func TestAlertsPartial(t *testing.T) {
	s, db := newTestServer(t, nil)
	db.CreateAlert(database.Alert{Title: "Manutenzione Sicraweb", Level: "maintenance", StartsAt: fixedNow.Add(-time.Minute), CreatedAt: fixedNow})
	rec := do(t, s, "GET", "/partials/alerts", nil, nil, nil)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Manutenzione Sicraweb") || strings.Contains(body, "<html") {
		t.Fatalf("partial avvisi: %d\n%s", rec.Code, body)
	}
}

func TestGreetingAndDate(t *testing.T) {
	for h, want := range map[int]string{0: "Buonasera", 6: "Buongiorno", 12: "Buongiorno", 13: "Buon pomeriggio", 17: "Buon pomeriggio", 18: "Buonasera"} {
		if got := greeting(h); got != want {
			t.Errorf("greeting(%d) = %q, atteso %q", h, got, want)
		}
	}
	if got := italianDate(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)); got != "domenica 1 marzo 2026" {
		t.Errorf("italianDate: %q", got)
	}
}
