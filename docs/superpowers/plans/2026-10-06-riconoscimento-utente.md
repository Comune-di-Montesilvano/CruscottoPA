# Riconoscimento utente e filtri per ufficio — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** riconoscere via NTLM (identità dichiarata) chi apre la plancia da un PC del dominio, salutarlo per nome e mostrargli i contenuti per tutti più quelli del suo ufficio (da AD), con "Mostra tutto"; uffici assegnabili da admin.

**Architecture:** nuovo pacchetto `internal/identity` (parser NTLM, cookie firmato dell'utente, regola di pertinenza, directory LDAP/mock). Endpoint `/io` chiamato in background da `dashboard.js` fa l'handshake e salva il cookie. Il server filtra plancia/avvisi in base al cookie e al cookie di preferenza `cruscotto_tutto`. Tabelle `*_offices` (migrazione v5) collegano app, guide e avvisi agli uffici.

**Tech Stack:** Go 1.26 (`net/http`, `html/template`), `github.com/go-ldap/ldap/v3`, `github.com/gorilla/securecookie`, SQLite `modernc.org/sqlite`, JS senza dipendenze.

**Spec:** `docs/superpowers/specs/2026-10-06-riconoscimento-utente-design.md`

## Global Constraints

- **Identità dichiarata, mai usata per autorizzare**: il cookie utente non dà accesso a `/admin` né ad alcuna azione. Scriverlo nei commenti di `identity` e di `/io`.
- Ufficio = `physicalDeliveryOfficeName` di AD; confronto senza maiuscole/minuscole e spazi ai bordi.
- `NTLM_DOMAIN` vuota = riconoscimento disattivato; attivo solo se c'è anche una directory (`LDAP_BIND_DN` impostato oppure `LDAP_HOST=mock`).
- Cookie `cruscotto_utente`: `Path=/`, `HttpOnly`, `SameSite=Lax`, `Secure` come il cookie admin; 30 giorni se riconosciuto, 24 ore se anonimo.
- Cookie di preferenza `cruscotto_tutto=1`: `Path=/`, 1 anno, `SameSite=Lax`, non `HttpOnly`.
- `/io`: `Cache-Control: no-store`; 404 con riconoscimento disattivato.
- Migrazioni: la v5 va in coda, mai modificare le precedenti.
- Nuova env var in tre posti: `docker-compose.yml`, `.env.example`, codice.
- CSP invariata: niente script/stili inline, niente `on*=`.
- Testi in italiano. `go test ./...`, `go vet ./...`, `gofmt -l internal/` vuoto.

## Review Focus

- Messaggio NTLM ostile (offset/lunghezze oltre il buffer, overflow di `offset+len`, lunghezza dispari): mai panic, risposta 400 → fuzz + casi in Task 2.
- Cookie utente valido ma richiesta a `/admin`: deve restare 303 al login → test in Task 6.
- Utente riconosciuto senza ufficio in AD: vede solo i contenuti per tutti, nessun errore → test in Task 7.
- Avviso urgente di un altro ufficio con "Mostra tutto" attivo: compare nel carosello ma **senza** popup → test in Task 7.
- Ufficio assegnato a un elemento e poi sparito da AD: resta modificabile in admin ("non più in AD") e il salvataggio non lo rifiuta → test in Task 8.

## Prima di iniziare

Branch `feat/riconoscimento-utente` da `spec/riconoscimento-utente`.

---

### Task 1: configurazione `NTLM_DOMAIN` e limitatore solo per username

**Files:**
- Modify: `internal/config/config.go`, `internal/config/config_test.go`
- Modify: `docker-compose.yml`, `.env.example`
- Modify: `internal/web/admin_auth.go`, `internal/web/admin_auth_test.go`

**Interfaces:**
- Produces: `config.Config.NTLMDomain string`.

- [ ] **Step 1: test che falliscono**

In `internal/config/config_test.go` (seguire lo stile dei test esistenti che impostano le env con `t.Setenv` e chiamano `Load()`):

```go
func TestNTLMDomain(t *testing.T) {
	t.Setenv("LDAP_HOST", "mock")
	t.Setenv("NTLM_DOMAIN", " COMUNE-MS ")
	cfg, err := Load()
	if err != nil || cfg.NTLMDomain != "COMUNE-MS" {
		t.Fatalf("NTLMDomain = %q (%v)", cfg.NTLMDomain, err)
	}
}
```

In `internal/web/admin_auth_test.go`:

```go
// Dietro Podman rootless tutte le richieste hanno lo stesso IP: i tentativi
// sbagliati di un utente non devono bloccare gli altri.
func TestLoginRateLimitIsPerUser(t *testing.T) {
	s, _ := newTestServer(t, fakeAuth{ok: false})
	bad := url.Values{"username": {"mrossi"}, "password": {"sbagliata"}}
	for i := 0; i < 5; i++ {
		do(t, s, "POST", "/admin/login", bad, nil, nil)
	}
	other := url.Values{"username": {"bianchi"}, "password": {"sbagliata"}}
	if rec := do(t, s, "POST", "/admin/login", other, nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("un altro utente non deve essere bloccato: %d", rec.Code)
	}
}
```

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/config/ ./internal/web/ -run 'NTLMDomain|RateLimitIsPerUser'`
Expected: `cfg.NTLMDomain undefined`; dopo averlo aggiunto, `TestLoginRateLimitIsPerUser` FAIL con 429.

- [ ] **Step 3: implementazione**

`internal/config/config.go`: campo in `Config` dopo `LDAP`:

```go
	// NTLMDomain: dominio NetBIOS accettato da /io (vuoto = riconoscimento spento).
	NTLMDomain string
```

e in `Load`, nel letterale: `NTLMDomain: strings.TrimSpace(os.Getenv("NTLM_DOMAIN")),` (aggiungere `strings` agli import se manca).

`internal/web/admin_auth.go`: togliere `clientIP` (e l'import `net` se resta inutilizzato) e sostituire il blocco delle chiavi con:

```go
	// Solo per username: dietro Podman rootless RemoteAddr è uguale per tutti e
	// una chiave per IP bloccherebbe ogni admin. Restano i blocchi account di AD.
	keys := []string{"u:" + strings.ToLower(user)}
```

`docker-compose.yml`, nella lista `environment:` vicino alle variabili LDAP: `NTLM_DOMAIN: ${NTLM_DOMAIN}`. Seguire esattamente la forma delle righe vicine (mappa o lista).

`.env.example`, dopo il blocco LDAP:

```
# ── Riconoscimento utente in plancia (NTLM) ────────────────────────────────
# Dominio NetBIOS dei PC (es. COMUNE-MS). Vuoto = riconoscimento spento.
# Richiede LDAP_BIND_DN: l'ufficio si legge da AD (physicalDeliveryOfficeName).
# L'utente è DICHIARATO dal browser, non verificato: serve solo a personalizzare.
NTLM_DOMAIN=
```

- [ ] **Step 4: verifica**

Run: `go test ./internal/config/ ./internal/web/ && go vet ./...`
Expected: PASS (incluso il vecchio `TestLoginRateLimited`).

- [ ] **Step 5: commit**

```bash
git add internal/config docker-compose.yml .env.example internal/web/admin_auth.go internal/web/admin_auth_test.go
git commit -m "feat: NTLM_DOMAIN e limitatore di login solo per username"
```

---

### Task 2: parser e sfida NTLM

**Files:**
- Create: `internal/identity/ntlm.go`, `internal/identity/ntlm_test.go`
- Create: `internal/identity/ntlmtest/ntlmtest.go`

**Interfaces:**
- Produces:
  - `identity.Login{Domain, User, Workstation string}`
  - `identity.MessageType(msg []byte) int` (1, 2, 3; 0 = non NTLM o troppo corto)
  - `identity.Challenge() []byte`
  - `identity.ParseAuthenticate(msg []byte) (Login, error)`; errore `identity.ErrNTLM`
  - `ntlmtest.Negotiate() []byte`, `ntlmtest.Authenticate(domain, user, workstation string) []byte` (solo per i test)

- [ ] **Step 1: helper di test**

`internal/identity/ntlmtest/ntlmtest.go`:

```go
// Package ntlmtest costruisce messaggi NTLM per i test di identity e web.
package ntlmtest

import (
	"bytes"
	"encoding/binary"
	"unicode/utf16"
)

var sig = []byte("NTLMSSP\x00")

// Negotiate: messaggio di tipo 1 minimale.
func Negotiate() []byte {
	m := make([]byte, 32)
	copy(m, sig)
	binary.LittleEndian.PutUint32(m[8:], 1)
	binary.LittleEndian.PutUint32(m[12:], 0x00088207)
	return m
}

// Authenticate: messaggio di tipo 3 con dominio, utente e postazione in
// UTF-16LE; le risposte LM/NT sono vuote (il server non le verifica).
func Authenticate(domain, user, workstation string) []byte {
	const header = 72
	fields := [][]byte{nil, nil, u16(domain), u16(user), u16(workstation), nil}
	m := make([]byte, header)
	copy(m, sig)
	binary.LittleEndian.PutUint32(m[8:], 3)
	var payload bytes.Buffer
	for i, f := range fields { // Lm, Nt, Domain, User, Workstation, SessionKey
		at := 12 + 8*i
		binary.LittleEndian.PutUint16(m[at:], uint16(len(f)))
		binary.LittleEndian.PutUint16(m[at+2:], uint16(len(f)))
		binary.LittleEndian.PutUint32(m[at+4:], uint32(header+payload.Len()))
		payload.Write(f)
	}
	return append(m, payload.Bytes()...)
}

func u16(s string) []byte {
	var b bytes.Buffer
	for _, c := range utf16.Encode([]rune(s)) {
		binary.Write(&b, binary.LittleEndian, c)
	}
	return b.Bytes()
}
```

- [ ] **Step 2: test che falliscono**

`internal/identity/ntlm_test.go`:

```go
package identity

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity/ntlmtest"
)

