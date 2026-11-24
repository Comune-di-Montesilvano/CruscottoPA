package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestAdminRegistroPage(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	db.AddAdminLog(database.AdminLogEntry{At: fixedNow, Username: "mrossi", Section: "Applicativi", Action: "eliminato", ObjectID: "12", Title: "Webmail"})
	db.AddAdminLog(database.AdminLogEntry{At: fixedNow.Add(-time.Hour), Username: "lbianchi", Section: "Avvisi", Action: "creato", ObjectID: "3", Title: "Sciopero"})
	db.AddAdminLog(database.AdminLogEntry{At: fixedNow.Add(-2 * time.Hour), Username: "lbianchi", Section: "Guide", Action: "modificato", ObjectID: "7"})
	db.AddAdminLog(database.AdminLogEntry{At: fixedNow.Add(-3 * time.Hour), Username: "lbianchi", Section: "Ente", Action: "modificato"})

	body := do(t, s, "GET", "/admin/registro", nil, c, nil).Body.String()
	for _, want := range []string{"Webmail", "Sciopero", "10:00", "mrossi", "Applicativi", "eliminato", "Guide #7", `aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	body = do(t, s, "GET", "/admin/registro?chi=lbianchi", nil, c, nil).Body.String()
	if strings.Contains(body, "Webmail") || !strings.Contains(body, "Sciopero") {
		t.Fatalf("filtro utente:\n%s", body)
	}
	body = do(t, s, "GET", "/admin/registro?sezione=Avvisi", nil, c, nil).Body.String()
	if strings.Contains(body, "Webmail") || !strings.Contains(body, "Sciopero") {
		t.Fatalf("filtro sezione:\n%s", body)
	}
	for _, q := range []string{"?sezione=Inesistente", "?pagina=99"} {
		rec := do(t, s, "GET", "/admin/registro"+q, nil, c, nil)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Nessuna modifica registrata.") {
			t.Fatalf("%s: %d\n%s", q, rec.Code, rec.Body)
		}
	}
	if body = do(t, s, "GET", "/admin/registro?pagina=abc", nil, c, nil).Body.String(); !strings.Contains(body, "Webmail") {
		t.Fatalf("pagina non valida = prima pagina:\n%s", body)
	}
}

func TestAdminRegistroPaging(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	for i := 0; i < adminLogPage+1; i++ {
		title := "Riga"
		if i == adminLogPage {
			title = "Ultima più vecchia"
		}
		db.AddAdminLog(database.AdminLogEntry{At: fixedNow.Add(-time.Duration(i) * time.Minute), Username: "mrossi", Section: "Avvisi", Action: "creato", Title: title})
	}
	body := do(t, s, "GET", "/admin/registro?chi=mrossi", nil, c, nil).Body.String()
	if strings.Contains(body, "Ultima più vecchia") || !strings.Contains(body, "pagina=2") || !strings.Contains(body, "chi=mrossi") {
		t.Fatalf("prima pagina:\n%s", body)
	}
	body = do(t, s, "GET", "/admin/registro?chi=mrossi&pagina=2", nil, c, nil).Body.String()
	if !strings.Contains(body, "Ultima più vecchia") {
		t.Fatalf("seconda pagina:\n%s", body)
	}
}

func TestAdminRegistroRequiresLogin(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if rec := do(t, s, "GET", "/admin/registro", nil, nil, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("senza sessione: %d", rec.Code)
	}
}

func TestAdminLogCleanupInDailyRound(t *testing.T) {
	s, db := newTestServer(t, nil)
	db.AddAdminLog(database.AdminLogEntry{At: fixedNow.AddDate(0, -13, 0), Username: "mrossi", Section: "Avvisi", Action: "creato", Title: "vecchio"})
	db.AddAdminLog(database.AdminLogEntry{At: fixedNow.AddDate(0, -1, 0), Username: "mrossi", Section: "Avvisi", Action: "creato", Title: "recente"})
	s.cleanupAdminLog()
	rows := auditRows(t, db)
	if len(rows) != 1 || rows[0].Title != "recente" {
		t.Fatalf("righe: %+v", rows)
	}
}
