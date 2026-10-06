package identity

import (
	"crypto/sha256"
	"strings"
	"time"

	"github.com/gorilla/securecookie"
)

// User è chi guarda la plancia, come ricavato da NTLM + AD. Identità
// DICHIARATA: solo per personalizzare (vedi il commento del pacchetto).
type User struct {
	Username  string
	Name      string
	Anonymous bool // riconoscimento tentato e non riuscito
}

// FirstName è la prima parola del nome visualizzato ("Mirko D'Addiego" → "Mirko").
func (u User) FirstName() string {
	if f := strings.Fields(u.Name); len(f) > 0 {
		return f[0]
	}
	return ""
}

const (
	CookieName   = "cruscotto_utente"
	UserTTL      = 30 * 24 * time.Hour
	AnonymousTTL = 24 * time.Hour
)

// CookieCodec firma e cifra il cookie dell'utente con chiavi derivate da
// SESSION_SECRET (diverse da quelle della sessione admin).
type CookieCodec struct{ sc *securecookie.SecureCookie }

func NewCookieCodec(secret string) *CookieCodec {
	hashKey := sha256.Sum256([]byte("utente-auth:" + secret))
	encKey := sha256.Sum256([]byte("utente-enc:" + secret))
	sc := securecookie.New(hashKey[:], encKey[:])
	sc.MaxAge(int(UserTTL.Seconds()))
	return &CookieCodec{sc: sc}
}

func (c *CookieCodec) Encode(u User) (string, error) { return c.sc.Encode(CookieName, u) }

// Decode: valore illeggibile, manomesso o scaduto = nessun utente.
func (c *CookieCodec) Decode(v string) (User, bool) {
	var u User
	if v == "" || c.sc.Decode(CookieName, v, &u) != nil {
		return User{}, false
	}
	return u, true
}
