package database

import (
	"testing"
	"time"
)

func TestPresence(t *testing.T) {
	db := newTestDB(t)
	t0 := time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC)
	if err := db.TouchPresence("Mario.Rossi", "Mario Rossi", t0); err != nil {
		t.Fatal(err)
	}
	db.TouchPresence("mario.rossi", "", t0.Add(2*time.Minute)) // entro 5 minuti: niente scrittura
	db.TouchPresence("", "x", t0)                              // anonimo: ignorato
	ps, _ := db.ListPresence()
	if len(ps) != 1 || ps[0].Username != "mario.rossi" || ps[0].Name != "Mario Rossi" || !ps[0].LastSeen.Equal(t0) {
		t.Fatalf("presenza: %+v", ps)
	}
	db.TouchPresence("mario.rossi", "", t0.Add(6*time.Minute))
	ps, _ = db.ListPresence()
	if !ps[0].LastSeen.Equal(t0.Add(6*time.Minute)) || ps[0].Name != "Mario Rossi" {
		t.Fatalf("dopo 6 minuti: %+v", ps[0])
	}
	db.SetPresenceClient("mario.rossi", true, "granted", t0.Add(7*time.Minute))
	db.SetPresenceClient("mario.rossi", false, "boh", t0.Add(8*time.Minute)) // permesso non valido: resta granted
	db.SetPresenceClient("sconosciuto", true, "granted", t0)                 // senza riga: ignorato
	ps, _ = db.ListPresence()
	if len(ps) != 1 || ps[0].LastApp == nil || !ps[0].LastApp.Equal(t0.Add(7*time.Minute)) || ps[0].Permission != "granted" {
		t.Fatalf("client: %+v", ps[0])
	}
	db.TouchPresence("anna.bianchi", "Anna", t0.Add(-20*24*time.Hour))
	act, _ := db.ActiveUsernames(t0.Add(10 * time.Minute))
	if len(act) != 1 || act[0] != "mario.rossi" {
		t.Fatalf("attivi: %v", act)
	}
}
