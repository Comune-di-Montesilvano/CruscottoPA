package web

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/backup"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func waitExit(t *testing.T, s *Server) {
	t.Helper()
	select {
	case code := <-serverExits[s]:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("il ripristino non ha chiamato exit")
	}
}

func categoryNames(t *testing.T, dbPath string) string {
	t.Helper()
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cats, _ := db.ListCategories()
	var out []string
	for _, c := range cats {
		out = append(out, c.Name)
	}
	return strings.Join(out, ",")
}

func TestBackupPageAndCreate(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)

	page := do(t, s, "GET", "/admin/backup", nil, c, nil).Body.String()
	if !strings.Contains(page, "stesso server") || !strings.Contains(page, "Nessun backup") {
		t.Fatalf("pagina backup:\n%s", page)
	}
	rec := do(t, s, "POST", "/admin/backup", nil, c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Backup creato") || !strings.Contains(rec.Body.String(), "Manuale") {
		t.Fatalf("crea: %d\n%s", rec.Code, rec.Body)
	}
	rec = do(t, s, "POST", "/admin/backup", nil, c, hx) // stesso secondo (orologio fisso)
	if !invalid(rec) || !strings.Contains(rec.Body.String(), "esiste già") {
		t.Fatalf("doppio click: %d\n%s", rec.Code, rec.Body)
	}
}

func TestBackupDownloadAndDelete(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	info, _ := s.backup.Create(backup.KindManual)

	rec := do(t, s, "GET", "/admin/backup/"+info.Name, nil, c, nil)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") || !bytes.HasPrefix(rec.Body.Bytes(), []byte{0x1f, 0x8b}) {
		t.Fatalf("download: %d %v", rec.Code, rec.Header())
	}
	if rec := do(t, s, "GET", "/admin/backup/cruscotto-x.tar.gz", nil, c, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("nome non valido: %d", rec.Code)
	}
	if rec := do(t, s, "POST", "/admin/backup/"+info.Name+"/elimina", nil, c, hx); rec.Code != 200 {
		t.Fatalf("elimina: %d", rec.Code)
	}
	if list, _ := s.backup.List(); len(list) != 0 {
		t.Fatal("backup non eliminato")
	}

	auto, _ := s.backup.Create(backup.KindAuto)
	rec = do(t, s, "POST", "/admin/backup/"+auto.Name+"/elimina", nil, c, hx)
	if !invalid(rec) || !strings.Contains(rec.Body.String(), "conservazione automatica") {
		t.Fatalf("auto non eliminabile: %d\n%s", rec.Code, rec.Body)
	}
}

func TestRestoreFromList(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	db.CreateCategory("Prima")
	info, _ := s.backup.Create(backup.KindManual)
	db.CreateCategory("Dopo")
	path := "/admin/backup/" + info.Name + "/ripristina"

	rec := do(t, s, "POST", path, url.Values{"conferma": {"ripristina"}}, c, hx)
	if !invalid(rec) || !strings.Contains(rec.Body.String(), "digita RIPRISTINA") {
		t.Fatalf("conferma errata: %d\n%s", rec.Code, rec.Body)
	}
	if list, _ := s.backup.List(); len(list) != 1 {
		t.Fatal("senza conferma non deve partire nulla (niente pre-ripristino)")
	}

	rec = do(t, s, "POST", path, url.Values{"conferma": {"RIPRISTINA"}}, c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "data-restarting") {
		t.Fatalf("ripristino: %d\n%s", rec.Code, rec.Body)
	}
	waitExit(t, s)
	if names := categoryNames(t, s.cfg.DBPath); !strings.Contains(names, "Prima") || strings.Contains(names, "Dopo") {
		t.Fatalf("dati non ripristinati: %s", names)
	}
}

func TestBackupRequiresAdminAndSameOrigin(t *testing.T) {
	s, _ := newTestServer(t, nil)
	info, _ := s.backup.Create(backup.KindManual)
	for _, p := range []string{"/admin/backup", "/admin/backup/" + info.Name} {
		if rec := do(t, s, "GET", p, nil, nil, nil); rec.Code != http.StatusSeeOther {
			t.Errorf("%s senza sessione: %d", p, rec.Code)
		}
	}
	c := login(t, s)
	if rec := do(t, s, "POST", "/admin/backup", nil, c, map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Fatalf("POST cross-origin: %d", rec.Code)
	}
}

func TestOverviewBackupWarning(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	if body := do(t, s, "GET", "/admin", nil, c, nil).Body.String(); !strings.Contains(body, "Non è ancora stato fatto nessun backup") {
		t.Fatal("manca l'avviso senza backup")
	}
	s.backup.Create(backup.KindManual)
	if body := do(t, s, "GET", "/admin", nil, c, nil).Body.String(); strings.Contains(body, "nessun backup") {
		t.Fatal("avviso presente nonostante il backup")
	}
}

func TestBackupWarning(t *testing.T) {
	now := time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		st   backup.Status
		want string
	}{
		"nessuno": {backup.Status{}, "Non è ancora stato fatto nessun backup."},
		"recente": {backup.Status{LastSuccess: now.Add(-47 * time.Hour)}, ""},
		"vecchio": {backup.Status{LastSuccess: now.Add(-49 * time.Hour)}, "più di 48 ore"},
		"fallito": {backup.Status{LastSuccess: now, LastAutoError: "disco pieno"}, "disco pieno"},
	}
	for name, tc := range cases {
		got := backupWarning(tc.st, now)
		if (tc.want == "" && got != "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q", name, got)
		}
	}
}

// Un doppio click su "Ripristina" restituiva "operazione già in corso" al posto
// della pagina d'attesa, che quindi non si ricaricava più: i pulsanti delle
// azioni lunghe vanno disabilitati durante la richiesta.
func TestBackupActionsDisableDuringRequest(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	s.backup.Create(backup.KindManual)
	page := do(t, s, "GET", "/admin/backup", nil, c, nil).Body.String()
	if n := strings.Count(page, `hx-disabled-elt="find button"`); n < 1 {
		t.Fatalf("il form di ripristino deve disabilitare il pulsante (trovati %d)\n%s", n, page)
	}
	if !strings.Contains(page, `hx-post="/admin/backup" hx-disabled-elt="this"`) {
		t.Fatal(`"Crea backup ora" deve disabilitarsi durante la richiesta`)
	}
}
