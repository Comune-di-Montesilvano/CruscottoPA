package database

import (
	"strconv"
	"testing"
	"time"
)

func TestTicketsRecordCountList(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	for i, at := range []time.Time{now.Add(-2 * time.Hour), now.Add(-30 * time.Minute), now} {
		if err := db.RecordTicket(TicketSent{Username: "MRossi", Name: "Mario Rossi", Email: "mrossi@example.it",
			Subject: "Prova", TicketID: strconv.Itoa(i), TicketNumber: "N" + strconv.Itoa(i), CustomerSet: i != 1, Attachments: i, CreatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	db.RecordTicket(TicketSent{Username: "altro", Name: "A", Email: "a@example.it", Subject: "x", TicketID: "9", TicketNumber: "N9", CreatedAt: now})
	n, err := db.CountTicketsSince("mrossi", TicketOpen, now.Add(-time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("conteggio ultima ora: %d %v", n, err)
	}
	list, err := db.ListTickets(3)
	if err != nil || len(list) != 3 || list[2].TicketNumber != "N1" {
		t.Fatalf("elenco: %+v %v", list, err)
	}
	got := list[2]
	if got.Username != "mrossi" || got.CustomerSet || got.Attachments != 1 || !got.CreatedAt.Equal(now.Add(-30*time.Minute)) {
		t.Fatalf("riga: %+v", got)
	}
}

func TestTicketsPC(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	if err := db.RecordTicket(TicketSent{Username: "mrossi", Name: "M", Email: "m@example.it", Subject: "s", TicketID: "1", TicketNumber: "N1", PC: "PC-PROVA-001", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	list, err := db.ListTickets(1)
	if err != nil || list[0].PC != "PC-PROVA-001" {
		t.Fatalf("PC: %+v %v", list, err)
	}
}

func TestTicketsKindAndCount(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	db.RecordTicket(TicketSent{Username: "mrossi", Name: "M", Email: "m@example.it", Subject: "s", TicketID: "1", TicketNumber: "N1", CreatedAt: now})
	db.RecordTicket(TicketSent{Username: "mrossi", Name: "M", Email: "m@example.it", Subject: "s", TicketID: "1", TicketNumber: "N1", Kind: TicketReply, CreatedAt: now})
	open, _ := db.CountTicketsSince("mrossi", TicketOpen, now.Add(-time.Hour))
	replies, _ := db.CountTicketsSince("mrossi", TicketReply, now.Add(-time.Hour))
	if open != 1 || replies != 1 {
		t.Fatalf("conteggi: %d %d", open, replies)
	}
	list, _ := db.ListTickets(5)
	if list[0].Kind == "" || list[1].Kind == "" {
		t.Fatalf("kind: %+v", list)
	}
}

func TestTicketSeen(t *testing.T) {
	db := newTestDB(t)
	at := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	db.MarkTicketSeen("MRossi", "5", at)
	db.MarkTicketSeen("mrossi", "5", at.Add(time.Hour))
	seen, err := db.TicketSeen("mrossi")
	if err != nil || len(seen) != 1 || !seen["5"].Equal(at.Add(time.Hour)) {
		t.Fatalf("visti: %v %v", seen, err)
	}
}

func TestTicketNotifyStateAndUsers(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	if _, ok, _ := db.TicketNotifyState("5"); ok {
		t.Fatal("stato inesistente")
	}
	db.SetTicketNotifyState("5", 102, now)
	if last, ok, err := db.TicketNotifyState("5"); !ok || last != 102 || err != nil {
		t.Fatalf("stato: %d %v %v", last, ok, err)
	}
	db.UpsertTicketUser("MRossi@Example.it", "mrossi", now)
	if u, _ := db.TicketUserFor("mrossi@example.it"); u != "mrossi" {
		t.Fatalf("mail → utente: %q", u)
	}
	if u, _ := db.TicketUserFor("nessuno@example.it"); u != "" {
		t.Fatalf("mail sconosciuta: %q", u)
	}
	if err := db.CleanupTicketState(now.Add(31 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := db.TicketNotifyState("5"); ok {
		t.Fatal("pulizia: stato vecchio rimasto")
	}
	if u, _ := db.TicketUserFor("mrossi@example.it"); u != "" {
		t.Fatal("pulizia: utente vecchio rimasto")
	}
}

// L'associazione mail → utente si riscrive solo se cambia o dopo 24 ore.
func TestUpsertTicketUserThrottled(t *testing.T) {
	db := newTestDB(t)
	t0 := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	updated := func() string {
		var at string
		db.QueryRow(`SELECT updated_at FROM ticket_users`).Scan(&at)
		return at
	}
	db.UpsertTicketUser("m@example.it", "mrossi", t0)
	db.UpsertTicketUser("m@example.it", "mrossi", t0.Add(time.Hour))
	if updated() != formatTime(t0) {
		t.Fatalf("riscritto entro 24 ore: %s", updated())
	}
	db.UpsertTicketUser("m@example.it", "mrossi", t0.Add(25*time.Hour))
	if updated() != formatTime(t0.Add(25*time.Hour)) {
		t.Fatalf("non rinnovato dopo 24 ore: %s", updated())
	}
	db.UpsertTicketUser("m@example.it", "altro", t0.Add(26*time.Hour))
	if u, _ := db.TicketUserFor("m@example.it"); u != "altro" {
		t.Fatalf("utente cambiato non registrato: %q", u)
	}
}
