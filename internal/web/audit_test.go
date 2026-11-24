package web

import (
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// auditRows restituisce le righe del registro, dalla più recente.
func auditRows(t *testing.T, db *database.DB) []database.AdminLogEntry {
	t.Helper()
	list, _, err := db.ListAdminLog(database.AdminLogFilter{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// Ogni POST /admin/* registrato in server.go deve stare in auditRoutes o in
// auditSkip, e nessuna voce delle due mappe deve riferirsi a route sparite.
func TestAuditRoutesCover(t *testing.T) {
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, m := range regexp.MustCompile(`"(POST /admin[^"]*)"`).FindAllStringSubmatch(string(src), -1) {
		p := m[1]
		if p == "POST /admin/login" || p == "POST /admin/logout" {
			continue
		}
		found[p] = true
		_, mapped := auditRoutes[p]
		if !mapped && !auditSkip[p] {
			t.Errorf("route senza voce nel registro: %s (aggiungerla ad auditRoutes o auditSkip)", p)
		}
		if mapped && auditSkip[p] {
			t.Errorf("route sia registrata sia esclusa: %s", p)
		}
	}
	for p := range auditRoutes {
		if !found[p] {
			t.Errorf("auditRoutes: route inesistente %s", p)
		}
	}
	for p := range auditSkip {
		if !found[p] {
			t.Errorf("auditSkip: route inesistente %s", p)
		}
	}
}

func TestAuditLogsSuccess(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	rec := do(t, s, "POST", "/admin/categorie", url.Values{"name": {"Servizi"}}, c, hx)
	if rec.Code != 200 || invalid(rec) {
		t.Fatalf("creazione: %d", rec.Code)
	}
	rows := auditRows(t, db)
	if len(rows) != 1 || rows[0].Username != "mrossi" || rows[0].Section != "Categorie" || rows[0].Action != "creato" || !rows[0].At.Equal(fixedNow) {
		t.Fatalf("righe: %+v", rows)
	}
}

func TestAuditSkipsInvalid(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	rec := do(t, s, "POST", "/admin/categorie", url.Values{"name": {""}}, c, hx)
	if !invalid(rec) {
		t.Fatalf("atteso non valido: %d", rec.Code)
	}
	if rows := auditRows(t, db); len(rows) != 0 {
		t.Fatalf("righe: %+v", rows)
	}
}

func TestAuditSkipsExcluded(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	do(t, s, "POST", "/admin/anteprima", url.Values{"body": {"**ciao**"}}, c, hx)
	if rows := auditRows(t, db); len(rows) != 0 {
		t.Fatalf("righe: %+v", rows)
	}
}

func TestAuditSkipsNotFound(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	do(t, s, "POST", "/admin/app/999/elimina", nil, c, hx)
	do(t, s, "POST", "/admin/app/abc/elimina", nil, c, hx)
	if rows := auditRows(t, db); len(rows) != 0 {
		t.Fatalf("righe: %+v", rows)
	}
}

func TestAuditObjectNoRecord(t *testing.T) {
	auditObject(httptest.NewRequest("POST", "/", nil), 1, "x")
}
