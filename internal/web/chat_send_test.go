package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/notify"
)

func chatSend(t *testing.T, s *Server, from, to, text string, verified bool) map[string]any {
	t.Helper()
	cs := []*http.Cookie{userCookie(t, s, from, "")}
	if verified {
		cs = append(cs, chatCookie(t, s, from))
	}
	return chatJSON(t, do(t, s, "POST", "/chat/messaggi", url.Values{"a": {to}, "testo": {text}}, nil,
		withCookies(map[string]string{"X-Real-Ip": "192.0.2.15"}, cs...)))
}

func onlyMessage(t *testing.T, db *database.DB, a, b, viewer string) database.ChatMessage {
	t.Helper()
	id, _, err := db.ChatConversationID(a, b, false, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := db.ChatMessages(id, viewer, 0, 50)
	if err != nil || len(ms) != 1 {
		t.Fatalf("messaggi visti da %s: %+v %v", viewer, ms, err)
	}
	return ms[0]
}

func TestChatSendDeclared(t *testing.T) {
	s, db := newChatServer(t, nil)
	if m := chatSend(t, s, "mrossi", "ABianchi", "ciao Anna", false); m["ok"] != true || m["conversazione"] == nil {
		t.Fatalf("invio: %v", m)
	}
	msg := onlyMessage(t, db, "mrossi", "abianchi", "abianchi")
	if msg.Verified || msg.Body != "ciao Anna" || msg.IP != "192.0.2.15" || msg.PC != "PC-PROVA-001" || msg.Sender != "mrossi" {
		t.Fatalf("messaggio: %+v", msg)
	}
}

func TestChatSendVerified(t *testing.T) {
	s, db := newChatServer(t, nil)
	chatSend(t, s, "mrossi", "abianchi", "ciao", true)
	if msg := onlyMessage(t, db, "mrossi", "abianchi", "abianchi"); !msg.Verified {
		t.Fatalf("non verificato: %+v", msg)
	}
}

// Review Focus 2: il bloccato non se ne accorge, il destinatario non vede nulla.
func TestChatSendBlocked(t *testing.T) {
	s, db := newChatServer(t, nil)
	db.SetChatBlock("abianchi", "mrossi", true, fixedNow)
	c, _ := s.hub.Subscribe("abianchi")
	defer s.hub.Unsubscribe(c)
	if m := chatSend(t, s, "mrossi", "abianchi", "ciao", true); m["ok"] != true {
		t.Fatalf("invio: %v", m)
	}
	if msg := onlyMessage(t, db, "mrossi", "abianchi", "mrossi"); !msg.Hidden {
		t.Fatalf("non nascosto: %+v", msg)
	}
	if n, _ := db.ChatUnreadTotal("abianchi"); n != 0 {
		t.Fatalf("non letti: %d", n)
	}
	select {
	case e := <-c.Events:
		t.Fatalf("evento al destinatario: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

// Review Focus 3: pubblico della chat in entrambe le direzioni.
func TestChatSendAudience(t *testing.T) {
	s, db := newChatServer(t, nil)
	g := chatGroup(t, db, "Chat", "mrossi", "abianchi")
	db.SetContentAudience(database.ContentChat, database.ChatAudienceID, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{g}})
	if m := chatSend(t, s, "mrossi", "lverdi", "ciao", false); m["errore"] != "destinatario" {
		t.Fatalf("destinatario fuori: %v", m)
	}
	if m := chatSend(t, s, "lverdi", "mrossi", "ciao", false); m["errore"] != "anonimo" {
		t.Fatalf("mittente fuori: %v", m)
	}
	if m := chatSend(t, s, "mrossi", "abianchi", "ciao", false); m["ok"] != true {
		t.Fatalf("entrambi dentro: %v", m)
	}
}

func TestChatSendADDown(t *testing.T) {
	s, _ := newChatServer(t, nil)
	chatSend(t, s, "mrossi", "abianchi", "riscalda la cache", false)
	d := chatDirectory()
	d.err = errAD
	s2, _ := newChatServer(t, func(o *Options) { o.Directory = d })
	if m := chatSend(t, s2, "mrossi", "abianchi", "ciao", true); m["errore"] != "ad" {
		t.Fatalf("AD giù: %v", m)
	}
}

func TestChatSendValidation(t *testing.T) {
	s, _ := newChatServer(t, nil)
	cases := map[string][2]string{
		"vuoto":        {"abianchi", "   "},
		"lungo":        {"abianchi", strings.Repeat("a", 2001)},
		"destinatario": {"mrossi", "ciao"},
	}
	for want, c := range cases {
		if m := chatSend(t, s, "mrossi", c[0], c[1], false); m["errore"] != want {
			t.Errorf("%s: %v", want, m)
		}
	}
	if m := chatSend(t, s, "mrossi", "sconosciuto", "ciao", false); m["errore"] != "destinatario" {
		t.Errorf("sconosciuto: %v", m)
	}
	if m := chatSend(t, s, "mrossi", "abianchi", strings.Repeat("è", 2000), false); m["ok"] != true {
		t.Errorf("2000 rune: %v", m)
	}
}

func TestChatSendLimits(t *testing.T) {
	s, _ := newChatServer(t, nil)
	for i := 0; i < 30; i++ {
		if m := chatSend(t, s, "mrossi", "abianchi", "m", false); m["ok"] != true {
			t.Fatalf("messaggio %d: %v", i, m)
		}
	}
	if m := chatSend(t, s, "mrossi", "abianchi", "m", false); m["errore"] != "limite" {
		t.Fatalf("31°: %v", m)
	}
	l := newChatLimits()
	for i := 0; i < 50; i++ {
		if !l.allowNewConversation("mrossi", "2026-10-06") {
			t.Fatalf("conversazione %d rifiutata", i)
		}
	}
	if l.allowNewConversation("mrossi", "2026-10-06") {
		t.Fatal("51ª conversazione del giorno accettata")
	}
	if !l.allowNewConversation("mrossi", "2026-10-07") {
		t.Fatal("giorno nuovo rifiutato")
	}
	now := fixedNow
	for i := 0; i < 300; i++ {
		if !l.allowMessage("lverdi", now.Add(time.Duration(i)*10*time.Second)) {
			t.Fatalf("messaggio %d rifiutato", i)
		}
	}
	if l.allowMessage("lverdi", now.Add(3000*time.Second)) {
		t.Fatal("301° nell'ora accettato")
	}
}

func TestChatEventNoText(t *testing.T) {
	s, db := newChatServer(t, nil)
	fp := &fakePusher{}
	s.pusher = fp
	db.SavePushSubscription(database.PushSubscription{Endpoint: "https://fcm.googleapis.com/x", P256dh: "k", Auth: "a", Username: "abianchi", CreatedAt: fixedNow}, 100)
	c, _ := s.hub.Subscribe("abianchi")
	defer s.hub.Unsubscribe(c)
	chatSend(t, s, "mrossi", "abianchi", "testo segreto", false)
	s.chatPush.Wait()
	var e notify.Event
	select {
	case e = <-c.Events:
	case <-time.After(time.Second):
		t.Fatal("nessun evento")
	}
	if e.Kind != "chat" || e.Body != "" || strings.Contains(e.Title, "segreto") || !strings.Contains(e.Title, "Mario Rossi") || !strings.Contains(e.Title, "non verificato") {
		t.Fatalf("evento: %+v", e)
	}
	if len(fp.sent) != 1 || strings.Contains(fp.sent[0], "segreto") || !strings.Contains(fp.sent[0], `"tag":"chat-`) {
		t.Fatalf("push: %v", fp.sent)
	}
}
