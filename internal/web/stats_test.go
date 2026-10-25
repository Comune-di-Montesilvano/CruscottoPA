package web

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

func visit(t *testing.T, s *Server, users ...string) string {
	t.Helper()
	body := ""
	for _, u := range users {
		body = do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Username: u, Name: u}), nil).Body.String()
	}
	return body
}

func TestPillCountsRecognizedUsers(t *testing.T) {
	s, db := newTestServer(t, nil)
	body := visit(t, s, "mrossi", "anna", "luca", "mrossi")
	if !strings.Contains(body, "Oggi siamo in 3") {
		t.Fatalf("pastiglia assente o sbagliata:\n%s", pillSnippet(body))
	}
	if n, _ := db.UsersOnDay("2026-10-06"); n != 3 {
		t.Fatalf("utenti oggi: %d", n)
	}
}

func TestPillHiddenUnderTwo(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if body := visit(t, s, "mrossi"); strings.Contains(body, "hero-together") {
		t.Fatal("pastiglia con un solo utente")
	}
}

func TestPillAnonymous(t *testing.T) {
	s, db := newTestServer(t, nil)
	visit(t, s, "mrossi", "anna")
	s.stats.reset()
	body := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Anonymous: true}), nil).Body.String()
	if !strings.Contains(body, "Oggi siamo in 2") {
		t.Fatal("l'anonimo non vede la pastiglia")
	}
	do(t, s, "GET", "/", nil, nil, nil)
	if n, _ := db.UsersOnDay("2026-10-06"); n != 2 {
		t.Fatalf("anonimo contato: %d", n)
	}
}

func TestRecordDayUsesRomeCalendar(t *testing.T) {
	s, db := newTestServerWith(t, nil, func(o *Options) {
		o.Now = func() time.Time { return time.Date(2026, 10, 24, 22, 30, 0, 0, time.UTC) }
	})
	visit(t, s, "mrossi")
	if n, _ := db.UsersOnDay("2026-10-25"); n != 1 {
		t.Fatalf("giorno di Roma: %d", n)
	}
}

func TestMilestonePill(t *testing.T) {
	s, db := newTestServer(t, nil)
	if _, err := db.Exec(`UPDATE counters SET value = 999 WHERE name = 'accessi'`); err != nil {
		t.Fatal(err)
	}
	body := visit(t, s, "mrossi")
	if !strings.Contains(body, "Abbiamo superato 1.000 accessi") {
		t.Fatalf("traguardo assente:\n%s", pillSnippet(body))
	}
}

func TestStatsDisabled(t *testing.T) {
	s, db := newTestServerWith(t, nil, func(o *Options) { o.Config.StatsEnabled = false })
	body := visit(t, s, "mrossi", "anna")
	if strings.Contains(body, "hero-together") {
		t.Fatal("pastiglia con STATS_ENABLED=false")
	}
	if n, _ := db.UsersOnDay("2026-10-06"); n != 0 {
		t.Fatalf("accessi registrati: %d", n)
	}
}

func pillSnippet(body string) string {
	if i := strings.Index(body, `class="hello"`); i >= 0 {
		return body[i:min(len(body), i+600)]
	}
	return body
}

// readPctSetup: n utenti attivi, reads letture dell'avviso id.
func readPctSetup(t *testing.T, db *database.DB, id int64, n, reads int) {
	t.Helper()
	for i := 0; i < n; i++ {
		u := "u" + strconv.Itoa(i)
		db.TouchPresence(u, u, fixedNow)
		if i < reads {
			db.MarkRead(id, u, database.ReadConfirm, fixedNow)
		}
	}
}

func TestReadPct(t *testing.T) {
	s, db := newTestServer(t, nil)
	id := newAlert(t, db, "Cedolini", longBody, database.LevelNews)
	readPctSetup(t, db, id, 20, 15)
	if body := do(t, s, "GET", "/", nil, nil, nil).Body.String(); !strings.Contains(body, "Letto dal 75% dei colleghi") {
		t.Fatal("percentuale assente in plancia")
	}
	if body := do(t, s, "GET", "/partials/alerts", nil, nil, hx).Body.String(); !strings.Contains(body, "Letto dal 75% dei colleghi") {
		t.Fatal("percentuale assente nel refresh")
	}
}

func TestReadPctHidden(t *testing.T) {
	cases := map[string]func(t *testing.T, s *Server, db *database.DB){
		"riservato": func(t *testing.T, s *Server, db *database.DB) {
			id := newAlert(t, db, "X", longBody, database.LevelNews)
			g, _ := db.CreateAudienceGroup("Nessuno")
			db.SetContentAudience(database.ContentAlert, id, database.ContentAudience{Mode: audience.ModeHide, Groups: []int64{g}})
			readPctSetup(t, db, id, 20, 15)
		},
		"pochi_utenti": func(t *testing.T, s *Server, db *database.DB) {
			id := newAlert(t, db, "X", longBody, database.LevelNews)
			readPctSetup(t, db, id, 8, 8) // con mrossi che guarda: 9 attivi
		},
		"appena_iniziato": func(t *testing.T, s *Server, db *database.DB) {
			id, _ := db.CreateAlert(database.Alert{Title: "X", Body: longBody, Level: database.LevelNews, StartsAt: fixedNow.Add(-30 * time.Minute), CreatedAt: fixedNow})
			readPctSetup(t, db, id, 20, 15)
		},
		"sotto_soglia": func(t *testing.T, s *Server, db *database.DB) {
			id := newAlert(t, db, "X", longBody, database.LevelNews)
			readPctSetup(t, db, id, 20, 1)
		},
		"statistiche_spente": func(t *testing.T, s *Server, db *database.DB) {
			s.cfg.StatsEnabled = false
			id := newAlert(t, db, "X", longBody, database.LevelNews)
			readPctSetup(t, db, id, 20, 15)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			s, db := newTestServer(t, nil)
			setup(t, s, db)
			// mrossi vede anche l'avviso riservato (ModeHide di un gruppo non suo).
			body := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario"}), nil).Body.String()
			if !strings.Contains(body, "news-title") {
				t.Fatal("avviso non in pagina: il caso non prova nulla")
			}
			if strings.Contains(body, "news-readers") {
				t.Fatal("percentuale mostrata")
			}
		})
	}
}
