package web

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/backup"
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

// Il ripristino sostituisce il DB: la sua riga va nel file pending.
func TestAuditRestorePending(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	info, _ := s.backup.Create(backup.KindManual)
	rec := do(t, s, "POST", "/admin/backup/"+info.Name+"/ripristina", url.Values{"conferma": {"RIPRISTINA"}}, c, hx)
	if rec.Code != 200 || invalid(rec) {
		t.Fatalf("ripristino: %d", rec.Code)
	}
	waitExit(t, s)
	b, err := os.ReadFile(pendingAuditPath(s.cfg.DBPath))
	if err != nil {
		t.Fatal(err)
	}
	var e database.AdminLogEntry
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	if e.Section != "Backup" || e.Action != "backup ripristinato" || e.ObjectID != info.Name || e.Username != "mrossi" || !e.At.Equal(fixedNow) {
		t.Fatalf("riga: %+v", e)
	}
}

func TestAuditRestoreRejectedNoPending(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	dir := filepath.Dir(s.cfg.DBPath)
	bad := filepath.Join(dir, "backups", "manuale-rotto.tar.gz")
	os.MkdirAll(filepath.Dir(bad), 0o750)
	os.WriteFile(bad, []byte("non è un archivio"), 0o600)
	rec := do(t, s, "POST", "/admin/backup/manuale-rotto.tar.gz/ripristina", url.Values{"conferma": {"RIPRISTINA"}}, c, hx)
	if !invalid(rec) && rec.Code != 404 {
		t.Fatalf("atteso rifiuto: %d\n%s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(pendingAuditPath(s.cfg.DBPath)); !os.IsNotExist(err) {
		t.Fatalf("file pending creato: %v", err)
	}
}

func TestAuditPendingImportedAtStart(t *testing.T) {
	var p string
	_, db := newTestServerWith(t, nil, func(o *Options) {
		p = pendingAuditPath(o.Config.DBPath)
		database.WritePendingAdminLog(p, database.AdminLogEntry{At: fixedNow, Username: "mrossi", Section: "Backup", Action: "backup ripristinato", ObjectID: "manuale-1.tar.gz"})
	})
	rows := auditRows(t, db)
	if len(rows) != 1 || rows[0].ObjectID != "manuale-1.tar.gz" {
		t.Fatalf("righe: %+v", rows)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("file pending ancora presente: %v", err)
	}
}

func TestAuditPendingBrokenAtStart(t *testing.T) {
	var p string
	newTestServerWith(t, nil, func(o *Options) {
		p = pendingAuditPath(o.Config.DBPath)
		os.WriteFile(p, []byte("{rotto"), 0o600)
	})
	if _, err := os.Stat(p + ".bad"); err != nil {
		t.Fatalf("manca .bad: %v", err)
	}
}
