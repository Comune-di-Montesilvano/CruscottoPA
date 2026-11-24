package web

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// Il collega si indica con lo username (sAMAccountName), mai con l'UPN:
// mrossi@dominio sarebbe un'altra conversazione che il vero mrossi non vede.
func TestChatRecipientUPN(t *testing.T) {
	s, _ := newChatServer(t, nil)
	if m := chatSend(t, s, "mrossi", "abianchi@example.it", "ciao", true); m["errore"] != "destinatario" {
		t.Fatalf("invio a UPN: %v", m)
	}
	if m, _ := chatGet(t, s, "/chat/con/abianchi@example.it", "mrossi", true); m["errore"] != "destinatario" {
		t.Fatalf("conversazione con UPN: %v", m)
	}
	m := chatJSON(t, do(t, s, "POST", "/chat/blocchi", url.Values{"utente": {"abianchi@example.it"}, "blocca": {"1"}}, nil, chatAs(t, s, "mrossi", true)))
	if m["errore"] != "destinatario" {
		t.Fatalf("blocco di un UPN: %v", m)
	}
}

// Il limite per minuto scatta prima di creare la conversazione: niente
// conversazioni vuote né quota giornaliera consumata.
func TestChatLimitBeforeNewConversation(t *testing.T) {
	s, db := newChatServer(t, nil)
	for i := 0; i < 30; i++ {
		chatSend(t, s, "mrossi", "abianchi", "m", false)
	}
	if m := chatSend(t, s, "mrossi", "lverdi", "ciao", false); m["errore"] != "limite" {
		t.Fatalf("31° messaggio: %v", m)
	}
	if _, _, err := db.ChatConversationID("mrossi", "lverdi", false, fixedNow); !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("conversazione creata nonostante il limite: %v", err)
	}
}

// Corpo oltre 16 KB rifiutato prima di leggerlo tutto.
func TestChatSendBodyLimit(t *testing.T) {
	s, _ := newChatServer(t, nil)
	form := url.Values{"a": {"abianchi"}, "testo": {"ciao"}, "x": {strings.Repeat("a", 20<<10)}}
	m := chatJSON(t, do(t, s, "POST", "/chat/messaggi", form, nil, chatAs(t, s, "mrossi", false)))
	if m["errore"] != "lungo" {
		t.Fatalf("corpo da 20 KB: %v", m)
	}
}

type slowPusher struct {
	mu   sync.Mutex
	sent int
}

func (p *slowPusher) Send(context.Context, database.PushSubscription, []byte, bool) (bool, error) {
	time.Sleep(200 * time.Millisecond)
	p.mu.Lock()
	p.sent++
	p.mu.Unlock()
	return false, nil
}

// Allo spegnimento Close aspetta le push della chat (DB ancora aperto).
func TestChatPushWaitOnClose(t *testing.T) {
	s, db := newChatServer(t, nil)
	sp := &slowPusher{}
	s.pusher = sp
	db.SavePushSubscription(database.PushSubscription{Endpoint: "https://fcm.googleapis.com/x", P256dh: "k", Auth: "a", Username: "abianchi", CreatedAt: fixedNow}, 100)
	chatSend(t, s, "mrossi", "abianchi", "ciao", false)
	s.Close()
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.sent != 1 {
		t.Fatalf("push al termine di Close: %d", sp.sent)
	}
}

// Al più chatPushMax invii della chat in corso insieme.
func TestChatPushBounded(t *testing.T) {
	s, _ := newChatServer(t, nil)
	n := 0
	for i := 0; i < chatPushMax+5; i++ {
		if s.chatPushSlot() {
			n++
		}
	}
	if n != chatPushMax {
		t.Fatalf("posti di invio: %d, attesi %d", n, chatPushMax)
	}
}
