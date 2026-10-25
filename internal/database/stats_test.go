package database

import (
	"reflect"
	"testing"
)

func TestRecordDayOncePerDay(t *testing.T) {
	db := newTestDB(t)
	added, err := db.RecordDay("MRossi", "2026-10-23")
	if err != nil || !added {
		t.Fatalf("prima: %v %v", added, err)
	}
	if added, err = db.RecordDay("mrossi", "2026-10-23"); err != nil || added {
		t.Fatalf("seconda: %v %v", added, err)
	}
	if added, _ = db.RecordDay("  ", "2026-10-23"); added {
		t.Fatal("username vuoto registrato")
	}
	if n, _ := db.UsersOnDay("2026-10-23"); n != 1 {
		t.Fatalf("utenti del giorno: %d", n)
	}
	if n, _ := db.AccessTotal(); n != 1 {
		t.Fatalf("totale: %d", n)
	}
}

func TestAccessTotalSurvivesCleanup(t *testing.T) {
	db := newTestDB(t)
	for _, d := range []string{"2026-10-21", "2026-10-22", "2026-10-23"} {
		db.RecordDay("mrossi", d)
	}
	if err := db.CleanupStats("2026-10-23", "2026-07-25"); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.UsersOnDay("2026-10-21"); n != 0 {
		t.Fatalf("giorno vecchio rimasto: %d", n)
	}
	if n, _ := db.UsersOnDay("2026-10-23"); n != 1 {
		t.Fatalf("giorno recente sparito: %d", n)
	}
	if n, _ := db.AccessTotal(); n != 3 {
		t.Fatalf("totale calato: %d", n)
	}
}

func TestMilestones(t *testing.T) {
	if got, want := Milestones(250000), []int64{1000, 2500, 5000, 10000, 25000, 50000, 100000, 200000}; !reflect.DeepEqual(got, want) {
		t.Fatalf("soglie: %v", got)
	}
	if got := Milestones(999); len(got) != 0 {
		t.Fatalf("sotto la prima soglia: %v", got)
	}
	db := newTestDB(t)
	if err := db.ReachMilestones(1200, "2026-10-23"); err != nil {
		t.Fatal(err)
	}
	if th, ok, _ := db.MilestoneOn("2026-10-23"); !ok || th != 1000 {
		t.Fatalf("traguardo: %d %v", th, ok)
	}
	db.ReachMilestones(1300, "2026-10-24")
	if _, ok, _ := db.MilestoneOn("2026-10-24"); ok {
		t.Fatal("soglia già raggiunta riscritta")
	}
	db.ReachMilestones(5200, "2026-10-25")
	if th, ok, _ := db.MilestoneOn("2026-10-25"); !ok || th != 5000 {
		t.Fatalf("più soglie: vale la più alta, %d %v", th, ok)
	}
}

func TestUserDaysBetween(t *testing.T) {
	db := newTestDB(t)
	for _, d := range []string{"2026-10-01", "2026-10-15", "2026-10-31", "2026-11-01"} {
		db.RecordDay("mrossi", d)
	}
	db.RecordDay("anna", "2026-10-15")
	if n, _ := db.UserDaysBetween("MRossi", "2026-10-01", "2026-10-31"); n != 3 {
		t.Fatalf("giorni di ottobre: %d", n)
	}
}

func TestDeleteUserDays(t *testing.T) {
	db := newTestDB(t)
	db.RecordDay("ex", "2026-10-23")
	db.RecordDay("mrossi", "2026-10-23")
	if err := db.DeleteUserDays("EX"); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.UsersOnDay("2026-10-23"); n != 1 {
		t.Fatalf("utenti: %d", n)
	}
}
