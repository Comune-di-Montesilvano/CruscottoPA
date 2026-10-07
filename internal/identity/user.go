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
	GivenName string
	Anonymous bool  // riconoscimento tentato e non riuscito
	Expires   int64 // scadenza (Unix), verificata anche lato server
}

// FirstName è il nome di battesimo: givenName se c'è, altrimenti la prima
// parola del nome visualizzato ("Mirko D'Addiego" → "Mirko").
func (u User) FirstName() string {
	if u.GivenName != "" {
		return u.GivenName
	}
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
type CookieCodec struct {
	sc  *securecookie.SecureCookie
	now func() time.Time
}

func NewCookieCodec(secret string) *CookieCodec {
	hashKey := sha256.Sum256([]byte("utente-auth:" + secret))
	encKey := sha256.Sum256([]byte("utente-enc:" + secret))
	sc := securecookie.New(hashKey[:], encKey[:])
	sc.MaxAge(int(UserTTL.Seconds()))
	return &CookieCodec{sc: sc, now: time.Now}
}

// Encode imposta la scadenza: 30 giorni se riconosciuto, 24 ore se anonimo.
func (c *CookieCodec) Encode(u User) (string, error) {
	ttl := UserTTL
	if u.Anonymous {
		ttl = AnonymousTTL
	}
	u.Expires = c.now().Add(ttl).Unix()
	return c.sc.Encode(CookieName, u)
}

// Decode: valore illeggibile, manomesso o scaduto = nessun utente.
func (c *CookieCodec) Decode(v string) (User, bool) {
	var u User
	if v == "" || c.sc.Decode(CookieName, v, &u) != nil || (u.Expires > 0 && c.now().Unix() > u.Expires) {
		return User{}, false
	}
	u.Expires = 0
	return u, true
}
