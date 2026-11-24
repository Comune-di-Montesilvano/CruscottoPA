package identity

import (
	"testing"
	"time"
)

func TestChatCodecRoundTrip(t *testing.T) {
	now := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	c := NewChatCodec("segreto-di-prova-lungo-almeno-32-caratteri")
	c.now = func() time.Time { return now }
	v, err := c.Encode(ChatSession{Username: "mrossi"})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := c.Decode(v)
	if !ok || s.Username != "mrossi" || s.Issued != now.Unix() || s.Expires != now.Add(ChatTTL).Unix() {
		t.Fatalf("decodifica: %+v %v", s, ok)
	}
}

func TestChatCodecExpired(t *testing.T) {
	now := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	c := NewChatCodec("segreto-di-prova-lungo-almeno-32-caratteri")
	c.now = func() time.Time { return now }
	v, _ := c.Encode(ChatSession{Username: "mrossi"})
	now = now.Add(179 * 24 * time.Hour)
	if _, ok := c.Decode(v); !ok {
		t.Fatal("scaduto troppo presto")
	}
	now = now.Add(2 * 24 * time.Hour)
	if _, ok := c.Decode(v); ok {
		t.Fatal("valido oltre 180 giorni")
	}
}

// Il cookie dell'utente (identità dichiarata) non vale come cookie della chat.
func TestChatCodecNotUserCookie(t *testing.T) {
	secret := "segreto-di-prova-lungo-almeno-32-caratteri"
	v, _ := NewCookieCodec(secret).Encode(User{Username: "mrossi"})
	if _, ok := NewChatCodec(secret).Decode(v); ok {
		t.Fatal("cookie utente accettato come cookie chat")
	}
	if _, ok := NewChatCodec(secret).Decode(""); ok {
		t.Fatal("valore vuoto accettato")
	}
}
