package database

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestPendingAndMarkNotified(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	on, _ := db.CreateAlert(Alert{Title: "attivo", Level: LevelUrgent, Notify: true, StartsAt: now.Add(-time.Minute)})
	db.CreateAlert(Alert{Title: "senza notifica", Level: LevelNews, StartsAt: now.Add(-time.Minute)})
	later, _ := db.CreateAlert(Alert{Title: "programmato", Level: LevelNews, Notify: true, StartsAt: future})

	p, err := db.PendingNotifications(now)
	if err != nil || len(p) != 1 || p[0].ID != on {
		t.Fatalf("pending: %+v %v", p, err)
	}
	if ok, err := db.MarkNotified(on, now); !ok || err != nil {
		t.Fatalf("prima marcatura: %v %v", ok, err)
	}
	if ok, _ := db.MarkNotified(on, now); ok {
		t.Fatal("seconda marcatura: l'avviso era già notificato")
	}
	if p, _ := db.PendingNotifications(now); len(p) != 0 {
		t.Fatalf("dopo la marcatura: %+v", p)
	}
	if p, _ := db.PendingNotifications(future.Add(time.Second)); len(p) != 1 || p[0].ID != later {
		t.Fatalf("avviso programmato all'orario di inizio: %+v", p)
	}
	a, _ := db.GetAlert(on)
	if a.NotifiedAt == nil || !a.NotifiedAt.Equal(now) {
		t.Fatalf("NotifiedAt: %v", a.NotifiedAt)
	}
}

func TestPushSubscriptions(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	ps := PushSubscription{Endpoint: "https://push.example/a", P256dh: "k1", Auth: "a1", Username: "mrossi", CreatedAt: now}
	if ok, err := db.SavePushSubscription(ps, 1); !ok || err != nil {
		t.Fatal(ok, err)
	}
	ps.P256dh, ps.Username = "k2", ""
	if ok, err := db.SavePushSubscription(ps, 1); !ok || err != nil {
		t.Fatal("al tetto il rinnovo dello stesso endpoint va accettato:", ok, err)
	}
	if ok, _ := db.SavePushSubscription(PushSubscription{Endpoint: "https://push.example/c", P256dh: "k", Auth: "a", CreatedAt: now}, 1); ok {
		t.Fatal("oltre il tetto: nuova iscrizione accettata")
	}
	all, _ := db.ListPushSubscriptions()
	if len(all) != 1 || all[0].P256dh != "k2" || all[0].Username != "" {
		t.Fatalf("upsert per endpoint: %+v", all)
	}
	db.SavePushSubscription(PushSubscription{Endpoint: "https://push.example/b", P256dh: "k", Auth: "a", Username: "mrossi", CreatedAt: now}, 10)
	if mine, _ := db.ListPushSubscriptionsFor("MRossi"); len(mine) != 1 || mine[0].Endpoint != "https://push.example/b" {
		t.Fatalf("per utente: %+v", mine)
	}
	if err := db.TouchPushSubscription("https://push.example/b", now); err != nil {
		t.Fatal(err)
	}
	if err := db.DeletePushSubscription("https://push.example/a"); err != nil {
		t.Fatal(err)
	}
	if all, _ := db.ListPushSubscriptions(); len(all) != 1 {
		t.Fatalf("dopo la cancellazione: %+v", all)
	}
}

func TestEnsureVAPIDKeys(t *testing.T) {
	db := newTestDB(t)
	calls := 0
	gen := func() (string, string, error) { calls++; return "priv", "pub", nil }
	pub, priv, err := db.EnsureVAPIDKeys(gen)
	if err != nil || pub != "pub" || priv != "priv" {
		t.Fatalf("prima generazione: %q %q %v", pub, priv, err)
	}
	if pub, _, _ := db.EnsureVAPIDKeys(gen); pub != "pub" || calls != 1 {
		t.Fatalf("le chiavi si generano una volta sola: calls=%d", calls)
	}
}

// Gli avvisi già attivi al momento della migrazione non vanno notificati in ritardo.
func TestMigrationMarksActiveAlerts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v5.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := raw.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations[:5] {
		if err := m(tx); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	for title, starts := range map[string]time.Time{"attivo": now.Add(-time.Hour), "futuro": now.Add(time.Hour)} {
		if _, err := tx.Exec(`INSERT INTO alerts (title, body, level, source, starts_at, notify, created_at, created_by)
VALUES (?, '', 'urgent', '', ?, 1, ?, 'admin')`, title, formatTime(starts), formatTime(now)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`PRAGMA user_version = 5`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if p, _ := db.PendingNotifications(now.Add(2 * time.Hour)); len(p) != 1 || p[0].Title != "futuro" {
		t.Fatalf("solo l'avviso programmato resta da notificare: %+v", p)
	}
}
