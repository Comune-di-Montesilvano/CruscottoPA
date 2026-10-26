package web

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

var longBody = strings.Repeat("parola ", 60)

func newAlert(t *testing.T, db *database.DB, title, body, level string) int64 {
	t.Helper()
	id, err := db.CreateAlert(database.Alert{Title: title, Body: body, Level: level, StartsAt: fixedNow.Add(-2 * time.Hour), CreatedAt: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestShortAlertReadOnDashboard(t *testing.T) {
	s, db := newTestServer(t, nil)
	short := newAlert(t, db, "Corto", "Solo testo.", database.LevelNews)
	long := newAlert(t, db, "Lungo", longBody, database.LevelNews)
	visit(t, s, "mrossi")
	if rs, _ := db.ReadsFor(short); len(rs) != 1 || rs[0].How != database.ReadPlancia || rs[0].Username != "mrossi" {
		t.Fatalf("corto: %+v", rs)
	}
	if rs, _ := db.ReadsFor(long); len(rs) != 0 {
		t.Fatalf("lungo letto senza «Ho letto»: %+v", rs)
	}
	do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Anonymous: true}), nil)
	if rs, _ := db.ReadsFor(short); len(rs) != 1 {
		t.Fatalf("anonimo registrato: %+v", rs)
	}
}

func TestShortUrgentNotAutoRead(t *testing.T) {
	s, db := newTestServer(t, nil)
	id := newAlert(t, db, "Urgente corto", "Solo testo.", database.LevelUrgent)
	visit(t, s, "mrossi")
	if rs, _ := db.ReadsFor(id); len(rs) != 0 {
		t.Fatalf("urgente letto senza conferma: %+v", rs)
	}
}

func TestReservedShortAlertNotRead(t *testing.T) {
	s, db := newTestServer(t, nil)
	g, _ := db.CreateAudienceGroup("Nessuno")
	id := newAlert(t, db, "Riservato", "Solo testo.", database.LevelNews)
	db.SetContentAudience(database.ContentAlert, id, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{g}})
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario"})
	// Con «Mostra tutto» l'avviso è in pagina, ma non è destinato a lui.
	body := do(t, s, "GET", "/", nil, nil, map[string]string{"Cookie": c.Name + "=" + c.Value + "; " + showAllCookie + "=1"}).Body.String()
	if !strings.Contains(body, "Riservato") {
		t.Fatal("con «Mostra tutto» l'avviso deve essere in pagina")
	}
	if rs, _ := db.ReadsFor(id); len(rs) != 0 {
		t.Fatalf("riservato letto: %+v", rs)
	}
}

func TestAlertsPartialCarriesReadState(t *testing.T) {
	s, db := newTestServer(t, nil)
	urg := newAlert(t, db, "Urgente", longBody, database.LevelUrgent)
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario"})
	do(t, s, "POST", "/avvisi/"+itoa(urg)+"/letto", url.Values{"come": {"conferma"}}, c, nil)
	want := `data-urgent="` + itoa(urg) + `" data-letto`
	if body := do(t, s, "GET", "/", nil, c, nil).Body.String(); !strings.Contains(body, want) {
		t.Fatalf("plancia senza stato di lettura")
	}
	short := newAlert(t, db, "Corto dopo", "Solo testo.", database.LevelNews)
	if body := do(t, s, "GET", "/partials/alerts", nil, c, hx).Body.String(); !strings.Contains(body, want) {
		t.Fatalf("refresh senza stato di lettura:\n%s", body)
	}
	if rs, _ := db.ReadsFor(short); len(rs) != 0 {
		t.Fatalf("il refresh ha registrato letture: %+v", rs)
	}
}

func TestReadButtonDisabled(t *testing.T) {
	s, db := newTestServer(t, nil)
	id := newAlert(t, db, "Lungo", longBody, database.LevelNews)
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario"})
	if body := do(t, s, "GET", "/", nil, c, nil).Body.String(); !strings.Contains(body, `class="news-read" data-read disabled`) {
		t.Fatal("«Ho letto» assente o già abilitato")
	}
	db.MarkRead(id, "mrossi", database.ReadConfirm, fixedNow)
	body := do(t, s, "GET", "/", nil, c, nil).Body.String()
	if strings.Contains(body, `class="news-read"`) || !strings.Contains(body, "news-read-done") {
		t.Fatal("avviso letto: deve mostrare «Letto»")
	}
}
