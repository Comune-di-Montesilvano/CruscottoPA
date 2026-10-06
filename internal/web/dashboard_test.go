package web

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/calendar"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func id64(v int64) *int64 { return &v }

func TestDashboardFreshInstall(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(t, s, "GET", "/", nil, nil, nil)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Nessun applicativo configurato") {
		t.Fatalf("installazione nuova: %d\n%s", rec.Code, body)
	}
	if strings.Contains(body, "data-carousel") || strings.Contains(body, `class="widget guides"`) {
		t.Fatal("senza avvisi né guide generali: niente carosello né widget guide")
	}
	for _, want := range []string{
		"Buongiorno", "martedì 6 ottobre 2026", "vtest",
		"Settimana 41 · giorno 279 di 365", // widget Oggi
		"Ottobre 2026",                     // widget Calendario
		"Ognissanti",                       // prossimi: festività calcolate
	} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
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
	alert := database.Alert{Title: "Phishing via PEC", Body: "Dettagli su https://cert.local", Level: "urgent", Source: "CED",
		StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow}
	alertID, _ := db.CreateAlert(alert)
	alert.ID = alertID

	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	for _, want := range []string{
		`href="https://rubrica.local"`, `href="https://mail.local"`,
		`style="background:#dee8fc"`, // tinta di #2563eb (Rubrica)
		`data-flyout-toggle`, "1 guida", "Cercare un interno",
		`class="widget guides"`, "VPN da casa",
		"data-carousel", `class="news news-urgent"`, "CED",
		`<a href="https://cert.local" target="_blank" rel="noopener">`,
		`data-urgent="` + itoa(alertID) + `" data-version="` + alertVersion(alert) + `"`,
		`<span class="material-icons" aria-hidden="true">contacts</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	if n := strings.Count(body, "data-flyout-toggle"); n != 1 {
		t.Errorf("badge guide solo sulle app con guide: trovati %d", n)
	}
}

func TestCarouselControlsOnlyWithMoreAlerts(t *testing.T) {
	s, db := newTestServer(t, nil)
	db.CreateAlert(database.Alert{Title: "Uno", Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	if body := do(t, s, "GET", "/", nil, nil, nil).Body.String(); strings.Contains(body, "data-prev") || !strings.Contains(body, `href="/avvisi"`) {
		t.Fatal("un solo avviso: niente frecce, ma link a tutti gli avvisi")
	}
	db.CreateAlert(database.Alert{Title: "Due", Level: "maintenance", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	if body := do(t, s, "GET", "/", nil, nil, nil).Body.String(); !strings.Contains(body, "data-prev") || !strings.Contains(body, "data-pause") {
		t.Fatal("più avvisi: frecce e pausa")
	}
}

func TestDashboardUnsafeURLNeutralized(t *testing.T) {
	s, db := newTestServer(t, nil)
	apps, _ := db.ListApps()
	a := apps[0]
	a.URL = "javascript:alert(1)"
	db.UpdateApp(a)
	if body := do(t, s, "GET", "/", nil, nil, nil).Body.String(); strings.Contains(body, "javascript:alert") {
		t.Fatal("URL javascript: deve essere neutralizzato da html/template")
	}
}

func TestAlertsPartial(t *testing.T) {
	s, db := newTestServer(t, nil)
	db.CreateAlert(database.Alert{Title: "Manutenzione Sicraweb", Level: "maintenance", StartsAt: fixedNow.Add(-time.Minute), CreatedAt: fixedNow})
	rec := do(t, s, "GET", "/partials/alerts", nil, nil, nil)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Manutenzione Sicraweb") || !strings.Contains(body, "data-carousel") || strings.Contains(body, "<html") {
		t.Fatalf("partial avvisi: %d\n%s", rec.Code, body)
	}
}

func TestCalendarPartial(t *testing.T) { // Review Focus #2
	s, _ := newTestServer(t, nil)
	rec := do(t, s, "GET", "/partials/calendario?mese=2026-11", nil, nil, nil)
	if body := rec.Body.String(); rec.Code != 200 || !strings.Contains(body, "Novembre 2026") || !strings.Contains(body, `popovertarget="giorno-20261101"`) || strings.Contains(body, "<html") {
		t.Fatalf("novembre: %d\n%s", rec.Code, body)
	}
	for _, bad := range []string{"2026-13", "abc", "0001-01", ""} {
		rec := do(t, s, "GET", "/partials/calendario?mese="+bad, nil, nil, nil)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Ottobre 2026") {
			t.Errorf("mese=%q: atteso mese corrente, ottenuto %d", bad, rec.Code)
		}
	}
}

func TestCalendarWidgetShowsEvents(t *testing.T) {
	s, db := newTestServer(t, nil)
	db.CreateCalendarEvent(database.CalendarEvent{Title: "Formazione PEC", Kind: "event", StartsOn: calendar.Date(2026, 10, 8), EndsOn: calendar.Date(2026, 10, 8), Description: "Sala consiliare"})
	db.CreateCalendarEvent(database.CalendarEvent{Title: "Uffici chiusi (ponte)", Kind: "closure", StartsOn: calendar.Date(2026, 10, 31), EndsOn: calendar.Date(2026, 10, 31)})
	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	for _, want := range []string{
		`ev"><button type="button" class="cal-day" popovertarget="giorno-20261008"`,
		`closed"><button type="button" class="cal-day" popovertarget="giorno-20261031"`,
		"Sala consiliare", "gio 8 ott", "sab 31 ott",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
}

func TestAvvisiPage(t *testing.T) { // Review Focus #5
	s, db := newTestServer(t, nil)
	if body := do(t, s, "GET", "/avvisi", nil, nil, nil).Body.String(); !strings.Contains(body, "Nessun avviso attivo.") {
		t.Fatal("pagina avvisi vuota")
	}
	long := strings.Repeat("Testo molto lungo dell'avviso. ", 80)
	db.CreateAlert(database.Alert{Title: "Lungo", Body: long, Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	if body := do(t, s, "GET", "/avvisi", nil, nil, nil).Body.String(); !strings.Contains(body, "Lungo") || !strings.Contains(body, "Testo molto lungo") {
		t.Fatal("la pagina avvisi mostra il testo completo")
	}
	css, err := os.ReadFile("../../web/static/css/plancia.css")
	if err != nil || !strings.Contains(string(css), "-webkit-line-clamp: 8") {
		t.Fatal("nel carosello i testi lunghi vanno troncati a 8 righe")
	}
}

func TestTodayInfoAndGreeting(t *testing.T) {
	for h, want := range map[int]string{0: "Buonasera", 6: "Buongiorno", 12: "Buongiorno", 13: "Buon pomeriggio", 17: "Buon pomeriggio", 18: "Buonasera"} {
		if got := greeting(h); got != want {
			t.Errorf("greeting(%d) = %q, atteso %q", h, got, want)
		}
	}
	if got := italianDate(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)); got != "domenica 1 marzo 2026" {
		t.Errorf("italianDate: %q", got)
	}
	ti := todayInfo(time.Date(2028, 12, 31, 10, 0, 0, 0, time.UTC))
	if ti.Day != 31 || ti.Weekday != "domenica" || ti.YearDay != 366 || ti.YearDays != 366 || ti.Week != 52 {
		t.Fatalf("todayInfo 31/12/2028 (bisestile): %+v", ti)
	}
}
