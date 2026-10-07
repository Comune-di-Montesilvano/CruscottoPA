package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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
	maxPushSubscriptions = 1
	defer func() { maxPushSubscriptions = 20000 }()
	other := `{"endpoint":"https://fcm.googleapis.com/fcm/send/z","keys":{"p256dh":"BAAA","auth":"AAAA"}}`
	if json.Unmarshal(postJSON(t, s, "/push/iscrizioni", other, nil).Body.Bytes(), &res); res["ok"] {
		t.Fatal("oltre il tetto delle iscrizioni: nuova iscrizione accettata")
	}
	if json.Unmarshal(postJSON(t, s, "/push/iscrizioni", body, c).Body.Bytes(), &res); !res["ok"] {
		t.Fatal("al tetto: il rinnovo di un'iscrizione esistente va accettato")
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
	for _, want := range []string{`<script src="/static/js/notifiche.js" defer></script>`, `<dialog class="notify-ask"`, `<dialog class="notify-ask notify-blocked"`, `<meta name="theme-color" content="#1565c0">`} {
		if !strings.Contains(body, want) {
			t.Errorf("plancia: manca %q", want)
		}
	}
	// Nessun interruttore per l'utente: le notifiche si richiedono col popup.
	if strings.Contains(body, "data-notifiche") {
		t.Error("il link Notifiche nel footer non deve esserci")
	}
}

type okPusher struct{ n int }

func (p *okPusher) Send(context.Context, database.PushSubscription, []byte, bool) (bool, error) {
	p.n++
	return false, nil
}

// L'admin che entra con l'UPN (mrossi@dominio) è lo stesso utente che la
// plancia riconosce via NTLM come mrossi.
func TestPushTestFindsAdminByUPN(t *testing.T) {
	s, db := newTestServerWith(t, nil, func(o *Options) { o.Config.VAPIDSubject = "mailto:supporto@example.it" })
	p := &okPusher{}
	s.pusher = p
	db.SavePushSubscription(database.PushSubscription{Endpoint: "https://fcm.googleapis.com/fcm/send/a", P256dh: "k", Auth: "a", Username: "mrossi", CreatedAt: fixedNow}, 10)
	rec := do(t, s, "POST", "/admin/login", url.Values{"username": {"MRossi@comune.local"}, "password": {"pw"}}, nil, nil)
	c := sessionCookie(rec)
	if body := do(t, s, "POST", "/admin/notifiche/prova", nil, c, hx).Body.String(); !strings.Contains(body, "Iscrizioni: 1 · inviate: 1") || p.n != 1 {
		t.Fatalf("prova con UPN: %s", body)
	}
}

// Modificare un avviso già notificato non rinvia la notifica: il form lo dice.
func TestAlertFormSaysAlreadyNotified(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	id, _ := db.CreateAlert(database.Alert{Title: "Sciopero", Level: database.LevelUrgent, Notify: true, StartsAt: fixedNow})
	db.MarkNotified(id, fixedNow)
	page := do(t, s, "GET", fmt.Sprintf("/admin/avvisi/%d/modifica", id), nil, c, hx).Body.String()
	if !strings.Contains(page, "Notifica già inviata il 06/10/2026 10:00") {
		t.Fatal("manca l'avviso di notifica già inviata")
	}
}

// Review I3: il clic su una notifica non deve portare via una scheda
// dell'admin (lavoro non salvato) e, se navigate() fallisce, apre una finestra.
func TestServiceWorkerClickSparesAdmin(t *testing.T) {
	sw, err := os.ReadFile("../../web/static/sw.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(sw)
	if !strings.Contains(src, `"/admin"`) || !strings.Contains(src, ".catch(") {
		t.Fatal("sw.js: notificationclick deve saltare le schede /admin e ripiegare su openWindow")
	}
}