func TestMessageType(t *testing.T) {
	if MessageType(ntlmtest.Negotiate()) != 1 || MessageType(ntlmtest.Authenticate("D", "u", "w")) != 3 || MessageType(Challenge()) != 2 {
		t.Fatal("tipi NTLM non riconosciuti")
	}
	for _, bad := range [][]byte{nil, []byte("NTLMSSP"), []byte("XXXXXXXX\x01\x00\x00\x00")} {
		if MessageType(bad) != 0 {
			t.Errorf("MessageType(%q) != 0", bad)
		}
	}
}

func TestChallengeIsRandom(t *testing.T) {
	a, b := Challenge(), Challenge()
	if bytes.Equal(a[24:32], b[24:32]) {
		t.Fatal("la sfida deve cambiare a ogni chiamata")
	}
}

func TestParseAuthenticate(t *testing.T) {
	got, err := ParseAuthenticate(ntlmtest.Authenticate("COMUNE-MS", "mirko.daddiego", "PC-LIV02-024"))
	want := Login{Domain: "COMUNE-MS", User: "mirko.daddiego", Workstation: "PC-LIV02-024"}
	if err != nil || got != want {
		t.Fatalf("ParseAuthenticate = %+v, %v", got, err)
	}
}

func TestParseAuthenticateRejectsHostile(t *testing.T) {
	good := ntlmtest.Authenticate("D", "utente", "W")
	setField := func(at int, length uint16, off uint32) []byte {
		m := append([]byte{}, good...)
		binary.LittleEndian.PutUint16(m[at:], length)
		binary.LittleEndian.PutUint32(m[at+4:], off)
		return m
	}
	cases := map[string][]byte{
		"vuoto":            nil,
		"troncato":         good[:40],
		"tipo 1":           ntlmtest.Negotiate(),
		"offset oltre":     setField(36, 4, uint32(len(good))),
		"offset enorme":    setField(36, 4, 0xFFFFFFF0),
		"lunghezza dispari": setField(36, 3, 72),
		"utente vuoto":     ntlmtest.Authenticate("D", "", "W"),
	}
	for name, msg := range cases {
		if _, err := ParseAuthenticate(msg); !errors.Is(err, ErrNTLM) {
			t.Errorf("%s: atteso ErrNTLM, ottenuto %v", name, err)
		}
	}
}

func FuzzParseAuthenticate(f *testing.F) {
	f.Add(ntlmtest.Authenticate("D", "u", "w"))
	f.Add(ntlmtest.Negotiate())
	f.Fuzz(func(t *testing.T, msg []byte) {
		ParseAuthenticate(msg) // non deve mai andare in panic
		MessageType(msg)
	})
}
```

- [ ] **Step 3: verifica che falliscano**

Run: `go test ./internal/identity/`
Expected: errori di compilazione (`MessageType undefined`…).

- [ ] **Step 4: implementazione**

`internal/identity/ntlm.go`:

```go
// Package identity riconosce chi usa la plancia.
//
// ATTENZIONE: l'identità ricavata da NTLM è DICHIARATA dal browser e non è
// verificata (servirebbe NETLOGON verso il domain controller). Chiunque in rete
// può fabbricare un messaggio con il nome di un collega. Va usata solo per
// personalizzare la vista, MAI per autorizzare azioni o dare accesso all'admin.
package identity

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"unicode/utf16"
)

var (
	ErrNTLM = errors.New("identity: messaggio NTLM non valido")
	ntlmSig = []byte("NTLMSSP\x00")
)

// Login è quanto il browser dichiara nel messaggio NTLM di tipo 3.
type Login struct {
	Domain, User, Workstation string
}

// MessageType restituisce 1, 2 o 3; 0 se msg non è un messaggio NTLM.
func MessageType(msg []byte) int {
	if len(msg) < 12 || !bytes.HasPrefix(msg, ntlmSig) {
		return 0
	}
	switch t := binary.LittleEndian.Uint32(msg[8:12]); t {
	case 1, 2, 3:
		return int(t)
	}
	return 0
}

// Challenge costruisce un messaggio di tipo 2 minimale con sfida casuale.
func Challenge() []byte {
	name := utf16le("CRUSCOTTO")
	var info bytes.Buffer
	for _, avID := range []uint16{2, 1} { // MsvAvNbDomainName, MsvAvNbComputerName
		binary.Write(&info, binary.LittleEndian, avID)
		binary.Write(&info, binary.LittleEndian, uint16(len(name)))
		info.Write(name)
	}
	info.Write([]byte{0, 0, 0, 0}) // MsvAvEOL

	const header = 48
	msg := make([]byte, header)
	copy(msg, ntlmSig)
	binary.LittleEndian.PutUint32(msg[8:], 2)
	binary.LittleEndian.PutUint16(msg[12:], uint16(len(name))) // TargetName
	binary.LittleEndian.PutUint16(msg[14:], uint16(len(name)))
	binary.LittleEndian.PutUint32(msg[16:], header)
	// UNICODE | REQUEST_TARGET | NTLM | ALWAYS_SIGN | TARGET_TYPE_DOMAIN | EXTENDED_SESSIONSECURITY | TARGET_INFO
	binary.LittleEndian.PutUint32(msg[20:], 0x00000001|0x00000004|0x00000200|0x00008000|0x00010000|0x00080000|0x00800000)
	rand.Read(msg[24:32])
	binary.LittleEndian.PutUint16(msg[40:], uint16(info.Len())) // TargetInfo
	binary.LittleEndian.PutUint16(msg[42:], uint16(info.Len()))
	binary.LittleEndian.PutUint32(msg[44:], uint32(header+len(name)))
	msg = append(msg, name...)
	return append(msg, info.Bytes()...)
}

// ParseAuthenticate legge dominio, utente e postazione da un messaggio di
// tipo 3. NON verifica la risposta: vedi il commento del pacchetto.
func ParseAuthenticate(msg []byte) (Login, error) {
	if MessageType(msg) != 3 || len(msg) < 52 {
		return Login{}, ErrNTLM
	}
	var l Login
	var err error
	if l.Domain, err = field(msg, 28); err != nil {
		return Login{}, err
	}
	if l.User, err = field(msg, 36); err != nil || l.User == "" {
		return Login{}, ErrNTLM
	}
	if l.Workstation, err = field(msg, 44); err != nil {
		return Login{}, err
	}
	return l, nil
}

// field legge un campo UTF-16LE descritto da (lunghezza, max, offset) in at.
func field(msg []byte, at int) (string, error) {
	n := uint64(binary.LittleEndian.Uint16(msg[at:]))
	off := uint64(binary.LittleEndian.Uint32(msg[at+4:]))
	if n%2 != 0 || off+n > uint64(len(msg)) {
		return "", ErrNTLM
	}
	u := make([]uint16, n/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(msg[off+uint64(2*i):])
	}
	return string(utf16.Decode(u)), nil
}

func utf16le(s string) []byte {
	var b bytes.Buffer
	for _, c := range utf16.Encode([]rune(s)) {
		binary.Write(&b, binary.LittleEndian, c)
	}
	return b.Bytes()
}
```

- [ ] **Step 5: verifica**

Run: `go test ./internal/identity/ && go test ./internal/identity/ -run '^$' -fuzz FuzzParseAuthenticate -fuzztime 20s && go vet ./...`
Expected: PASS, fuzz senza crash. Se il fuzz trova un caso, aggiungere il file generato in `testdata/fuzz` e correggere `field`.

- [ ] **Step 6: commit**

```bash
git add internal/identity
git commit -m "feat(identity): parser e sfida NTLM (identità dichiarata)"
```

---

### Task 3: utente, cookie firmato e regola di pertinenza

**Files:**
- Create: `internal/identity/user.go`, `internal/identity/user_test.go`
- Modify: `go.mod`, `go.sum` (`gorilla/securecookie` da indiretta a diretta)

**Interfaces:**
- Produces:
  - `identity.User{Username, Name, Office string; Anonymous bool}`; `func (u User) FirstName() string`
  - `const identity.CookieName = "cruscotto_utente"`; `identity.UserTTL = 30*24*time.Hour`; `identity.AnonymousTTL = 24*time.Hour`
  - `identity.NewCookieCodec(secret string) *CookieCodec`; `(*CookieCodec).Encode(User) (string, error)`; `(*CookieCodec).Decode(string) (User, bool)`
  - `identity.Concerns(userOffice string, offices []string) bool`

- [ ] **Step 1: test che falliscono**

`internal/identity/user_test.go`:

```go
package identity

import (
	"strings"
	"testing"
)

func TestCookieRoundTrip(t *testing.T) {
	c := NewCookieCodec(strings.Repeat("s", 32))
	u := User{Username: "mrossi", Name: "Mario Rossi", Office: "TRIBUTI"}
	v, err := c.Encode(u)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := c.Decode(v); !ok || got != u {
		t.Fatalf("Decode = %+v, %v", got, ok)
	}
	if _, ok := c.Decode(v[:len(v)-2] + "xx"); ok {
		t.Fatal("cookie manomesso accettato")
	}
	if _, ok := NewCookieCodec(strings.Repeat("t", 32)).Decode(v); ok {
		t.Fatal("cookie firmato con un altro segreto accettato")
	}
	if _, ok := c.Decode(""); ok {
		t.Fatal("cookie vuoto accettato")
	}
}

