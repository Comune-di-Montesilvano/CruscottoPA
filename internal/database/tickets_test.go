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
	n, err := db.CountTicketsSince("mrossi", now.Add(-time.Hour))
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
