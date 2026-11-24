package web

import (
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestCleanupChatRetention(t *testing.T) {
	s, db := newChatServer(t, nil)
	old, _, _ := db.ChatConversationID("mrossi", "abianchi", true, fixedNow.AddDate(0, 0, -31))
	db.AddChatMessage(database.ChatMessage{ConversationID: old, Sender: "mrossi", Body: "vecchio", CreatedAt: fixedNow.AddDate(0, 0, -31)})
	recent, _, _ := db.ChatConversationID("mrossi", "lverdi", true, fixedNow)
	db.AddChatMessage(database.ChatMessage{ConversationID: recent, Sender: "mrossi", Body: "recente", CreatedAt: fixedNow.AddDate(0, 0, -29)})
	s.cleanupChat()
	if ms, err := db.ChatMessages(old, "abianchi", 0, 50); err == nil {
		t.Fatalf("messaggio di 31 giorni rimasto: %+v", ms)
	}
	if ms, _ := db.ChatMessages(recent, "lverdi", 0, 50); len(ms) != 1 {
		t.Fatalf("messaggio di 29 giorni cancellato: %+v", ms)
	}
}

// Anche a chat spenta i messaggi non restano oltre i 30 giorni.
func TestCleanupChatWhenDisabled(t *testing.T) {
	s, db := newChatServer(t, func(o *Options) { o.Config.ChatEnabled = false })
	c, _, _ := db.ChatConversationID("mrossi", "abianchi", true, fixedNow.AddDate(0, 0, -31))
	db.AddChatMessage(database.ChatMessage{ConversationID: c, Sender: "mrossi", Body: "vecchio", CreatedAt: fixedNow.AddDate(0, 0, -31)})
	s.cleanupChat()
	if us, _ := db.ChatUsers(); len(us) != 0 {
		t.Fatalf("dati rimasti: %v", us)
	}
}

func TestCleanupChatUnknownUser(t *testing.T) {
	s, db := newChatServer(t, nil)
	c, _, _ := db.ChatConversationID("mrossi", "uscito", true, fixedNow)
	db.AddChatMessage(database.ChatMessage{ConversationID: c, Sender: "mrossi", Body: "ciao", CreatedAt: fixedNow})
	db.SetChatBlock("abianchi", "uscito", true, fixedNow)
	s.cleanupChat()
	if us, _ := db.ChatUsers(); len(us) != 0 {
		t.Fatalf("utente uscito da AD rimasto: %v", us)
	}

	down := chatDirectory()
	down.err = errAD
	s2, db2 := newChatServer(t, func(o *Options) { o.Directory = down })
	c2, _, _ := db2.ChatConversationID("mrossi", "uscito", true, fixedNow)
	db2.AddChatMessage(database.ChatMessage{ConversationID: c2, Sender: "mrossi", Body: "ciao", CreatedAt: fixedNow})
	s2.cleanupChat()
	if us, _ := db2.ChatUsers(); len(us) != 2 {
		t.Fatalf("con AD giù cancellato: %v", us)
	}
}
