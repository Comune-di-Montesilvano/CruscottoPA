package web

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

// Le letture «in plancia» non devono dire a che ora uno è arrivato: solo il giorno.
func TestPlanciaReadKeepsOnlyTheDay(t *testing.T) {
	s, db := newTestServer(t, nil)
	id := newAlert(t, db, "Corto", "Solo testo.", database.LevelNews)
	visit(t, s, "mrossi")
	rs, _ := db.ReadsFor(id)
	if len(rs) != 1 {
		t.Fatalf("letture: %+v", rs)
	}
	// fixedNow = 10:00 di Roma del 06/10: mezzanotte di Roma = 22:00Z del 05/10.
	if got := rs[0].ReadAt.UTC().Format("2006-01-02T15:04:05Z"); got != "2026-10-05T22:00:00Z" {
		t.Fatalf("read_at = %s, voluto il solo giorno", got)
	}
	body := do(t, s, "GET", "/admin/avvisi/"+itoa(id)+"/letture", nil, login(t, s), nil).Body.String()
	if !strings.Contains(body, "06/10/2026") || strings.Contains(body, "06/10/2026 00:00") {
		t.Fatalf("la pagina delle letture deve mostrare solo la data:\n%s", body)
	}
}

// Un errore del widget «I tuoi ultimi 30 giorni» non deve far cadere la plancia.
func TestDashboardSurvivesMonthError(t *testing.T) {
	s, db := newTestServer(t, nil)
	if _, err := db.Exec(`DROP TABLE push_subscriptions`); err != nil {
		t.Fatal(err)
	}
	rec := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "mrossi"}), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "widget-mese") {
		t.Fatal("widget mostrato nonostante l'errore")
	}
}

// Il traguardo va nel giorno in cui viene superato, anche con accessi ravvicinati.
func TestMilestoneNotThrottled(t *testing.T) {
	s, db := newTestServer(t, nil)
	if _, err := db.Exec(`UPDATE counters SET value = 997 WHERE name = 'accessi'`); err != nil {
		t.Fatal(err)
	}
	visit(t, s, "anna", "luca", "mrossi") // 998, 999, 1000 nello stesso minuto
	if th, ok, _ := db.MilestoneOn("2026-10-06"); !ok || th != 1000 {
		t.Fatalf("traguardo: %d %v", th, ok)
	}
}

// STATS_ENABLED=false: niente fiammella, percentuale, widget, clic.
func TestStatsDisabledHidesEverything(t *testing.T) {
	s, db := newTestServerWith(t, nil, func(o *Options) { o.Config.StatsEnabled = false })
	app := webApp(t, db, "protocollo")
	for _, d := range []string{"2026-10-01", "2026-10-02", "2026-10-03", "2026-10-04", "2026-10-05", "2026-10-06"} {
		db.AddAppClick(app, d)
	}
	id := newAlert(t, db, "Mensa", "Solo testo.", database.LevelNews)
	readPctSetup(t, db, id, 12, 12)
	click(t, s, "/app/"+itoa(app)+"/clic", "mrossi")
	if n := clicks(t, db, app); n != 6 {
		t.Fatalf("clic registrato: %d", n)
	}
	body := visit(t, s, "mrossi")
	for _, bad := range []string{"tile-hot", "news-readers", "widget-mese"} {
		if strings.Contains(body, bad) {
			t.Errorf("%s con STATS_ENABLED=false", bad)
		}
	}
}

// Senza cookie nessuna lettura e nessun «Ho letto» (il server lo ignorerebbe).
func TestUnknownViewerNoReads(t *testing.T) {
	s, db := newTestServer(t, nil)
	short := newAlert(t, db, "Corto", "Solo testo.", database.LevelNews)
	newAlert(t, db, "Lungo", longBody, database.LevelNews)
	for _, c := range []*http.Cookie{nil, viewerCookie(t, s, identity.User{Anonymous: true})} {
		body := do(t, s, "GET", "/", nil, c, nil).Body.String()
		if strings.Contains(body, "data-read ") || strings.Contains(body, "news-read\"") {
			t.Errorf("«Ho letto» per chi non è riconosciuto (cookie %v)", c != nil)
		}
	}
	if rs, _ := db.ReadsFor(short); len(rs) != 0 {
		t.Fatalf("letture senza utente: %+v", rs)
	}
	body := visit(t, s, "mrossi")
	if !strings.Contains(body, `class="news-read" data-read`) {
		t.Fatal("«Ho letto» sparito anche per i riconosciuti")
	}
}

// A inizio mese, senza richieste, niente «0 richieste, 0 risolte».
func TestAssistLineHiddenWhenEmpty(t *testing.T) {
	s, c := ticketTestServer(t, otrs.NewMock())
	s.db.SaveTicketStats(database.TicketStats{UpdatedAt: fixedNow})
	if body := do(t, s, "GET", "/", nil, c, nil).Body.String(); strings.Contains(body, "month-assist") {
		t.Fatal("riga dell'assistenza vuota mostrata")
	}
}

// La spilla «Notifiche attive» si accende appena l'iscrizione riesce.
func TestNotifyBadgeLightsUp(t *testing.T) {
	js, err := os.ReadFile("../../web/static/js/notifiche.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), `classList.add("earned")`) {
		t.Fatal("notifiche.js non accende la spilla dopo l'iscrizione")
	}
}
