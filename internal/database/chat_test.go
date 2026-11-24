package database

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
)

var chatT0 = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

func chatMsg(t *testing.T, db *DB, conv int64, sender, body string, hidden bool, at time.Time) int64 {
	t.Helper()
	id, err := db.AddChatMessage(ChatMessage{ConversationID: conv, Sender: sender, Body: body, Hidden: hidden, CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func chatConv(t *testing.T, db *DB, a, b string) int64 {
	t.Helper()
	id, _, err := db.ChatConversationID(a, b, true, chatT0)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestChatConversationPairOrdered(t *testing.T) {
	db := newTestDB(t)
	id, created, err := db.ChatConversationID("mrossi", "abianchi", true, chatT0)
	if err != nil || !created {
		t.Fatalf("prima creazione: %d %v %v", id, created, err)
	}
	id2, created2, err := db.ChatConversationID("ABIANCHI", "mrossi", false, chatT0)
	if err != nil || created2 || id2 != id {
		t.Fatalf("stessa coppia: %d %v %v (atteso %d)", id2, created2, err, id)
	}
	if _, _, err := db.ChatConversationID("mrossi", "lverdi", false, chatT0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("assente senza create: %v", err)
	}
	if other, err := db.ChatConversationOther(id, "mrossi"); err != nil || other != "abianchi" {
		t.Fatalf("altro: %q %v", other, err)
	}
	if _, err := db.ChatConversationOther(id, "lverdi"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non partecipante: %v", err)
	}
}

func TestChatHiddenMessages(t *testing.T) {
	db := newTestDB(t)
	c := chatConv(t, db, "mrossi", "abianchi")
	chatMsg(t, db, c, "mrossi", "ciao", true, chatT0)
	mine, err := db.ChatMessages(c, "mrossi", 0, 50)
	if err != nil || len(mine) != 1 || !mine[0].Hidden {
		t.Fatalf("mittente: %+v %v", mine, err)
	}
	theirs, err := db.ChatMessages(c, "abianchi", 0, 50)
	if err != nil || len(theirs) != 0 {
		t.Fatalf("destinatario: %+v %v", theirs, err)
	}
	if n, _ := db.ChatUnreadTotal("abianchi"); n != 0 {
		t.Fatalf("non letti: %d", n)
	}
	if cs, _ := db.ListChatConversations("abianchi", 20); len(cs) != 0 {
		t.Fatalf("conversazioni del destinatario: %+v", cs)
	}
	if cs, _ := db.ListChatConversations("mrossi", 20); len(cs) != 1 || cs[0].Other != "abianchi" {
		t.Fatalf("conversazioni del mittente: %+v", cs)
	}
}

func TestChatUnreadAndRead(t *testing.T) {
	db := newTestDB(t)
	c := chatConv(t, db, "mrossi", "abianchi")
	chatMsg(t, db, c, "mrossi", "uno", false, chatT0)
	chatMsg(t, db, c, "mrossi", "due", false, chatT0.Add(time.Minute))
	if n, _ := db.ChatUnreadTotal("abianchi"); n != 2 {
		t.Fatalf("non letti: %d", n)
	}
	if n, _ := db.ChatUnreadTotal("mrossi"); n != 0 {
		t.Fatalf("non letti del mittente: %d", n)
	}
	cs, _ := db.ListChatConversations("abianchi", 20)
	if len(cs) != 1 || cs[0].Unread != 2 || !cs[0].LastAt.Equal(chatT0.Add(time.Minute)) {
		t.Fatalf("elenco: %+v", cs)
	}
	if err := db.MarkChatRead("abianchi", c); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.ChatUnreadTotal("abianchi"); n != 0 {
		t.Fatalf("dopo la lettura: %d", n)
	}
}

func TestChatMessagesPaging(t *testing.T) {
	db := newTestDB(t)
	c := chatConv(t, db, "mrossi", "abianchi")
	var ids []int64
	for i := 0; i < 60; i++ {
		ids = append(ids, chatMsg(t, db, c, "mrossi", "m", false, chatT0.Add(time.Duration(i)*time.Second)))
	}
	page, err := db.ChatMessages(c, "abianchi", 0, 50)
	if err != nil || len(page) != 50 || page[0].ID != ids[10] || page[49].ID != ids[59] {
		t.Fatalf("prima pagina: %d %v", len(page), err)
	}
	older, err := db.ChatMessages(c, "abianchi", ids[10], 50)
	if err != nil || len(older) != 10 || older[0].ID != ids[0] {
		t.Fatalf("pagina precedente: %d %v", len(older), err)
	}
}

func TestChatNotParticipant(t *testing.T) {
	db := newTestDB(t)
	c := chatConv(t, db, "mrossi", "abianchi")
	if _, err := db.ChatMessages(c, "lverdi", 0, 50); !errors.Is(err, ErrNotFound) {
		t.Fatalf("estraneo: %v", err)
	}
}

func TestChatMessageFields(t *testing.T) {
	db := newTestDB(t)
	c := chatConv(t, db, "mrossi", "abianchi")
	db.AddChatMessage(ChatMessage{ConversationID: c, Sender: "MRossi", Body: "è", Verified: true, IP: "192.0.2.15", PC: "PC-PROVA-001", CreatedAt: chatT0})
	ms, _ := db.ChatMessages(c, "abianchi", 0, 50)
	if len(ms) != 1 || ms[0].Sender != "mrossi" || ms[0].Body != "è" || !ms[0].Verified || ms[0].IP != "192.0.2.15" || ms[0].PC != "PC-PROVA-001" || !ms[0].CreatedAt.Equal(chatT0) {
		t.Fatalf("campi: %+v", ms)
	}
}

func TestChatBlocks(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetChatBlock("abianchi", "MRossi", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	db.SetChatBlock("abianchi", "mrossi", true, time.Now()) // ripetuto: nessun errore
	if b, _ := db.ChatBlocked("abianchi", "mrossi"); !b {
		t.Fatal("blocco assente")
	}
	if b, _ := db.ChatBlocked("mrossi", "abianchi"); b {
		t.Fatal("il blocco non è simmetrico")
	}
	if l, _ := db.ChatBlockedList("abianchi"); !slices.Equal(l, []string{"mrossi"}) {
		t.Fatalf("elenco: %v", l)
	}
	db.SetChatBlock("abianchi", "mrossi", false, time.Now())
	if b, _ := db.ChatBlocked("abianchi", "mrossi"); b {
		t.Fatal("sblocco non riuscito")
	}
}

func TestCleanupChat(t *testing.T) {
	db := newTestDB(t)
	now := chatT0
	// Conversazioni create col primo messaggio, 31 giorni fa.
	old, _, _ := db.ChatConversationID("mrossi", "abianchi", true, now.AddDate(0, 0, -31))
	chatMsg(t, db, old, "mrossi", "vecchio", false, now.AddDate(0, 0, -31))
	live, _, _ := db.ChatConversationID("mrossi", "lverdi", true, now.AddDate(0, 0, -31))
	chatMsg(t, db, live, "mrossi", "vecchio", false, now.AddDate(0, 0, -31))
	chatMsg(t, db, live, "mrossi", "recente", false, now.AddDate(0, 0, -1))
	if err := db.CleanupChat(now.AddDate(0, 0, -30)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ChatConversationOther(old, "mrossi"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("conversazione vuota rimasta: %v", err)
	}
	ms, _ := db.ChatMessages(live, "lverdi", 0, 50)
	if len(ms) != 1 || ms[0].Body != "recente" {
		t.Fatalf("messaggi rimasti: %+v", ms)
	}
	if n, _ := db.ChatUnreadTotal("lverdi"); n != 1 {
		t.Fatalf("non letti dopo la pulizia: %d", n)
	}
	if n, _ := db.ChatUnreadTotal("abianchi"); n != 0 {
		t.Fatalf("non letti di una conversazione cancellata: %d", n)
	}
}

func TestDeleteChatUser(t *testing.T) {
	db := newTestDB(t)
	c := chatConv(t, db, "mrossi", "abianchi")
	chatMsg(t, db, c, "mrossi", "ciao", false, chatT0)
	chatConv(t, db, "abianchi", "lverdi")
	db.SetChatBlock("lverdi", "mrossi", true, time.Now())
	if us, _ := db.ChatUsers(); !slices.Equal(us, []string{"abianchi", "lverdi", "mrossi"}) {
		t.Fatalf("utenti: %v", us)
	}
	if err := db.DeleteChatUser("MRossi"); err != nil {
		t.Fatal(err)
	}
	if us, _ := db.ChatUsers(); !slices.Equal(us, []string{"abianchi", "lverdi"}) {
		t.Fatalf("utenti dopo: %v", us)
	}
	if n, _ := db.ChatUnreadTotal("abianchi"); n != 0 {
		t.Fatalf("non letti rimasti: %d", n)
	}
}

func TestGroupUsedByChat(t *testing.T) {
	db := newTestDB(t)
	g, _ := db.CreateAudienceGroup("CED")
	if err := db.SetContentAudience(ContentChat, ChatAudienceID, ContentAudience{Mode: audience.ModeOnly, Groups: []int64{g}}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteAudienceGroup(g); !errors.Is(err, ErrInUse) {
		t.Fatalf("eliminazione: %v", err)
	}
	if uses, _ := db.GroupUses(g); !slices.Contains(uses, "Chat") {
		t.Fatalf("usi: %v", uses)
	}
}

func TestChatStats(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	c1, _, _ := db.ChatConversationID("mrossi", "abianchi", true, now)
	c2, _, _ := db.ChatConversationID("mrossi", "lverdi", true, now)
	c3, _, _ := db.ChatConversationID("abianchi", "lverdi", true, now)
	add := func(c int64, from string, daysAgo int, verified bool) {
		if _, err := db.AddChatMessage(ChatMessage{ConversationID: c, Sender: from, Body: "x", Verified: verified, CreatedAt: now.AddDate(0, 0, -daysAgo)}); err != nil {
			t.Fatal(err)
		}
	}
	add(c1, "mrossi", 1, true)
	add(c1, "abianchi", 2, false)
	add(c2, "mrossi", 10, true)
	add(c3, "lverdi", 31, true) // fuori dai 30 giorni
	db.SetChatBlock("abianchi", "lverdi", true, time.Now())
	st, err := db.ChatStats(now)
	if err != nil {
		t.Fatal(err)
	}
	want := ChatStats{Messages7: 2, Messages30: 3, Senders7: 2, Senders30: 2, Conversations30: 2, Unverified30: 1, Blocks: 1}
	if st != want {
		t.Fatalf("statistiche: %+v, attese %+v", st, want)
	}
}

func TestChatBlockUsesClock(t *testing.T) {
	db := newTestDB(t)
	t0 := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := db.SetChatBlock("abianchi", "mrossi", true, t0); err != nil {
		t.Fatal(err)
	}
	var at string
	db.QueryRow(`SELECT created_at FROM chat_blocks`).Scan(&at)
	if at != formatTime(t0) {
		t.Fatalf("created_at: %s", at)
	}
}

// Conversazioni rimaste vuote (creazione senza messaggio) tolte dalla pulizia
// dopo 30 giorni, anche se non scade nessun messaggio.
func TestCleanupChatEmptyConversations(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	old, _, _ := db.ChatConversationID("mrossi", "abianchi", true, now.AddDate(0, 0, -31))
	fresh, _, _ := db.ChatConversationID("mrossi", "lverdi", true, now)
	if err := db.CleanupChat(now.AddDate(0, 0, -30)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ChatConversationOther(old, "mrossi"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("conversazione vuota di 31 giorni rimasta: %v", err)
	}
	if _, err := db.ChatConversationOther(fresh, "mrossi"); err != nil {
		t.Fatalf("conversazione appena creata cancellata: %v", err)
	}
}
