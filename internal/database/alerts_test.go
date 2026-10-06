package database

import (
	"testing"
	"time"
)

var now = time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)

func mkAlert(t *testing.T, db *DB, title, level string, start time.Time, end *time.Time) int64 {
	t.Helper()
	id, err := db.CreateAlert(Alert{Title: title, Level: level, StartsAt: start, EndsAt: end, CreatedAt: now, CreatedBy: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func tp(t time.Time) *time.Time { return &t }

func TestListActiveAlerts(t *testing.T) {
	db := newTestDB(t)
	mkAlert(t, db, "novità", LevelNews, now.Add(-time.Hour), nil)
	mkAlert(t, db, "urgente", LevelUrgent, now.Add(-2*time.Hour), tp(now.Add(time.Hour)))
	mkAlert(t, db, "inizia ora", LevelMaintenance, now, nil)                 // starts_at == now → attivo
	mkAlert(t, db, "finisce ora", LevelUrgent, now.Add(-time.Hour), tp(now)) // ends_at == now → scaduto
	mkAlert(t, db, "futuro", LevelUrgent, now.Add(time.Hour), nil)

	got, err := db.ListActiveAlerts(now)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, a := range got {
		titles = append(titles, a.Title)
	}
	want := []string{"urgente", "inizia ora", "novità"} // ordine per livello
	if len(titles) != len(want) {
		t.Fatalf("attivi: atteso %v, ottenuto %v", want, titles)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Fatalf("attivi: atteso %v, ottenuto %v", want, titles)
		}
	}
	if got[0].EndsAt == nil || !got[0].EndsAt.Equal(now.Add(time.Hour)) || got[2].EndsAt != nil {
		t.Fatalf("EndsAt non letto correttamente: %+v", got)
	}
}

func TestAlertsForAdminAndUpdate(t *testing.T) {
	db := newTestDB(t)
	id := mkAlert(t, db, "vecchio", LevelNews, now.Add(-48*time.Hour), tp(now.Add(-24*time.Hour)))
	mkAlert(t, db, "programmato", LevelNews, now.Add(time.Hour), nil)

	current, expired, err := db.ListAlertsForAdmin(now)
	if err != nil || len(current) != 1 || len(expired) != 1 || expired[0].ID != id {
		t.Fatalf("ListAlertsForAdmin: %v %v %v", current, expired, err)
	}

	a, _ := db.GetAlert(id)
	a.EndsAt = nil // riattivato senza scadenza
	a.CreatedBy = "altro"
	if err := db.UpdateAlert(a); err != nil {
		t.Fatal(err)
	}
	a, _ = db.GetAlert(id)
	if a.EndsAt != nil || a.CreatedBy != "admin" {
		t.Fatalf("UpdateAlert: %+v (created_by non deve cambiare)", a)
	}
	if err := db.DeleteAlert(id); err != nil {
		t.Fatal(err)
	}
}
