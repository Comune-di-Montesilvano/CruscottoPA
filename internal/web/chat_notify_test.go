package web

import (
	"os"
	"strings"
	"testing"
)

func TestChatNotifyState(t *testing.T) {
	s, db := newChatServer(t, nil)
	chatSend(t, s, "mrossi", "abianchi", "ciao", false)
	if m, _ := chatGet(t, s, "/chat/con/abianchi", "mrossi", true); m["notifiche"] != "no" {
		t.Fatalf("senza iscrizioni: %v", m["notifiche"])
	}
	if _, err := db.Exec(`INSERT INTO push_subscriptions (endpoint, p256dh, auth, username, created_at) VALUES ('https://push.example.it/1', 'k', 'a', 'abianchi', '2026-11-24T08:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if m, _ := chatGet(t, s, "/chat/con/abianchi", "mrossi", true); m["notifiche"] != "si" {
		t.Fatalf("con iscrizione: %v", m["notifiche"])
	}
	// Stato nascosto dal collega o da chi guarda: niente.
	db.SetChatHideStatus("abianchi", true)
	if m, _ := chatGet(t, s, "/chat/con/abianchi", "mrossi", true); m["notifiche"] != "" {
		t.Fatalf("stato nascosto: %v", m["notifiche"])
	}
	db.SetChatHideStatus("abianchi", false)
	db.SetChatHideStatus("mrossi", true)
	if m, _ := chatGet(t, s, "/chat/con/abianchi", "mrossi", true); m["notifiche"] != "" {
		t.Fatalf("chi guarda ha nascosto il suo: %v", m["notifiche"])
	}
}

func TestChatToastAndNotifyAsk(t *testing.T) {
	s, _ := newChatServer(t, nil)
	chatSend(t, s, "abianchi", "mrossi", "ciao", false)
	body := do(t, s, "GET", "/", nil, nil, chatAs(t, s, "mrossi", false)).Body.String()
	for _, want := range []string{
		`data-chat-toast data-unread="1"`,
		"Hai 1 messaggio nuovo nella chat",
		"serve la password di Windows",
		`data-chat-notify-ask hidden`,
		`data-chat-notify>Attiva</button>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("plancia senza %q", want)
		}
	}
	// Senza messaggi nuovi niente popup.
	if body := do(t, s, "GET", "/", nil, nil, chatAs(t, s, "lverdi", true)).Body.String(); strings.Contains(body, "data-chat-toast") {
		t.Fatal("popup senza messaggi nuovi")
	}
	js, _ := os.ReadFile("../../web/static/js/chat.js")
	if src := string(js); !strings.Contains(src, "cruscotto-chat-popup") || !strings.Contains(src, "notifiche-cambiate") {
		t.Fatal("chat.js senza popup giornaliero o invito alle notifiche")
	}
	nj, _ := os.ReadFile("../../web/static/js/notifiche.js")
	if !strings.Contains(string(nj), "[data-chat-notify]") {
		t.Fatal("notifiche.js non gestisce l'invito della chat")
	}
}
