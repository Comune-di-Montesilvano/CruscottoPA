package database

import (
	"reflect"
	"testing"
	"time"
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

func newTestApp(t *testing.T, db *DB, title string) int64 {
	t.Helper()
	cats, _ := db.ListCategories()
	id, err := db.CreateApp(App{CategoryID: cats[0].ID, Title: title, URL: "https://" + title + ".local",
		IconKind: IconPack, IconValue: "apps", IconColor: "#475569", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAppClicks(t *testing.T) {
	db := newTestDB(t)
	a, b := newTestApp(t, db, "a"), newTestApp(t, db, "b")
	db.AddAppClick(a, "2026-10-23")
	db.AddAppClick(a, "2026-10-23")
	db.AddAppClick(b, "2026-10-23")
	db.AddAppClick(a, "2026-10-13")
	if err := db.AddAppClick(99999, "2026-10-23"); err == nil {
		t.Fatal("app inesistente accettata")
	}
	got, err := db.AppClicksSince("2026-10-17")
	if err != nil || !reflect.DeepEqual(got, map[int64]int{a: 2, b: 1}) {
		t.Fatalf("clic: %v %v", got, err)
	}
	db.DeleteApp(a)
	if got, _ = db.AppClicksSince("2026-10-17"); !reflect.DeepEqual(got, map[int64]int{b: 1}) {
		t.Fatalf("dopo l'eliminazione: %v", got)
	}
}

func TestTicketsOpenedBetween(t *testing.T) {
	db := newTestDB(t)
	oct := mustTime(t, "2026-10-10T08:00:00Z")
	for _, r := range []TicketSent{
		{Username: "mrossi", TicketID: "1", Kind: TicketOpen, CreatedAt: oct},
		{Username: "mrossi", TicketID: "1", Kind: TicketReply, CreatedAt: oct},
		{Username: "mrossi", TicketID: "2", Kind: TicketClose, CreatedAt: oct},
		{Username: "anna", TicketID: "3", Kind: TicketOpen, CreatedAt: oct},
		{Username: "mrossi", TicketID: "4", Kind: TicketOpen, CreatedAt: mustTime(t, "2026-09-10T08:00:00Z")},
	} {
		if err := db.RecordTicket(r); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := db.TicketsOpenedBetween("MRossi", mustTime(t, "2026-10-01T00:00:00Z"), mustTime(t, "2026-11-01T00:00:00Z"))
	if err != nil || !reflect.DeepEqual(ids, []string{"1"}) {
		t.Fatalf("ticket: %v %v", ids, err)
	}
	if ok, _ := db.HasOpenedTicket("mrossi"); !ok {
		t.Fatal("mrossi ha aperto ticket")
	}
	if ok, _ := db.HasOpenedTicket("luca"); ok {
		t.Fatal("luca non ne ha")
	}
}

func TestHasPushSubscription(t *testing.T) {
	db := newTestDB(t)
	db.SavePushSubscription(PushSubscription{Endpoint: "https://fcm.googleapis.com/x", P256dh: "p", Auth: "a", Username: "mrossi", CreatedAt: mustTime(t, "2026-10-10T08:00:00Z")}, 100)
	if ok, _ := db.HasPushSubscription("MRossi"); !ok {
		t.Fatal("iscrizione non trovata")
	}
	if ok, _ := db.HasPushSubscription(""); ok {
		t.Fatal("username vuoto")
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTicketFactsReopen(t *testing.T) {
	db := newTestDB(t)
	db.UpsertTicketCreated("1", mustTime(t, "2026-10-02T08:00:00Z"))
	if err := db.SetClosedMonth("2026-10", []string{"1", "2"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.ClosedInMonth("2026-10", []string{"1", "2", "3"}); n != 2 {
		t.Fatalf("chiusi: %d", n)
	}
	db.SetClosedMonth("2026-10", []string{"1"}) // il 2 è stato riaperto
	if n, _ := db.ClosedInMonth("2026-10", []string{"1", "2"}); n != 1 {
		t.Fatalf("dopo la riapertura: %d", n)
	}
	if n, _ := db.ClosedInMonth("2026-10", nil); n != 0 {
		t.Fatalf("nessun id: %d", n)
	}
}

func TestTicketsWithoutReply(t *testing.T) {
	db := newTestDB(t)
	from := mustTime(t, "2026-10-01T00:00:00Z")
	db.UpsertTicketCreated("1", mustTime(t, "2026-10-02T08:00:00Z"))
	db.UpsertTicketCreated("2", mustTime(t, "2026-10-03T08:00:00Z"))
	db.UpsertTicketCreated("3", mustTime(t, "2026-10-04T08:00:00Z"))
	db.UpsertTicketCreated("0", mustTime(t, "2026-09-04T08:00:00Z"))
	if err := db.SetFirstReply("1", mustTime(t, "2026-10-02T09:00:00Z")); err != nil {
		t.Fatal(err)
	}
	db.UpsertTicketCreated("1", mustTime(t, "2026-10-02T08:00:00Z")) // non azzera la risposta
	fs, err := db.TicketsWithoutReply(from, 1)
	if err != nil || len(fs) != 1 || fs[0].TicketID != "3" {
		t.Fatalf("senza risposta, prima i più recenti: %+v %v", fs, err)
	}
	// Chiuso senza risposta: non si rilegge più ogni ora.
	db.SetClosedMonth("2026-10", []string{"3"})
	if fs, _ := db.TicketsWithoutReply(from, 5); len(fs) != 1 || fs[0].TicketID != "2" {
		t.Fatalf("chiusi esclusi: %+v", fs)
	}
	ds, _ := db.FirstReplyDurations(from, mustTime(t, "2026-11-01T00:00:00Z"))
	if len(ds) != 1 || ds[0] != time.Hour {
		t.Fatalf("durate: %v", ds)
	}
}

func TestTicketStatsRoundTrip(t *testing.T) {
	db := newTestDB(t)
	if _, ok, _ := db.GetTicketStats("2026-10"); ok {
		t.Fatal("statistiche inesistenti")
	}
	now := mustTime(t, "2026-10-10T08:00:00Z")
	if err := db.SaveTicketStats(TicketStats{Month: "2026-10", Requests: 3, Resolved: 1, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	st, ok, _ := db.GetTicketStats("2026-10")
	if !ok || st.Requests != 3 || st.Resolved != 1 || st.MedianFirst != nil || !st.UpdatedAt.Equal(now) {
		t.Fatalf("senza mediana: %+v", st)
	}
	d := 2 * time.Hour
	db.SaveTicketStats(TicketStats{Month: "2026-10", Requests: 4, Resolved: 2, MedianFirst: &d, UpdatedAt: now})
	if st, _, _ = db.GetTicketStats("2026-10"); st.Requests != 4 || st.MedianFirst == nil || *st.MedianFirst != d {
		t.Fatalf("con mediana: %+v", st)
	}
}

func TestMissingTicketFacts(t *testing.T) {
	db := newTestDB(t)
	db.UpsertTicketCreated("1", mustTime(t, "2026-10-02T08:00:00Z"))
	db.SetClosedMonth("2026-10", []string{"2"}) // chiuso ma creazione non ancora letta
	got, err := db.MissingTicketFacts([]string{"1", "2", "3"})
	if err != nil || !reflect.DeepEqual(got, []string{"2", "3"}) {
		t.Fatalf("mancanti: %v %v", got, err)
	}
	db.UpsertTicketCreated("2", mustTime(t, "2026-10-05T08:00:00Z"))
	if fs, _ := db.TicketsWithoutReply(mustTime(t, "2026-10-01T00:00:00Z"), 10); len(fs) != 1 || fs[0].TicketID != "1" { // il 2 è chiuso: letto una volta con la creazione, poi basta
		t.Fatalf("dopo la lettura: %+v", fs)
	}
}

// I segnaposto (chiusi, creazione non ancora letta) del periodo restano: il job
// li reinserirebbe a ogni giro. Quelli di mesi oltre la conservazione no.
func TestCleanupKeepsCurrentPlaceholders(t *testing.T) {
	db := newTestDB(t)
	db.SetClosedMonth("2026-10", []string{"9"})
	db.SetClosedMonth("2025-08", []string{"8"})
	if err := db.CleanupStats("2025-09-25", "2026-07-25"); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.ClosedInMonth("2026-10", []string{"9"}); n != 1 {
		t.Fatal("segnaposto del mese cancellato")
	}
	if n, _ := db.ClosedInMonth("2025-08", []string{"8"}); n != 0 {
		t.Fatal("segnaposto vecchio rimasto")
	}
}
