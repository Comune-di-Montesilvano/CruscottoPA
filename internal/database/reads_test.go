package database

import (
	"testing"
	"time"
)

func TestReadsAndDeliveries(t *testing.T) {
	db := newTestDB(t)
	t0 := time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC)
	id, _ := db.CreateAlert(Alert{Title: "A", Level: LevelNews, StartsAt: t0})
	// Prima lettura vince (due schede insieme).
	db.MarkRead(id, "Mario.Rossi", ReadOpen, t0)
	db.MarkRead(id, "mario.rossi", ReadConfirm, t0.Add(time.Minute))
	db.MarkRead(id, "anna", "boh", t0) // modo non valido: ignorato
	db.MarkRead(id, "", ReadOpen, t0)  // anonimo: ignorato
	rs, _ := db.ReadsFor(id)
	if len(rs) != 1 || rs[0].Username != "mario.rossi" || rs[0].How != ReadOpen || !rs[0].ReadAt.Equal(t0) {
		t.Fatalf("letture: %+v", rs)
	}
	if c, _ := db.ReadCounts(); c[id] != 1 {
		t.Fatalf("conteggio letture: %v", c)
	}

	db.RecordDelivery(id, "mario.rossi", "https://fcm.googleapis.com/a", "Chrome/Edge", DeliverySent, t0)
	db.RecordDelivery(id, "anna", "https://updates.push.services.mozilla.com/b", "Firefox", DeliveryFailed, t0)
	db.MarkReceived(id, "https://fcm.googleapis.com/a", t0.Add(time.Second))
	db.MarkReceived(id, "https://fcm.googleapis.com/a", t0.Add(time.Hour)) // ripetuta: resta la prima
	db.MarkReceived(id, "https://altro.example/x", t0)                     // endpoint sconosciuto: ignorata
	ds, _ := db.DeliveriesFor(id)
	if len(ds) != 2 || ds[1].Username != "mario.rossi" || ds[1].ReceivedAt == nil || !ds[1].ReceivedAt.Equal(t0.Add(time.Second)) || ds[0].Status != DeliveryFailed {
		t.Fatalf("consegne: %+v", ds)
	}
	if c, _ := db.DeliveryCounts(); c[id] != (DeliveryCount{Sent: 1, Received: 1}) {
		t.Fatalf("conteggi consegne: %+v", c)
	}
	if last, _ := db.LastReceivedByUser(); !last["mario.rossi"].Equal(t0.Add(time.Second)) {
		t.Fatalf("ultima ricevuta: %v", last)
	}
	// Reinvio: stato aggiornato, ricevuta azzerata, nessuna riga in più.
	db.RecordDelivery(id, "mario.rossi", "https://fcm.googleapis.com/a", "Chrome/Edge", DeliverySent, t0.Add(2*time.Hour))
	if ds, _ = db.DeliveriesFor(id); len(ds) != 2 || ds[1].ReceivedAt != nil {
		t.Fatalf("reinvio: %+v", ds)
	}
	// Avviso eliminato: tutto sparisce.
	if err := db.DeleteAlert(id); err != nil {
		t.Fatal(err)
	}
	if rs, _ := db.ReadsFor(id); len(rs) != 0 {
		t.Fatalf("letture dopo eliminazione: %+v", rs)
	}
	if ds, _ := db.DeliveriesFor(id); len(ds) != 0 {
		t.Fatalf("consegne dopo eliminazione: %+v", ds)
	}
}
