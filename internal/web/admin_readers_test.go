package web

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestAlertReadersPage(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	id, _ := db.CreateAlert(database.Alert{Title: "Sciopero", Body: strings.Repeat("parola ", 60), Level: database.LevelNews, StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	db.TouchPresence("mrossi", "Mario Rossi", fixedNow)
	db.TouchPresence("senzanome", "", fixedNow)
	db.MarkRead(id, "mrossi", database.ReadConfirm, fixedNow)
	db.RecordDelivery(id, "mrossi", "https://fcm.googleapis.com/a", "Chrome/Edge", database.DeliverySent, fixedNow)
	db.MarkReceived(id, "https://fcm.googleapis.com/a", fixedNow)

	list := do(t, s, "GET", "/admin/avvisi", nil, c, nil).Body.String()
	if !strings.Contains(list, "Letto da 1") || !strings.Contains(list, "ricevute 1/1") || !strings.Contains(list, `href="/admin/avvisi/`+itoa(id)+`/letture"`) {
		t.Fatalf("lista avvisi:\n%s", list)
	}
	page := do(t, s, "GET", "/admin/avvisi/"+itoa(id)+"/letture", nil, c, nil).Body.String()
	for _, want := range []string{"Sciopero", "Mario Rossi", "conferma", "Chrome/Edge", "Non ancora letto da", "senzanome", "non una prova"} {
		if !strings.Contains(page, want) {
			t.Errorf("letture: manca %q", want)
		}
	}
	if rec := do(t, s, "GET", "/admin/avvisi/999/letture", nil, c, nil); rec.Code != 404 {
		t.Fatalf("avviso inesistente: %d", rec.Code)
	}
}

func TestUsersPanel(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	db.TouchPresence("mrossi", "Mario Rossi", fixedNow)
	db.SetPresenceClient("mrossi", true, "granted", fixedNow)
	db.TouchPresence("anna", "Anna Verdi", fixedNow.Add(-20*24*time.Hour))
	db.SetPresenceClient("anna", false, "granted", fixedNow.Add(-20*24*time.Hour)) // concesso ma senza iscrizione
	db.TouchPresence("luca", "Luca Neri", fixedNow)
	db.SetPresenceClient("luca", false, "denied", fixedNow)
	db.SavePushSubscription(database.PushSubscription{Endpoint: "https://fcm.googleapis.com/m", P256dh: "k", Auth: "a", Username: "mrossi", CreatedAt: fixedNow}, 100)

	page := do(t, s, "GET", "/admin/utenti", nil, c, nil).Body.String()
	for _, want := range []string{"Mario Rossi", ">attivo<", "Chrome/Edge", "Anna Verdi", ">inattivo<", "senza iscrizione", "Luca Neri", "bloccate", "informativa"} {
		if !strings.Contains(page, want) {
			t.Errorf("utenti: manca %q", want)
		}
	}
	if only := do(t, s, "GET", "/admin/utenti?attivi=1", nil, c, nil).Body.String(); strings.Contains(only, "Anna Verdi") {
		t.Error("filtro attivi")
	}
	if q := do(t, s, "GET", "/admin/utenti?q=luca", nil, c, nil).Body.String(); strings.Contains(q, "Mario Rossi") || !strings.Contains(q, "Luca Neri") {
		t.Error("ricerca")
	}
	if body := do(t, s, "GET", "/admin", nil, c, nil).Body.String(); !strings.Contains(body, `href="/admin/utenti"`) {
		t.Error("voce Utenti nel menu")
	}
}

// Avviso pubblico: «non ancora letto da» senza interrogare AD.
func TestReadersPublicAlertNoAD(t *testing.T) {
	calls := 0
	dir := testDirectory
	dir.calls = &calls
	s, db := newTestServerWith(t, nil, func(o *Options) { o.Directory = dir })
	c := login(t, s)
	long := strings.Repeat("parola ", 60)
	id, _ := db.CreateAlert(database.Alert{Title: "Lungo", Body: long, Level: database.LevelNews, StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	db.TouchPresence("mrossi", "Mario Rossi", fixedNow)
	db.TouchPresence("senzanome", "", fixedNow)
	page := do(t, s, "GET", "/admin/avvisi/"+itoa(id)+"/letture", nil, c, nil).Body.String()
	if !strings.Contains(page, "Mario Rossi") || !strings.Contains(page, "senzanome") || calls != 0 {
		t.Fatalf("pubblico: chiamate AD %d\n%s", calls, page)
	}
}

// AD giù su un avviso riservato: ci si ferma al primo errore e lo si dice.
func TestReadersADDownStops(t *testing.T) {
	calls := 0
	dir := fakeDirectory{err: errors.New("giù"), calls: &calls}
	s, db := newTestServerWith(t, nil, func(o *Options) { o.Directory = dir })
	c := login(t, s)
	g, _ := db.CreateAudienceGroup("G")
	id, _ := db.CreateAlertWithAudience(database.Alert{Title: "Riservato", Body: strings.Repeat("parola ", 60), Level: database.LevelNews, StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow},
		database.ContentAudience{Mode: "only", Groups: []int64{g}})
	for _, u := range []string{"a1", "a2", "a3"} {
		db.TouchPresence(u, "", fixedNow)
	}
	page := do(t, s, "GET", "/admin/avvisi/"+itoa(id)+"/letture", nil, c, nil).Body.String()
	if !strings.Contains(page, "Elenco non disponibile: AD non raggiungibile") || calls != 1 {
		t.Fatalf("AD giù: chiamate %d\n%s", calls, page)
	}
}

// Avviso breve non urgente: si legge tutto nel carosello, la lettura non è
// rilevabile e non si elenca chi «non l'ha letto».
func TestReadersShortAlertNotTrackable(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	id, _ := db.CreateAlert(database.Alert{Title: "Breve", Body: "Solo testo.", Level: database.LevelNews, StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	db.TouchPresence("mrossi", "Mario Rossi", fixedNow)
	page := do(t, s, "GET", "/admin/avvisi/"+itoa(id)+"/letture", nil, c, nil).Body.String()
	if !strings.Contains(page, "lettura non rilevabile") || strings.Contains(page, "Non ancora letto da") {
		t.Fatalf("avviso breve:\n%s", page)
	}
	if list := do(t, s, "GET", "/admin/avvisi", nil, c, nil).Body.String(); !strings.Contains(list, "lettura non rilevabile") {
		t.Fatal("lista: avviso breve senza indicazione")
	}
}
