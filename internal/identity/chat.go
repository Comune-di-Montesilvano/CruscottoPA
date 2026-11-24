package identity

import (
	"crypto/sha256"
	"time"

	"github.com/gorilla/securecookie"
)

// Cookie della chat: il browser ha superato il login LDAP per Username.
// Identità VERIFICATA (password di dominio), diversa da User che è solo
// dichiarata. Vale solo insieme a un cookie utente con lo stesso Username.
const (
	ChatCookieName = "cruscotto_chat"
	ChatTTL        = 180 * 24 * time.Hour
	// ChatRenewAfter: il cookie si riscrive (scadenza rinnovata) al massimo una volta al giorno.
	ChatRenewAfter = 24 * time.Hour
)

type ChatSession struct {
	Username        string
	Issued, Expires int64 // Unix
}

type ChatCodec struct {
	sc  *securecookie.SecureCookie
	now func() time.Time
}

func NewChatCodec(secret string) *ChatCodec {
	hashKey := sha256.Sum256([]byte("chat-auth:" + secret))
	encKey := sha256.Sum256([]byte("chat-enc:" + secret))
	sc := securecookie.New(hashKey[:], encKey[:])
	sc.MaxAge(int(ChatTTL.Seconds()))
	return &ChatCodec{sc: sc, now: time.Now}
}

// Encode imposta emissione e scadenza (ChatTTL da adesso).
func (c *ChatCodec) Encode(s ChatSession) (string, error) {
	now := c.now()
	s.Issued = now.Unix()
	s.Expires = now.Add(ChatTTL).Unix()
	return c.sc.Encode(ChatCookieName, s)
}

// Decode: illeggibile, manomesso o scaduto = nessuna sessione.
func (c *ChatCodec) Decode(v string) (ChatSession, bool) {
	var s ChatSession
	if v == "" || c.sc.Decode(ChatCookieName, v, &s) != nil || s.Username == "" || c.now().Unix() > s.Expires {
		return ChatSession{}, false
	}
	return s, true
}

// SetClock sostituisce l'orologio (test e Server con Options.Now).
func (c *ChatCodec) SetClock(now func() time.Time) { c.now = now }