func TestFirstName(t *testing.T) {
	for in, want := range map[string]string{"Mirko D'Addiego": "Mirko", "  Anna  Maria Bianchi": "Anna", "": ""} {
		if got := (User{Name: in}).FirstName(); got != want {
			t.Errorf("FirstName(%q) = %q, atteso %q", in, got, want)
		}
	}
}

func TestConcerns(t *testing.T) {
	cases := []struct {
		office  string
		offices []string
		want    bool
	}{
		{"TRIBUTI", nil, true},                         // per tutti
		{"", nil, true},                                // anonimo, per tutti
		{"TRIBUTI", []string{"TRIBUTI"}, true},
		{" tributi ", []string{"TRIBUTI", "LLPP"}, true}, // maiuscole e spazi
		{"LLPP", []string{"TRIBUTI"}, false},
		{"", []string{"TRIBUTI"}, false},               // anonimo o senza ufficio
	}
	for _, c := range cases {
		if got := Concerns(c.office, c.offices); got != c.want {
			t.Errorf("Concerns(%q, %v) = %v", c.office, c.offices, got)
		}
	}
}
```

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/identity/ -run 'Cookie|FirstName|Concerns'`
Expected: errori di compilazione.

- [ ] **Step 3: implementazione**

`internal/identity/user.go`:

```go
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
	Office    string
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

// Concerns dice se un contenuto assegnato a offices riguarda chi è in
// userOffice. Nessun ufficio = per tutti.
func Concerns(userOffice string, offices []string) bool {
	if len(offices) == 0 {
		return true
	}
	userOffice = strings.TrimSpace(userOffice)
	if userOffice == "" {
		return false
	}
	for _, o := range offices {
		if strings.EqualFold(strings.TrimSpace(o), userOffice) {
			return true
		}
	}
	return false
}
```

Poi `go mod tidy` (securecookie diventa diretta).

- [ ] **Step 4: verifica**

Run: `go test ./internal/identity/ && go vet ./... && git diff --stat go.mod`
Expected: PASS; in `go.mod` `gorilla/securecookie` senza `// indirect`.

- [ ] **Step 5: commit**

```bash
git add internal/identity go.mod go.sum
git commit -m "feat(identity): cookie firmato dell'utente e regola di pertinenza per ufficio"
```

---

### Task 4: directory LDAP e mock

**Files:**
- Modify: `internal/auth/ldap.go` (esportare `Dial`)
- Create: `internal/identity/directory.go`, `internal/identity/directory_test.go`

**Interfaces:**
- Consumes: `config.LDAP`, `auth.Dial`.
- Produces:
  - `auth.Dial(cfg config.LDAP) (*ldap.Conn, error)` (ex metodo `dial`)
  - `identity.Person{Username, Name, Office string}`
  - `identity.Directory` interface: `Lookup(username string) (Person, error)`, `Offices() ([]string, error)`
  - `identity.ErrUnknownUser`
  - `identity.NewLDAPDirectory(cfg config.LDAP) *LDAPDirectory`
  - `identity.MockDirectory{}` (Lookup: Name=username, Office=`INFORMATIZZAZIONE`; Offices: `AMMINISTRATIVO`, `INFORMATIZZAZIONE`, `TRIBUTI`)
  - `identity.userFilter(username string) (string, error)` (non esportata, testata)

- [ ] **Step 1: test che falliscono**

`internal/identity/directory_test.go`:

```go
package identity

import (
	"errors"
	"strings"
	"testing"
)

func TestUserFilter(t *testing.T) {
	f, err := userFilter("mirko.daddiego")
	if err != nil || !strings.Contains(f, "(sAMAccountName=mirko.daddiego)") || !strings.Contains(f, "userAccountControl:1.2.840.113556.1.4.803:=2") {
		t.Fatalf("filtro: %q %v", f, err)
	}
	for _, bad := range []string{"", "a*)(cn=*", "x y", strings.Repeat("a", 129)} {
		if _, err := userFilter(bad); !errors.Is(err, ErrUnknownUser) {
			t.Errorf("userFilter(%q): atteso ErrUnknownUser, ottenuto %v", bad, err)
		}
	}
}

func TestMockDirectory(t *testing.T) {
	var d Directory = MockDirectory{}
	p, err := d.Lookup("MRossi")
	if err != nil || p.Username != "mrossi" || p.Office != "INFORMATIZZAZIONE" {
		t.Fatalf("Lookup: %+v %v", p, err)
	}
	if o, err := d.Offices(); err != nil || len(o) != 3 {
		t.Fatalf("Offices: %v %v", o, err)
	}
}
```

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/identity/ -run 'UserFilter|MockDirectory'`
Expected: errori di compilazione.

- [ ] **Step 3: esportare `Dial`**

In `internal/auth/ldap.go` sostituire il metodo `dial` con una funzione esportata e farlo usare a `Authenticate`:

```go
// Dial apre la connessione LDAP con le stesse regole TLS del login admin
// (StartTLS fallito = errore, nessun ripiego in chiaro).
func Dial(cfg config.LDAP) (*ldap.Conn, error) {
	tlsCfg := &tls.Config{
		InsecureSkipVerify: cfg.TLSSkipVerify, //nolint:gosec // opzione esplicita per CA interne
		ServerName:         ldapHostname(cfg.Host),
	}
	conn, err := ldap.DialURL(cfg.Host, ldap.DialWithTLSConfig(tlsCfg))
	if err != nil {
		return nil, fmt.Errorf("ldap dial: %w", err)
	}
	conn.SetTimeout(5 * time.Second)
	if cfg.StartTLS && strings.HasPrefix(cfg.Host, "ldap://") {
		if err := conn.StartTLS(tlsCfg); err != nil {
			conn.Close()
			return nil, fmt.Errorf("ldap StartTLS: %w (LDAP_STARTTLS=false solo su rete fidata)", err)
		}
	}
	return conn, nil
}
```

e in `Authenticate`: `conn, err := Dial(l.cfg)`.

- [ ] **Step 4: directory**

`internal/identity/directory.go`:

```go
package identity

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-ldap/ldap/v3"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/auth"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

// ErrUnknownUser: utente inesistente, disattivato o con nome non ammesso.
var ErrUnknownUser = errors.New("identity: utente non trovato in AD")

// Person è un utente di AD come serve alla plancia.
type Person struct {
	Username string // sAMAccountName, minuscolo
	Name     string // displayName
	Office   string // physicalDeliveryOfficeName, spazi ai bordi rimossi
}

// Directory cerca utenti e uffici.
type Directory interface {
	Lookup(username string) (Person, error)
	Offices() ([]string, error) // valori distinti, ordinati
}

// Stessa regola dello username del login admin.
var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,128}$`)

const activeUsers = `(objectCategory=person)(objectClass=user)(!(userAccountControl:1.2.840.113556.1.4.803:=2))`

func userFilter(username string) (string, error) {
	if !usernameRe.MatchString(username) {
		return "", ErrUnknownUser
	}
	return fmt.Sprintf("(&%s(sAMAccountName=%s))", activeUsers, ldap.EscapeFilter(username)), nil
}

// LDAPDirectory interroga AD con l'account di servizio (LDAP_BIND_DN).
type LDAPDirectory struct {
	cfg config.LDAP

	mu        sync.Mutex
	offices   []string
	officesAt time.Time
}

const officesTTL = 6 * time.Hour

func NewLDAPDirectory(cfg config.LDAP) *LDAPDirectory { return &LDAPDirectory{cfg: cfg} }

func (d *LDAPDirectory) conn() (*ldap.Conn, error) {
	conn, err := auth.Dial(d.cfg)
	if err != nil {
		return nil, err
	}
	if err := conn.Bind(d.cfg.BindDN, d.cfg.BindPassword); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ldap bind di servizio: %w", err)
	}
	return conn, nil
}

func (d *LDAPDirectory) Lookup(username string) (Person, error) {
	filter, err := userFilter(username)
	if err != nil {
		return Person{}, err
	}
	conn, err := d.conn()
	if err != nil {
		return Person{}, err
	}
	defer conn.Close()
	res, err := conn.Search(ldap.NewSearchRequest(d.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, 5, false,
		filter, []string{"sAMAccountName", "displayName", "physicalDeliveryOfficeName"}, nil))
	if err != nil {
		return Person{}, fmt.Errorf("ldap search: %w", err)
	}
	if len(res.Entries) == 0 {
		return Person{}, ErrUnknownUser
	}
	e := res.Entries[0]
	return Person{
		Username: strings.ToLower(e.GetAttributeValue("sAMAccountName")),
		Name:     strings.TrimSpace(e.GetAttributeValue("displayName")),
		Office:   strings.TrimSpace(e.GetAttributeValue("physicalDeliveryOfficeName")),
	}, nil
}

// Offices: cache di 6 ore; se AD non risponde restituisce l'ultimo elenco valido.
func (d *LDAPDirectory) Offices() ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.offices != nil && time.Since(d.officesAt) < officesTTL {
		return d.offices, nil
	}
	list, err := d.loadOffices()
	if err != nil {
		if d.offices != nil {
			return d.offices, nil
		}
		return nil, err
	}
	d.offices, d.officesAt = list, time.Now()
	return list, nil
}

func (d *LDAPDirectory) loadOffices() ([]string, error) {
	conn, err := d.conn()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	res, err := conn.SearchWithPaging(ldap.NewSearchRequest(d.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 30, false,
		"(&"+activeUsers+"(physicalDeliveryOfficeName=*))", []string{"physicalDeliveryOfficeName"}, nil), 500)
	if err != nil {
		return nil, fmt.Errorf("ldap search uffici: %w", err)
	}
	seen := map[string]bool{}
	list := []string{}
	for _, e := range res.Entries {
		o := strings.TrimSpace(e.GetAttributeValue("physicalDeliveryOfficeName"))
		if k := strings.ToUpper(o); o != "" && !seen[k] {
			seen[k] = true
			list = append(list, o)
		}
	}
	sort.Strings(list)
	return list, nil
}

// MockDirectory: per LDAP_HOST=mock (solo sviluppo).
type MockDirectory struct{}

func (MockDirectory) Lookup(username string) (Person, error) {
	if !usernameRe.MatchString(username) {
		return Person{}, ErrUnknownUser
	}
	return Person{Username: strings.ToLower(username), Name: username, Office: "INFORMATIZZAZIONE"}, nil
}

func (MockDirectory) Offices() ([]string, error) {
	return []string{"AMMINISTRATIVO", "INFORMATIZZAZIONE", "TRIBUTI"}, nil
}
```

