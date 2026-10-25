package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
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

// Un errore del widget «Il tuo mese» non deve far cadere la plancia.
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
