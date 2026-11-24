package database

import "testing"

func TestChatPushUser(t *testing.T) {
	db := newTestDB(t)
	if on, err := db.ChatPushUser("mrossi"); err != nil || on {
		t.Fatalf("senza iscrizioni: %v %v", on, err)
	}
	if _, err := db.Exec(`INSERT INTO push_subscriptions (endpoint, p256dh, auth, username, created_at) VALUES ('https://push.example.it/1', 'k', 'a', 'MRossi', '2026-11-24T08:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if on, _ := db.ChatPushUser("mrossi"); !on {
		t.Fatal("iscrizione non trovata")
	}
}