- [ ] **Step 5: verifica**

Run: `go test ./internal/identity/ ./internal/auth/ && go vet ./...`
Expected: PASS (i test di `auth` usano ancora `Authenticate` invariato).

- [ ] **Step 6: commit**

```bash
git add internal/auth/ldap.go internal/identity
git commit -m "feat(identity): directory AD (utente e uffici) e mock"
```

---

### Task 5: uffici nel database (migrazione v5)

**Files:**
- Modify: `internal/database/migrations.go`
- Create: `internal/database/offices.go`, `internal/database/offices_test.go`
- Modify: `internal/database/alerts.go` (campo `OtherOffices`)

**Interfaces:**
- Produces:
  - `database.OfficeKind` con `OfficesApp`, `OfficesGuide`, `OfficesAlert`
  - `func (db *DB) Offices(k OfficeKind) (map[int64][]string, error)`
  - `func (db *DB) OfficesOf(k OfficeKind, id int64) ([]string, error)` (slice mai nil)
  - `func (db *DB) SetOffices(k OfficeKind, id int64, offices []string) error`
  - `database.Alert.OtherOffices bool` (calcolato dal web, non salvato)

- [ ] **Step 1: test che falliscono**

`internal/database/offices_test.go`:

```go
package database

import (
	"reflect"
	"testing"
	"time"
)

func TestOfficesLifecycle(t *testing.T) {
	db := newTestDB(t)
	apps, _ := db.ListApps()
	app := apps[0].ID

	if got, err := db.OfficesOf(OfficesApp, app); err != nil || len(got) != 0 || got == nil {
		t.Fatalf("iniziale: %v %v", got, err)
	}
	if err := db.SetOffices(OfficesApp, app, []string{"TRIBUTI", " LLPP ", "tributi", ""}); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.OfficesOf(OfficesApp, app); !reflect.DeepEqual(got, []string{"LLPP", "TRIBUTI"}) {
		t.Fatalf("dopo Set (trim, vuoti e doppioni senza maiuscole tolti, ordinati): %v", got)
	}
	if m, _ := db.Offices(OfficesApp); !reflect.DeepEqual(m[app], []string{"LLPP", "TRIBUTI"}) {
		t.Fatalf("Offices: %v", m)
	}
	if err := db.SetOffices(OfficesApp, app, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.OfficesOf(OfficesApp, app); len(got) != 0 {
		t.Fatalf("dopo Set vuoto: %v", got)
	}

	id, err := db.CreateAlert(Alert{Title: "x", Level: LevelNews, StartsAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	db.SetOffices(OfficesAlert, id, []string{"TRIBUTI"})
	if err := db.DeleteAlert(id); err != nil {
		t.Fatal(err)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM alert_offices`).Scan(&n)
	if n != 0 {
		t.Fatalf("cancellazione a cascata mancante: %d righe", n)
	}
}
```

(Se `DeleteAlert` ha un altro nome, usare il metodo di cancellazione esistente in `alerts.go`.)

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/database/ -run Offices`
Expected: errori di compilazione.

- [ ] **Step 3: migrazione e metodi**

`migrations.go`: aggiungere `migrateV5Offices` in coda all'elenco e:

```go
func migrateV5Offices(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE app_offices (
	app_id INTEGER NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
	office TEXT    NOT NULL,
	PRIMARY KEY (app_id, office)
);
CREATE TABLE guide_offices (
	guide_id INTEGER NOT NULL REFERENCES guides(id) ON DELETE CASCADE,
	office   TEXT    NOT NULL,
	PRIMARY KEY (guide_id, office)
);
CREATE TABLE alert_offices (
	alert_id INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
	office   TEXT    NOT NULL,
	PRIMARY KEY (alert_id, office)
);
`)
	return err
}
```

`internal/database/offices.go`:

```go
package database

import (
	"fmt"
	"sort"
	"strings"
)

// OfficeKind sceglie la tabella degli uffici (valori AD di physicalDeliveryOfficeName).
type OfficeKind string

const (
	OfficesApp   OfficeKind = "app"
	OfficesGuide OfficeKind = "guide"
	OfficesAlert OfficeKind = "alert"
)

func (k OfficeKind) table() (table, col string) {
	switch k {
	case OfficesApp:
		return "app_offices", "app_id"
	case OfficesGuide:
		return "guide_offices", "guide_id"
	case OfficesAlert:
		return "alert_offices", "alert_id"
	}
	panic(fmt.Sprintf("OfficeKind non valido: %q", string(k)))
}

