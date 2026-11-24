package database

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAdminLogAddList(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	rows := []AdminLogEntry{
		{At: now.Add(-2 * time.Hour), Username: "mrossi", Section: "Applicativi", Action: "creato", ObjectID: "12", Title: "Webmail"},
		{At: now.Add(-time.Hour), Username: "mrossi", Section: "Avvisi", Action: "modificato", ObjectID: "3", Title: "Sciopero"},
		{At: now, Username: "lbianchi", Section: "Avvisi", Action: "eliminato", ObjectID: "4", Title: "Manutenzione"},
	}
	for _, e := range rows {
		if err := db.AddAdminLog(e); err != nil {
			t.Fatal(err)
		}
	}
	list, more, err := db.ListAdminLog(AdminLogFilter{Limit: 2})
	if err != nil || len(list) != 2 || !more || list[0].Username != "lbianchi" || list[1].Title != "Sciopero" {
		t.Fatalf("prima pagina: %+v %v %v", list, more, err)
	}
	if !list[0].At.Equal(now) || list[0].ObjectID != "4" || list[0].Action != "eliminato" || list[0].ID == 0 {
		t.Fatalf("riga: %+v", list[0])
	}
	list, more, err = db.ListAdminLog(AdminLogFilter{Limit: 2, Offset: 2})
	if err != nil || len(list) != 1 || more || list[0].Title != "Webmail" {
		t.Fatalf("seconda pagina: %+v %v %v", list, more, err)
	}
	if list, _, _ = db.ListAdminLog(AdminLogFilter{Section: "Avvisi", Limit: 10}); len(list) != 2 {
		t.Fatalf("filtro sezione: %+v", list)
	}
	if list, _, _ = db.ListAdminLog(AdminLogFilter{Username: "lbianchi", Limit: 10}); len(list) != 1 {
		t.Fatalf("filtro utente: %+v", list)
	}
}

func TestAdminLogFacets(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	db.AddAdminLog(AdminLogEntry{At: now, Username: "mrossi", Section: "Avvisi", Action: "creato"})
	db.AddAdminLog(AdminLogEntry{At: now, Username: "lbianchi", Section: "Applicativi", Action: "creato"})
	db.AddAdminLog(AdminLogEntry{At: now, Username: "mrossi", Section: "Avvisi", Action: "eliminato"})
	users, sections, err := db.AdminLogFacets()
	if err != nil || !reflect.DeepEqual(users, []string{"lbianchi", "mrossi"}) || !reflect.DeepEqual(sections, []string{"Applicativi", "Avvisi"}) {
		t.Fatalf("valori: %v %v %v", users, sections, err)
	}
}

func TestAdminLogCleanup(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	db.AddAdminLog(AdminLogEntry{At: now.AddDate(0, -13, 0), Username: "mrossi", Section: "Avvisi", Action: "creato", Title: "vecchio"})
	db.AddAdminLog(AdminLogEntry{At: now.AddDate(0, -1, 0), Username: "mrossi", Section: "Avvisi", Action: "creato", Title: "recente"})
	if err := db.CleanupAdminLog(now.AddDate(0, -12, 0)); err != nil {
		t.Fatal(err)
	}
	list, _, _ := db.ListAdminLog(AdminLogFilter{Limit: 10})
	if len(list) != 1 || list[0].Title != "recente" {
		t.Fatalf("dopo la pulizia: %+v", list)
	}
}

func TestPendingAdminLog(t *testing.T) {
	db := newTestDB(t)
	p := filepath.Join(t.TempDir(), "admin_log_pending.json")
	e := AdminLogEntry{At: time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC), Username: "mrossi", Section: "Backup", Action: "backup ripristinato", ObjectID: "auto-1.tar.gz"}
	if err := WritePendingAdminLog(p, e); err != nil {
		t.Fatal(err)
	}
	if err := db.ImportPendingAdminLog(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("file pending ancora presente: %v", err)
	}
	if err := db.ImportPendingAdminLog(p); err != nil {
		t.Fatalf("file assente: %v", err)
	}
	list, _, _ := db.ListAdminLog(AdminLogFilter{Limit: 10})
	if len(list) != 1 || list[0].ObjectID != "auto-1.tar.gz" || list[0].Username != "mrossi" || !list[0].At.Equal(e.At) {
		t.Fatalf("righe: %+v", list)
	}
}

func TestPendingAdminLogBroken(t *testing.T) {
	db := newTestDB(t)
	p := filepath.Join(t.TempDir(), "admin_log_pending.json")
	os.WriteFile(p, []byte("{rotto"), 0o600)
	if err := db.ImportPendingAdminLog(p); err == nil {
		t.Fatal("atteso errore")
	}
	if _, err := os.Stat(p + ".bad"); err != nil {
		t.Fatalf("manca .bad: %v", err)
	}
	if list, _, _ := db.ListAdminLog(AdminLogFilter{Limit: 10}); len(list) != 0 {
		t.Fatalf("righe: %+v", list)
	}
}
