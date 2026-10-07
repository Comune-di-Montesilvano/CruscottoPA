package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

func postJSON(t *testing.T, s *Server, path, body string, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestPushKeyDisabledWithoutSubject(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if rec := do(t, s, "GET", "/push/chiave", nil, nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("push spento: atteso 404, ottenuto %d", rec.Code)
	}
}

func TestPushSubscribeAndRemove(t *testing.T) {
	s, db := newTestServerWith(t, nil, func(o *Options) { o.Config.VAPIDSubject = "mailto:supporto@example.it" })
	if rec := do(t, s, "GET", "/push/chiave", nil, nil, nil); rec.Code != 200 || len(strings.TrimSpace(rec.Body.String())) < 40 {
		t.Fatalf("chiave pubblica: %d %q", rec.Code, rec.Body)
	}
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	body := `{"endpoint":"https://fcm.googleapis.com/fcm/send/x","keys":{"p256dh":"BAAA","auth":"AAAA"}}`
	rec := postJSON(t, s, "/push/iscrizioni", body, c)
	var res map[string]bool
	json.Unmarshal(rec.Body.Bytes(), &res)
	if rec.Code != 200 || !res["ok"] {
		t.Fatalf("iscrizione: %d %s", rec.Code, rec.Body)
	}
	if mine, _ := db.ListPushSubscriptionsFor("mrossi"); len(mine) != 1 {
		t.Fatalf("iscrizione non associata all'utente: %+v", mine)
	}
	for _, bad := range []string{
		`{"endpoint":"http://fcm.googleapis.com/fcm/send/x","keys":{"p256dh":"BAAA","auth":"AAAA"}}`,
		`{"endpoint":"https://fcm.googleapis.com/fcm/send/` + strings.Repeat("a", 1100) + `","keys":{"p256dh":"BAAA","auth":"AAAA"}}`,
		`{"endpoint":"https://fcm.googleapis.com/fcm/send/y","keys":{"p256dh":"","auth":"AAAA"}}`,
		`non json`,
		`{"endpoint":"https://10.0.0.5/x","keys":{"p256dh":"BAAA","auth":"AAAA"}}`,
	} {
		rec := postJSON(t, s, "/push/iscrizioni", bad, nil)
		json.Unmarshal(rec.Body.Bytes(), &res)
		if rec.Code != 200 || res["ok"] {
			t.Errorf("iscrizione non valida accettata: %s", bad)
		}
	}
	postJSON(t, s, "/push/iscrizioni/rimuovi", `{"endpoint":"https://fcm.googleapis.com/fcm/send/x"}`, nil)
	if all, _ := db.ListPushSubscriptions(); len(all) != 0 {
		t.Fatalf("rimozione: %+v", all)
	}
}

func TestServiceWorkerAndManifest(t *testing.T) {
	s, _ := newTestServer(t, nil)
	sw := do(t, s, "GET", "/sw.js", nil, nil, nil)
	if sw.Code != 200 || !strings.Contains(sw.Header().Get("Content-Type"), "javascript") || sw.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("sw.js: %d %v", sw.Code, sw.Header())
	}
	m := do(t, s, "GET", "/manifest.webmanifest", nil, nil, nil)
	if m.Code != 200 || m.Header().Get("Content-Type") != "application/manifest+json" || !strings.Contains(m.Body.String(), `"start_url"`) {
		t.Fatalf("manifest: %d %v", m.Code, m.Header())
	}
	if body := do(t, s, "GET", "/", nil, nil, nil).Body.String(); !strings.Contains(body, `<link rel="manifest" href="/manifest.webmanifest">`) {
		t.Fatal("plancia senza link al manifest")
	}
}

func TestAlertFormNotifyCheckbox(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	page := do(t, s, "GET", "/admin/avvisi", nil, c, nil).Body.String()
	if !strings.Contains(page, `name="notify" value="1"`) {
		t.Fatal("casella Invia notifica mancante")
	}
	form := map[string][]string{"title": {"Sciopero"}, "level": {"urgent"}, "starts_at": {"2026-10-06T09:00"}, "notify": {"1"}}
	rec := do(t, s, "POST", "/admin/avvisi", form, c, hx)
	if rec.Code != 200 {
		t.Fatalf("salvataggio: %d", rec.Code)
	}
	all, _ := db.ListActiveAlerts(fixedNow)
	if !all[0].Notify {
		t.Fatal("notify non salvato")
	}
}

func TestPushTestAndNotifiedLabel(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	if body := do(t, s, "POST", "/admin/notifiche/prova", nil, c, hx).Body.String(); !strings.Contains(body, "Web Push spento") {
		t.Fatalf("push spento: %s", body)
	}
	id, _ := db.CreateAlert(database.Alert{Title: "Sciopero", Level: database.LevelUrgent, Notify: true, StartsAt: fixedNow})
	db.MarkNotified(id, fixedNow)
	if page := do(t, s, "GET", "/admin/avvisi", nil, c, nil).Body.String(); !strings.Contains(page, "notificato il 06/10/2026 10:00") {
		t.Fatal("etichetta notificato il mancante")
	}

	s, _ = newTestServerWith(t, nil, func(o *Options) { o.Config.VAPIDSubject = "mailto:supporto@example.it" })
	c = login(t, s)
	if body := do(t, s, "POST", "/admin/notifiche/prova", nil, c, hx).Body.String(); !strings.Contains(body, "Nessuna iscrizione") {
		t.Fatalf("nessuna iscrizione: %s", body)
	}
}

func TestDashboardNotifyMarkup(t *testing.T) {
	s, _ := newTestServer(t, nil)
	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	for _, want := range []string{`<script src="/static/js/notifiche.js" defer></script>`, `<dialog class="notify-ask"`, `data-notifiche`, `<meta name="theme-color" content="#1565c0">`} {
		if !strings.Contains(body, want) {
			t.Errorf("plancia: manca %q", want)
		}
	}
}