// Offices restituisce gli uffici di tutti gli elementi del tipo k che ne hanno.
func (db *DB) Offices(k OfficeKind) (map[int64][]string, error) {
	table, col := k.table()
	rows, err := db.Query(`SELECT ` + col + `, office FROM ` + table + ` ORDER BY ` + col + `, office COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var o string
		if err := rows.Scan(&id, &o); err != nil {
			return nil, err
		}
		out[id] = append(out[id], o)
	}
	return out, rows.Err()
}

func (db *DB) OfficesOf(k OfficeKind, id int64) ([]string, error) {
	table, col := k.table()
	rows, err := db.Query(`SELECT office FROM `+table+` WHERE `+col+` = ? ORDER BY office COLLATE NOCASE`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SetOffices sostituisce gli uffici dell'elemento. Nessun ufficio = per tutti.
func (db *DB) SetOffices(k OfficeKind, id int64, offices []string) error {
	table, col := k.table()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM `+table+` WHERE `+col+` = ?`, id); err != nil {
		return err
	}
	for _, o := range normalizeOffices(offices) {
		if _, err := tx.Exec(`INSERT INTO `+table+` (`+col+`, office) VALUES (?, ?)`, id, o); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// normalizeOffices toglie spazi, vuoti e doppioni (senza maiuscole/minuscole) e ordina.
func normalizeOffices(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, o := range in {
		o = strings.TrimSpace(o)
		if k := strings.ToUpper(o); o != "" && !seen[k] {
			seen[k] = true
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToUpper(out[i]) < strings.ToUpper(out[j]) })
	return out
}
```

`alerts.go`, nello struct `Alert` dopo `CreatedBy`:

```go
	OtherOffices bool // calcolato dal web per chi guarda, non salvato
```

- [ ] **Step 4: verifica**

Run: `go test ./internal/database/ ./internal/backup/ ./internal/web/ && go vet ./...`
Expected: PASS (`TestOpenAppliesMigrationsAndSeed` usa `len(migrations)`).

- [ ] **Step 5: commit**

```bash
git add internal/database
git commit -m "feat(database): migrazione 5 con gli uffici di app, guide e avvisi"
```

---

### Task 6: endpoint `/io` e cookie dell'utente nel server

**Files:**
- Modify: `internal/web/server.go` (Options/Server: `Directory`, `cookies`; route)
- Create: `internal/web/identity.go`, `internal/web/identity_test.go`
- Modify: `internal/web/server_test.go` (directory finta nei test)
- Modify: `cmd/server/main.go`

**Interfaces:**
- Consumes: `identity.*` (Task 2–4), `config.Config.NTLMDomain` (Task 1).
- Produces:
  - `web.Options.Directory identity.Directory`
  - `func (s *Server) recognitionEnabled() bool`
  - `func (s *Server) viewer(r *http.Request) (identity.User, bool)` (`false` = nessun cookie valido)
  - `func (s *Server) setViewer(w http.ResponseWriter, r *http.Request, u identity.User)`
  - route `GET /io`
  - nei test: `fakeDirectory` con `people map[string]identity.Person`, `offices []string`, `err error`

- [ ] **Step 1: directory finta nei test**

In `internal/web/server_test.go`, vicino a `fakeAuth`:

```go
type fakeDirectory struct {
	people  map[string]identity.Person
	offices []string
	err     error
}

func (f fakeDirectory) Lookup(u string) (identity.Person, error) {
	if f.err != nil {
		return identity.Person{}, f.err
	}
	p, ok := f.people[strings.ToLower(u)]
	if !ok {
		return identity.Person{}, identity.ErrUnknownUser
	}
	return p, nil
}

func (f fakeDirectory) Offices() ([]string, error) { return f.offices, f.err }

var testDirectory = fakeDirectory{
	people: map[string]identity.Person{
		"mrossi":  {Username: "mrossi", Name: "Mario Rossi", Office: "TRIBUTI"},
		"nessuno": {Username: "nessuno", Name: "Senza Ufficio"},
	},
	offices: []string{"LLPP", "TRIBUTI"},
}
```

In `newTestServerWith`, nell'`Options` di default: `Directory: testDirectory,` e nella `config.Config` del test: `NTLMDomain: "COMUNE-MS",`.

- [ ] **Step 2: test che falliscono**

`internal/web/identity_test.go`:

```go
package web

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity/ntlmtest"
)

func ntlmHeader(msg []byte) map[string]string {
	return map[string]string{"Authorization": "NTLM " + base64.StdEncoding.EncodeToString(msg)}
}

func cookieNamed(t *testing.T, rec interface{ Result() *http.Response }, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestIoHandshake(t *testing.T) {
	s, _ := newTestServer(t, nil)

	rec := do(t, s, "GET", "/io", nil, nil, nil)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") != "NTLM" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("passo 0: %d %v", rec.Code, rec.Header())
	}
	if c := cookieNamed(t, rec, identity.CookieName); c == nil || c.MaxAge != int(identity.AnonymousTTL.Seconds()) {
		t.Fatal("passo 0: atteso il cookie anonimo da 24 ore")
	}

	rec = do(t, s, "GET", "/io", nil, nil, ntlmHeader(ntlmtest.Negotiate()))
	if rec.Code != http.StatusUnauthorized || !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "NTLM ") {
		t.Fatalf("tipo 1: %d %q", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}

	rec = do(t, s, "GET", "/io", nil, nil, ntlmHeader(ntlmtest.Authenticate("comune-ms", "MRossi", "PC-1")))
	c := cookieNamed(t, rec, identity.CookieName)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"riconosciuto":true`) || !strings.Contains(rec.Body.String(), `"nome":"Mario"`) || c == nil {
		t.Fatalf("tipo 3: %d %s", rec.Code, rec.Body)
	}
	if !c.HttpOnly || c.Path != "/" || c.SameSite != http.SameSiteLaxMode || c.MaxAge != int(identity.UserTTL.Seconds()) {
		t.Fatalf("attributi del cookie: %+v", c)
	}
	if u, ok := s.cookies.Decode(c.Value); !ok || u.Office != "TRIBUTI" || u.Anonymous {
		t.Fatalf("contenuto del cookie: %+v %v", u, ok)
	}
}

func TestIoRejections(t *testing.T) {
	s, _ := newTestServer(t, nil)
	anon := func(rec interface{ Result() *http.Response }) bool {
		c := cookieNamed(t, rec, identity.CookieName)
		u, ok := s.cookies.Decode(c.Value)
		return ok && u.Anonymous
	}
	if rec := do(t, s, "GET", "/io", nil, nil, ntlmHeader(ntlmtest.Authenticate("ALTRO", "mrossi", "W"))); rec.Code != 200 || !anon(rec) {
		t.Fatalf("dominio sbagliato: %d", rec.Code)
	}
	if rec := do(t, s, "GET", "/io", nil, nil, ntlmHeader(ntlmtest.Authenticate("COMUNE-MS", "sconosciuto", "W"))); rec.Code != 200 || !anon(rec) {
		t.Fatalf("utente sconosciuto: %d", rec.Code)
	}
	for _, h := range []map[string]string{
		{"Authorization": "NTLM !!!"},
		{"Authorization": "Basic eDp5"},
		ntlmHeader([]byte("NTLMSSP\x00\x03\x00\x00\x00troppo corto")),
	} {
		if rec := do(t, s, "GET", "/io", nil, nil, h); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: atteso 400, ottenuto %d", h, rec.Code)
		}
	}
}

func TestIoLDAPDown(t *testing.T) {
	s, _ := newTestServerWith(t, nil, func(o *Options) { o.Directory = fakeDirectory{err: errors.New("giù")} })
	rec := do(t, s, "GET", "/io", nil, nil, ntlmHeader(ntlmtest.Authenticate("COMUNE-MS", "mrossi", "W")))
	if rec.Code != http.StatusServiceUnavailable || cookieNamed(t, rec, identity.CookieName) != nil {
		t.Fatalf("LDAP giù: %d", rec.Code)
	}
}

func TestIoDisabled(t *testing.T) {
	s, _ := newTestServerWith(t, nil, func(o *Options) { o.Config.NTLMDomain = "" })
	if rec := do(t, s, "GET", "/io", nil, nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("riconoscimento spento: %d", rec.Code)
	}
}

// Identità dichiarata: il cookie utente non deve mai aprire l'admin.
func TestViewerCookieDoesNotOpenAdmin(t *testing.T) {
	s, _ := newTestServer(t, nil)
	v, _ := s.cookies.Encode(identity.User{Username: "mrossi", Name: "Mario Rossi", Office: "TRIBUTI"})
	rec := do(t, s, "GET", "/admin", nil, &http.Cookie{Name: identity.CookieName, Value: v}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("/admin con il solo cookie utente: atteso 303, ottenuto %d", rec.Code)
	}
}
```

- [ ] **Step 3: verifica che falliscano**

Run: `go test ./internal/web/ -run 'Io|ViewerCookie'`
Expected: errori di compilazione (`s.cookies undefined`, `Options.Directory`…).

- [ ] **Step 4: server**

`internal/web/server.go`:
- in `Options`: `Directory identity.Directory // nil = nessuna directory (riconoscimento spento, uffici AD non disponibili)`
- in `Server`: `directory identity.Directory` e `cookies *identity.CookieCodec`
- in `New`: `directory: o.Directory,` nel letterale e, dopo `s.store = newSessionStore(...)`, `s.cookies = identity.NewCookieCodec(o.Config.SessionSecret)`
- in `routes()`, dopo `/health`: `s.mux.HandleFunc("GET /io", s.handleIo)`

`internal/web/identity.go`:

```go
package web

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/auth"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

func (s *Server) recognitionEnabled() bool {
	return s.cfg.NTLMDomain != "" && s.directory != nil
}

// viewer legge il cookie dell'utente. Identità DICHIARATA: usarla solo per
// personalizzare la vista, mai per autorizzare.
func (s *Server) viewer(r *http.Request) (identity.User, bool) {
	c, err := r.Cookie(identity.CookieName)
	if err != nil {
		return identity.User{}, false
	}
	return s.cookies.Decode(c.Value)
}

func (s *Server) setViewer(w http.ResponseWriter, r *http.Request, u identity.User) {
	v, err := s.cookies.Encode(u)
	if err != nil {
		slog.Warn("cookie utente", "err", err)
		return
	}
	ttl := identity.UserTTL
	if u.Anonymous {
		ttl = identity.AnonymousTTL
	}
	http.SetCookie(w, &http.Cookie{Name: identity.CookieName, Value: v, Path: "/", MaxAge: int(ttl.Seconds()),
		HttpOnly: true, Secure: s.secureRequest(r), SameSite: http.SameSiteLaxMode})
}

// handleIo fa l'handshake NTLM chiamato in background da dashboard.js e salva
// chi è l'utente (nome e ufficio da AD). Il nome NTLM NON è verificato.
func (s *Server) handleIo(w http.ResponseWriter, r *http.Request) {
	if !s.recognitionEnabled() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	authz := r.Header.Get("Authorization")
	if authz == "" {
		// Se il browser non prosegue resta anonimo per 24 ore, senza ritentare.
		s.setViewer(w, r, identity.User{Anonymous: true})
		w.Header().Set("WWW-Authenticate", "NTLM")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	scheme, tok, _ := strings.Cut(authz, " ")
	msg, err := base64.StdEncoding.DecodeString(strings.TrimSpace(tok))
	if !strings.EqualFold(scheme, "NTLM") || err != nil {
		http.Error(w, "Autorizzazione non valida", http.StatusBadRequest)
		return
	}
	switch identity.MessageType(msg) {
	case 1:
		w.Header().Set("WWW-Authenticate", "NTLM "+base64.StdEncoding.EncodeToString(identity.Challenge()))
		w.WriteHeader(http.StatusUnauthorized)
	case 3:
		s.finishRecognition(w, r, msg)
	default:
		http.Error(w, "Messaggio NTLM non valido", http.StatusBadRequest)
	}
}

func (s *Server) finishRecognition(w http.ResponseWriter, r *http.Request, msg []byte) {
	login, err := identity.ParseAuthenticate(msg)
	if err != nil {
		http.Error(w, "Messaggio NTLM non valido", http.StatusBadRequest)
		return
	}
	if !strings.EqualFold(login.Domain, s.cfg.NTLMDomain) {
		slog.Info("riconoscimento: dominio non ammesso", "domain", auth.SafeLog(login.Domain), "user", auth.SafeLog(login.User))
		s.recognized(w, r, identity.User{Anonymous: true})
		return
	}
	p, err := s.directory.Lookup(login.User)
	switch {
	case errors.Is(err, identity.ErrUnknownUser):
		slog.Info("riconoscimento: utente non trovato in AD", "user", auth.SafeLog(login.User))
		s.recognized(w, r, identity.User{Anonymous: true})
	case err != nil:
		slog.Warn("riconoscimento: AD non disponibile", "err", err)
		http.Error(w, "Directory non disponibile", http.StatusServiceUnavailable)
	default:
		s.recognized(w, r, identity.User{Username: p.Username, Name: p.Name, Office: p.Office})
	}
}

func (s *Server) recognized(w http.ResponseWriter, r *http.Request, u identity.User) {
	s.setViewer(w, r, u)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"riconosciuto": !u.Anonymous, "nome": u.FirstName()})
}
```

`cmd/server/main.go`, dove si costruiscono le `web.Options` (vicino a `Auth: auth.NewLDAP(cfg.LDAP)`):

```go
	var directory identity.Directory
	switch {
	case cfg.LDAP.Host == "mock":
		directory = identity.MockDirectory{}
	case cfg.LDAP.BindDN != "":
		directory = identity.NewLDAPDirectory(cfg.LDAP)
	}
	if cfg.NTLMDomain != "" && directory == nil {
		slog.Warn("NTLM_DOMAIN impostato ma LDAP_BIND_DN vuoto: riconoscimento utente disattivato")
	}
```

e nel letterale `Directory: directory,`.

- [ ] **Step 5: verifica**

Run: `go test ./... && go vet ./... && gofmt -l internal/ cmd/`
Expected: PASS, nessun file da gofmt.

- [ ] **Step 6: commit**

```bash
git add internal/web cmd/server/main.go
git commit -m "feat(web): /io riconosce l'utente via NTLM e salva nome e ufficio nel cookie"
```

---

### Task 7: plancia personalizzata e filtrata

**Files:**
- Create: `internal/web/offices.go`, `internal/web/offices_test.go`
- Modify: `internal/web/dashboard.go` (view, handler della plancia, `/partials/alerts`, `/avvisi`)
- Modify: `web/templates/dashboard.html`, `web/templates/partials_dashboard.html`
- Modify: `web/static/js/dashboard.js`, `web/static/css/plancia.css`

**Interfaces:**
- Consumes: `s.viewer`, `s.recognitionEnabled` (Task 6); `db.Offices`, `Alert.OtherOffices` (Task 5); `identity.Concerns` (Task 3).
- Produces:
  - `const showAllCookie = "cruscotto_tutto"`
  - `type officeFilter struct { Office string; ShowAll bool; Hidden int }`
  - `func (s *Server) officeFilterFor(r *http.Request, u identity.User) officeFilter`
  - `func (f *officeFilter) dashboard(d *database.Dashboard, apps, guides, alerts map[int64][]string)`
  - `func (f *officeFilter) alerts(list []database.Alert, offices map[int64][]string) []database.Alert`
  - in `dashboardView`: `User identity.User`, `Recognize bool`, `Filter officeFilter`

- [ ] **Step 1: test che falliscono**

`internal/web/offices_test.go`:

```go
package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// seedOffices: Rubrica (per tutti), Webmail (solo LLPP), una guida generale
// solo TRIBUTI, un urgente solo LLPP e una novità per tutti.
func seedOffices(t *testing.T, db *database.DB) {
	t.Helper()
	apps, _ := db.ListApps()
	for _, a := range apps {
		a.URL = "https://example.it/" + strings.ToLower(a.Title)
		if err := db.UpdateApp(a.App); err != nil {
			t.Fatal(err)
		}
		if a.Title == "Webmail" {
			db.SetOffices(database.OfficesApp, a.ID, []string{"LLPP"})
		}
	}
	gid, err := db.CreateGuide(database.Guide{Title: "Guida Tributi", URL: "https://example.it/g", Kind: "link", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	db.SetOffices(database.OfficesGuide, gid, []string{"TRIBUTI"})
	start := fixedNow.Add(-time.Hour)
	uid, _ := db.CreateAlert(database.Alert{Title: "Urgente LLPP", Level: database.LevelUrgent, StartsAt: start})
	db.SetOffices(database.OfficesAlert, uid, []string{"LLPP"})
	db.CreateAlert(database.Alert{Title: "Novità per tutti", Level: database.LevelNews, StartsAt: start})
}

func viewerCookie(t *testing.T, s *Server, u identity.User) *http.Cookie {
	t.Helper()
	v, err := s.cookies.Encode(u)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: identity.CookieName, Value: v}
}

func TestDashboardFilteredByOffice(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedOffices(t, db)
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi", Office: "TRIBUTI"})
	body := do(t, s, "GET", "/", nil, c, nil).Body.String()

	for _, want := range []string{"Rubrica", "Guida Tributi", "Novità per tutti", ", Mario", "Ufficio TRIBUTI", "Mostra anche i contenuti degli altri uffici (2)"} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	for _, no := range []string{"Webmail", "Urgente LLPP", "data-riconosci"} {
		if strings.Contains(body, no) {
			t.Errorf("non doveva esserci %q", no)
		}
	}
}

func TestDashboardShowAll(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedOffices(t, db)
	req := map[string]string{"Cookie": viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi", Office: "TRIBUTI"}).String() + "; " + showAllCookie + "=1"}
	body := do(t, s, "GET", "/", nil, nil, req).Body.String()
	for _, want := range []string{"Webmail", "Urgente LLPP", "Mostra solo il mio ufficio"} {
		if !strings.Contains(body, want) {
			t.Errorf("con Mostra tutto manca %q", want)
		}
	}
	// L'urgente di un altro ufficio sta nel carosello ma non apre il popup.
	if strings.Contains(body, `<dialog class="urgent"`) {
		t.Error("popup urgente per un altro ufficio")
	}
}

func TestDashboardUserWithoutOffice(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedOffices(t, db)
	body := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Username: "nessuno", Name: "Senza Ufficio"}), nil).Body.String()
	if !strings.Contains(body, "Rubrica") || strings.Contains(body, "Guida Tributi") || strings.Contains(body, "Webmail") || strings.Contains(body, "Ufficio ") {
		t.Fatal("senza ufficio: solo contenuti per tutti, nessuna riga ufficio")
	}
}

func TestDashboardAnonymousRecognize(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedOffices(t, db)
	if body := do(t, s, "GET", "/", nil, nil, nil).Body.String(); !strings.Contains(body, "data-riconosci") || strings.Contains(body, "Webmail") {
		t.Fatal("senza cookie: data-riconosci presente e solo contenuti per tutti")
	}
	anon := viewerCookie(t, s, identity.User{Anonymous: true})
	if body := do(t, s, "GET", "/", nil, anon, nil).Body.String(); strings.Contains(body, "data-riconosci") {
		t.Fatal("con il cookie anonimo non si ritenta")
	}
	s2, _ := newTestServerWith(t, nil, func(o *Options) { o.Config.NTLMDomain = "" })
	if body := do(t, s2, "GET", "/", nil, nil, nil).Body.String(); strings.Contains(body, "data-riconosci") {
		t.Fatal("riconoscimento spento: nessun tentativo")
	}
}

func TestUrgentPopupForOwnOffice(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedOffices(t, db)
	body := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Username: "x", Name: "X", Office: "llpp"}), nil).Body.String()
	if !strings.Contains(body, `<dialog class="urgent"`) || !strings.Contains(body, "Webmail") {
		t.Fatal("LLPP deve vedere Webmail e il popup del proprio urgente")
	}
}

func TestAvvisiAndPartialFiltered(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedOffices(t, db)
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi", Office: "TRIBUTI"})
	for _, path := range []string{"/avvisi", "/partials/alerts"} {
		body := do(t, s, "GET", path, nil, c, nil).Body.String()
		if strings.Contains(body, "Urgente LLPP") || !strings.Contains(body, "Novità per tutti") {
			t.Errorf("%s: filtro avvisi mancante", path)
		}
	}
}
```

Prima di scrivere il test verificare i nomi reali: `ListApps` restituisce righe con `App` incorporato (adeguare `a.App`/`a` al tipo reale), `CreateGuide` richiede i campi effettivi di `database.Guide` (es. `Kind`), e il nome del metodo di cancellazione. Adeguare il test, non l'implementazione.

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/web/ -run 'Dashboard|Urgent|AvvisiAndPartial'`
Expected: errori di compilazione (`showAllCookie undefined`) e poi FAIL sui contenuti.

- [ ] **Step 3: filtro**

`internal/web/offices.go`:

```go
package web

import (
	"net/http"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// showAllCookie: preferenza "Mostra tutto", impostata da dashboard.js.
const showAllCookie = "cruscotto_tutto"

// officeFilter toglie dalla vista i contenuti di altri uffici. È
// presentazione, non sicurezza: chi sceglie "Mostra tutto" li vede.
type officeFilter struct {
	Office  string
	ShowAll bool
	Hidden  int // contenuti esclusi (per il testo dell'interruttore)
}

func (s *Server) officeFilterFor(r *http.Request, u identity.User) officeFilter {
	c, err := r.Cookie(showAllCookie)
	return officeFilter{Office: u.Office, ShowAll: err == nil && c.Value == "1"}
}

// keep conta ciò che esclude e dice se l'elemento resta in pagina.
func (f *officeFilter) keep(concerns bool) bool {
	if concerns || f.ShowAll {
		return true
	}
	f.Hidden++
	return false
}

func (f *officeFilter) dashboard(d *database.Dashboard, apps, guides, alerts map[int64][]string) {
	d.Alerts = f.alerts(d.Alerts, alerts)

	cats := d.Categories[:0]
	for _, c := range d.Categories {
		kept := c.Apps[:0]
		for _, a := range c.Apps {
			if !f.keep(identity.Concerns(f.Office, apps[a.ID])) {
				continue
			}
			gs := a.Guides[:0]
			for _, g := range a.Guides {
				if f.keep(identity.Concerns(f.Office, guides[g.ID])) {
					gs = append(gs, g)
				}
			}
			a.Guides = gs
			kept = append(kept, a)
		}
		if len(kept) > 0 {
			c.Apps = kept
			cats = append(cats, c)
		}
	}
	d.Categories = cats

	gen := d.GeneralGuides[:0]
	for _, g := range d.GeneralGuides {
		if f.keep(identity.Concerns(f.Office, guides[g.ID])) {
			gen = append(gen, g)
		}
	}
	d.GeneralGuides = gen
}

// alerts filtra gli avvisi; con "Mostra tutto" quelli di altri uffici restano
// ma marcati, così non aprono il popup degli urgenti.
func (f *officeFilter) alerts(list []database.Alert, offices map[int64][]string) []database.Alert {
	out := list[:0]
	for _, a := range list {
		concerns := identity.Concerns(f.Office, offices[a.ID])
		if !f.keep(concerns) {
			continue
		}
		a.OtherOffices = !concerns
		out = append(out, a)
	}
	return out
}
```

`internal/web/dashboard.go`:
- `dashboardView` riceve `User identity.User`, `Recognize bool`, `Filter officeFilter`; `avvisiView` riceve `Filter officeFilter` (non usato nel template, ma utile per coerenza: se non serve, ometterlo).
- Helper:

```go
// visibleAlerts applica il filtro uffici a una lista di avvisi attivi.
func (s *Server) visibleAlerts(r *http.Request, list []database.Alert) ([]database.Alert, officeFilter, error) {
	u, _ := s.viewer(r)
	f := s.officeFilterFor(r, u)
	offices, err := s.db.Offices(database.OfficesAlert)
	if err != nil {
		return nil, f, err
	}
	return f.alerts(list, offices), f, nil
}
```

- `handleDashboard`, dopo `GetDashboard`:

```go
	u, known := s.viewer(r)
	f := s.officeFilterFor(r, u)
	apps, err := s.db.Offices(database.OfficesApp)
	if err != nil {
		s.serverError(w, err)
		return
	}
	guides, err := s.db.Offices(database.OfficesGuide)
	if err != nil {
		s.serverError(w, err)
		return
	}
	alerts, err := s.db.Offices(database.OfficesAlert)
	if err != nil {
		s.serverError(w, err)
		return
	}
	f.dashboard(&d, apps, guides, alerts)
```

e nel letterale della view: `User: u, Recognize: !known && s.recognitionEnabled(), Filter: f,`.
- `handleAlertsPartial` e `handleAvvisi`: dopo `ListActiveAlerts`, `alerts, _, err = s.visibleAlerts(r, alerts)` con gestione errore `s.serverError`.

- [ ] **Step 4: template, JS e CSS**

`web/templates/dashboard.html`:
- `<body class="plancia">` → `<body class="plancia"{{if .Recognize}} data-riconosci{{end}}>`
- `<span class="hello-name"></span>` → `<span class="hello-name">{{with .User.FirstName}}, {{.}}{{end}}</span>`
- dopo `<p class="hero-date" data-date>…</p>`: `{{with .User.Office}}<p class="hero-office">Ufficio {{.}}</p>{{end}}`
- dopo la `</label>` della ricerca:

```html
	{{if or .Filter.Hidden .Filter.ShowAll}}<div class="office-toggle"><button type="button" data-mostra-tutto="{{if .Filter.ShowAll}}0{{else}}1{{end}}" hidden>{{if .Filter.ShowAll}}Mostra solo il mio ufficio{{else}}Mostra anche i contenuti degli altri uffici ({{.Filter.Hidden}}){{end}}</button></div>{{end}}
```

`web/templates/partials_dashboard.html`, nel ciclo degli urgenti: `{{range .}}{{if eq .Level "urgent"}}` → `{{range .}}{{if and (eq .Level "urgent") (not .OtherOffices)}}`.

`web/static/js/dashboard.js`, subito prima di `initCarousel();` in fondo:

```js
	// Riconoscimento (identità dichiarata, solo per personalizzare): una chiamata
	// in background; se il server riconosce l'utente ricarica la pagina una volta.
	if (document.body.hasAttribute("data-riconosci")) {
		fetch("/io", { credentials: "same-origin" })
			.then((r) => (r.ok ? r.json() : null))
			.then((j) => { if (j && j.riconosciuto) location.reload(); })
			.catch(() => { /* resta anonimo */ });
	}

	// "Mostra tutto": preferenza in un cookie letto dal server, poi ricarica.
	const officeToggle = document.querySelector("[data-mostra-tutto]");
	if (officeToggle) {
		officeToggle.hidden = false;
		officeToggle.addEventListener("click", () => {
			document.cookie = officeToggle.dataset.mostraTutto === "1"
				? "cruscotto_tutto=1; Path=/; Max-Age=31536000; SameSite=Lax"
				: "cruscotto_tutto=; Path=/; Max-Age=0; SameSite=Lax";
			location.reload();
		});
	}
```

`web/static/css/plancia.css`, dopo `.hero-date { … }`:

```css
.hero-office { margin: .15rem 0 0; opacity: .75; font-size: .8rem; letter-spacing: .02em; }
.office-toggle { margin: .7rem var(--p-gutter) 0; text-align: right; }
.office-toggle button { border: 0; background: none; color: var(--p-blue-2); font: inherit; font-size: .82rem; font-weight: 600; cursor: pointer; padding: .2rem 0; }
.office-toggle button:hover { text-decoration: underline; }
```

e nel blocco `@media (max-width: 640px)` niente da aggiungere (`--p-gutter` vale già 1rem).

- [ ] **Step 5: verifica**

Run: `go test ./... && go vet ./... && gofmt -l internal/`
Expected: PASS.

- [ ] **Step 6: commit**

```bash
git add internal/web web/templates web/static
git commit -m "feat(plancia): saluto per nome e contenuti filtrati per ufficio con Mostra tutto"
```

---

### Task 8: uffici in admin (applicativi, guide, avvisi)

**Files:**
- Modify: `internal/web/offices.go` (helper admin)
- Modify: `internal/web/admin_apps.go`, `internal/web/admin_guides.go`, `internal/web/admin_alerts.go`
- Modify: `web/templates/admin_app.html`, `web/templates/admin_guide.html`, `web/templates/admin_avvisi.html`
- Create: `web/templates/partials_offices.html`
- Modify: `web/static/css/admin.css`
- Test: `internal/web/admin_offices_test.go`

**Interfaces:**
- Consumes: `s.directory` (Task 6), `db.Offices/OfficesOf/SetOffices` (Task 5).
- Produces:
  - `type officeChoice struct { Name string; Checked, Missing bool }`
  - `type officesField struct { Choices []officeChoice; Unavailable bool }`
  - `func (s *Server) officeChoices(selected []string) officesField`
  - `func (s *Server) parseOffices(r *http.Request, current []string, errs formErrors) []string` (campo form `uffici`, ripetuto)
  - template `offices_fieldset` (dati: `officesField`) e `offices_label` (dati: `[]string`)
  - nei tre form: campo `Offices []string`; nelle tre sezioni: `OfficeChoices officesField`, `ItemOffices map[int64][]string`

- [ ] **Step 1: test che falliscono**

`internal/web/admin_offices_test.go`:

```go
package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestAlertOfficesSaved(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	page := do(t, s, "GET", "/admin/avvisi", nil, c, nil).Body.String()
	if !strings.Contains(page, `name="uffici" value="LLPP"`) || !strings.Contains(page, `name="uffici" value="TRIBUTI"`) {
		t.Fatal("caselle degli uffici da AD mancanti")
	}
	form := url.Values{"title": {"Solo tributi"}, "level": {"news"}, "starts_at": {"2026-10-06T09:00"}, "uffici": {"TRIBUTI"}}
	rec := do(t, s, "POST", "/admin/avvisi", form, c, map[string]string{"HX-Request": "true"})
	if rec.Code != 200 {
		t.Fatalf("salvataggio: %d\n%s", rec.Code, rec.Body)
	}
	all, _ := db.ListActiveAlerts(fixedNow)
	got, _ := db.OfficesOf(database.OfficesAlert, all[0].ID)
	if len(got) != 1 || got[0] != "TRIBUTI" {
		t.Fatalf("uffici salvati: %v", got)
	}
	if !strings.Contains(rec.Body.String(), "TRIBUTI") {
		t.Fatal("l'elenco deve mostrare l'ufficio dell'avviso")
	}
}

func TestOfficeNotAllowed(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	form := url.Values{"title": {"x"}, "level": {"news"}, "starts_at": {"2026-10-06T09:00"}, "uffici": {"INVENTATO"}}
	if rec := do(t, s, "POST", "/admin/avvisi", form, c, map[string]string{"HX-Request": "true"}); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Ufficio non riconosciuto") {
		t.Fatalf("ufficio inventato: %d", rec.Code)
	}
}

// Un ufficio sparito da AD resta modificabile e non blocca il salvataggio.
func TestOfficeGoneFromAD(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	apps, _ := db.ListApps()
	id := apps[0].ID
	db.SetOffices(database.OfficesApp, id, []string{"SCIOLTO"})
	page := do(t, s, "GET", "/admin/app/"+itoa(id)+"/modifica", nil, c, map[string]string{"HX-Request": "true"}).Body.String()
	if !strings.Contains(page, "SCIOLTO") || !strings.Contains(page, "non più in AD") {
		t.Fatal("ufficio sparito da AD non mostrato")
	}
	rec := postMultipart(t, s, "/admin/app/"+itoa(id), appFields(db, map[string]string{"uffici": "SCIOLTO"}), nil, c)
	if rec.Code != 200 {
		t.Fatalf("salvataggio con ufficio sparito: %d\n%s", rec.Code, rec.Body)
	}
}

func TestOfficesADUnavailable(t *testing.T) {
	s, _ := newTestServerWith(t, nil, func(o *Options) { o.Directory = fakeDirectory{err: errors.New("giù")} })
	c := login(t, s)
	if page := do(t, s, "GET", "/admin/guide", nil, c, nil).Body.String(); !strings.Contains(page, "Elenco uffici da AD non disponibile") {
		t.Fatal("avviso AD non disponibile mancante")
	}
}
```

Verificare i nomi dei campi reali dei form (`title`, `level`, `starts_at` per gli avvisi; `appFields` già esistente per le app) e adeguare il test se diversi.

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/web/ -run 'Office'`
Expected: FAIL (caselle assenti, uffici non salvati).

- [ ] **Step 3: helper**

In `internal/web/offices.go` aggiungere (import `strings`, `sort`):

```go
type officeChoice struct {
	Name    string
	Checked bool
	Missing bool // assegnato ma non più presente in AD
}

type officesField struct {
	Choices     []officeChoice
	Unavailable bool // elenco AD non leggibile
}

const maxOffices = 30

// officeChoices: uffici di AD più quelli già assegnati che AD non ha più.
func (s *Server) officeChoices(selected []string) officesField {
	var f officesField
	var ad []string
	if s.directory != nil {
		var err error
		if ad, err = s.directory.Offices(); err != nil {
			slog.Warn("uffici da AD", "err", err)
			f.Unavailable = true
		}
	} else {
		f.Unavailable = true
	}
	sel := map[string]bool{}
	for _, o := range selected {
		sel[strings.ToUpper(o)] = true
	}
	inAD := map[string]bool{}
	for _, o := range ad {
		inAD[strings.ToUpper(o)] = true
		f.Choices = append(f.Choices, officeChoice{Name: o, Checked: sel[strings.ToUpper(o)]})
	}
	for _, o := range selected {
		if !inAD[strings.ToUpper(o)] {
			f.Choices = append(f.Choices, officeChoice{Name: o, Checked: true, Missing: !f.Unavailable})
		}
	}
	sort.Slice(f.Choices, func(i, j int) bool { return strings.ToUpper(f.Choices[i].Name) < strings.ToUpper(f.Choices[j].Name) })
	return f
}

// parseOffices legge le caselle "uffici": ammessi quelli di AD e quelli già
// assegnati all'elemento (current). Nessuna casella = per tutti.
func (s *Server) parseOffices(r *http.Request, current []string, errs formErrors) []string {
	allowed := map[string]bool{}
	for _, o := range current {
		allowed[strings.ToUpper(o)] = true
	}
	if s.directory != nil {
		if ad, err := s.directory.Offices(); err == nil {
			for _, o := range ad {
				allowed[strings.ToUpper(o)] = true
			}
		}
	}
	out := []string{}
	for _, o := range r.Form["uffici"] {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		if !allowed[strings.ToUpper(o)] {
			errs.add("uffici", "Ufficio non riconosciuto: "+o)
			continue
		}
		out = append(out, o)
	}
	if len(out) > maxOffices {
		errs.add("uffici", fmt.Sprintf("Massimo %d uffici.", maxOffices))
	}
	return out
}
```

(import anche `fmt` e `log/slog`).

- [ ] **Step 4: template condivisi**

`web/templates/partials_offices.html`:

```html
{{define "offices_fieldset"}}
<fieldset class="offices">
	<legend>Uffici</legend>
	{{if .Unavailable}}<p class="flash error">Elenco uffici da AD non disponibile: si possono solo togliere quelli già assegnati.</p>{{end}}
	<div class="offices-grid">
		{{range .Choices}}<label class="inline"><input type="checkbox" name="uffici" value="{{.Name}}"{{if .Checked}} checked{{end}}>{{.Name}}{{if .Missing}} <span class="tag warn">non più in AD</span>{{end}}</label>{{end}}
	</div>
	<p class="hint">Nessun ufficio selezionato: visibile a tutti. Con uno o più uffici: solo a chi ne fa parte (gli altri possono scegliere "Mostra tutto").</p>
</fieldset>
{{end}}

{{define "offices_label"}}{{if .}}{{range $i, $o := .}}{{if $i}}, {{end}}{{$o}}{{end}}{{else}}Tutti{{end}}{{end}}
```

`web/static/css/admin.css`, in coda:

```css
.offices-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(190px, 1fr)); gap: .25rem .9rem; margin: .3rem 0; }
```

- [ ] **Step 5: i tre form**

Per **ognuno** di `admin_apps.go` (kind `database.OfficesApp`), `admin_guides.go` (`database.OfficesGuide`), `admin_alerts.go` (`database.OfficesAlert`):

1. Nel form struct aggiungere `Offices []string`.
2. Nella section struct aggiungere `OfficeChoices officesField` e `ItemOffices map[int64][]string`.
3. Nella funzione `xxxData(form, errs)`: dopo aver caricato le righe, `sec.OfficeChoices = s.officeChoices(form.Offices)` e `sec.ItemOffices, err = s.db.Offices(<kind>)` (restituire l'errore come per le altre letture).
4. Nell'handler `…Edit`, dopo aver costruito il form dall'elemento: `form.Offices, err = s.db.OfficesOf(<kind>, id)`; errore → `s.serverError`.
5. Nell'handler `…Save`, dopo la lettura degli altri campi e prima del controllo `len(errs) > 0`:

```go
	var current []string
	if id != 0 {
		if current, err = s.db.OfficesOf(<kind>, id); err != nil {
			s.serverError(w, err)
			return
		}
	}
	form.Offices = s.parseOffices(r, current, errs)
```

   (per le app `r.Form` è già popolato da `ParseMultipartForm`; per guide e avvisi chiamare `r.ParseForm()` prima, se l'handler usa solo `FormValue`, perché `r.Form["uffici"]` sia letto).
6. Dove oggi si fa `CreateX`/`UpdateX`, conservare l'id (`newID, err := s.db.CreateX(...)`; per l'update l'id è `id`) e subito dopo il successo:

```go
	if err := s.db.SetOffices(<kind>, savedID, form.Offices); err != nil {
		s.serverError(w, err)
		return
	}
```

7. Template del form: prima di `<div class="actions">` del form, `{{template "offices_fieldset" .OfficeChoices}}` e, subito dopo, `{{with .Errors.uffici}}<p class="field-error">{{.}}</p>{{end}}`.
8. Template dell'elenco: nella cella del titolo (`admin_app.html` riga con `{{.Title}}`, `admin_guide.html` riga con il link della guida, `admin_avvisi.html` nel template `alert_rows` sotto la fonte) aggiungere `<br><small class="muted">Uffici: {{template "offices_label" (index $.ItemOffices .ID)}}</small>`. Nel template `alert_rows` il `$` è la lista passata: passare la mappa con un campo della riga oppure aggiungere `Offices []string` a `alertRow` popolato in `alertsData` e usare `{{template "offices_label" .Offices}}` (scelta consigliata per gli avvisi).

- [ ] **Step 6: verifica**

Run: `go test ./... && go vet ./... && gofmt -l internal/`
Expected: PASS, compresi i test admin esistenti.

- [ ] **Step 7: commit**

```bash
git add internal/web web/templates web/static/css/admin.css
git commit -m "feat(admin): uffici da AD per applicativi, guide e avvisi"
```

---

### Task 9: verifica manuale e documentazione

**Files:**
- Modify: `CLAUDE.md`
- Modify: `docs/superpowers/specs/2026-10-06-riconoscimento-utente-design.md` (stato)

- [ ] **Step 1: prova manuale (mock)**

```bash
LDAP_HOST=mock NTLM_DOMAIN=COMUNE-MS SECURE_COOKIES=false PORT=18091 DB_PATH=<scratch>/r.db UPLOAD_DIR=<scratch>/up go run ./cmd/server
```

- In admin assegnare "Webmail" (con URL) all'ufficio `TRIBUTI` e creare un avviso solo `AMMINISTRATIVO`.
- Simulare il riconoscimento con PowerShell: `Invoke-WebRequest http://localhost:18091/io -UseDefaultCredentials -SessionVariable s` (oppure Edge headless `--dump-dom` come nella sonda) e poi aprire `/` con la stessa sessione: saluto per nome (in mock il nome è lo username), "Ufficio INFORMATIZZAZIONE", contenuti di TRIBUTI e AMMINISTRATIVO assenti, interruttore con il conteggio; "Mostra tutto" li fa comparire e il pulsante diventa "Mostra solo il mio ufficio".
- 390 px: interruttore leggibile, nessuno scroll orizzontale.

- [ ] **Step 2: CLAUDE.md**

- Architettura, nuovo punto **`internal/identity`**: NTLM (identità **dichiarata**, mai per autorizzare), cookie `cruscotto_utente`, `Concerns`, directory AD/mock; flusso `/io` chiamato da `dashboard.js`; filtro lato server + cookie `cruscotto_tutto`.
- `internal/database`: tabelle `app_offices`, `guide_offices`, `alert_offices` (v5).
- Route pubbliche: aggiungere `/io`.
- Sezione "Identificazione utente": sostituire il testo con i risultati della sonda (nginx `revprx01` → container; `RemoteAddr` sempre `10.89.11.8` per rootlessport → IP inutilizzabile; NTLM automatico da Edge; Kerberos no per SPN mancante e CNAME).
- Variabili: `NTLM_DOMAIN`.
- Debito noto: togliere il punto sul rate limiter per IP.

- [ ] **Step 3: spec**

`Stato: in revisione` → `Stato: implementata`.

- [ ] **Step 4: verifica finale**

Run: `go vet ./... && go test ./... && gofmt -l internal/ cmd/`
Expected: tutto PASS.

- [ ] **Step 5: commit**

```bash
git add CLAUDE.md docs/superpowers/specs/2026-10-06-riconoscimento-utente-design.md
git commit -m "docs: riconoscimento utente e uffici in CLAUDE.md"
```
