# Riconoscimento utente — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** riconoscere via NTLM (identità dichiarata) chi apre la plancia da un PC del dominio e salutarlo per nome. I filtri sui contenuti sono una fase successiva (spec §9), configurabile da interfaccia.

**Architecture:** nuovo pacchetto `internal/identity` (parser NTLM, cookie firmato dell'utente, directory LDAP/mock che legge solo il nome). Endpoint `/io` chiamato in background da `dashboard.js` fa l'handshake e salva il cookie; la plancia saluta per nome.

**Tech Stack:** Go 1.26 (`net/http`, `html/template`), `github.com/go-ldap/ldap/v3`, `github.com/gorilla/securecookie`, SQLite `modernc.org/sqlite`, JS senza dipendenze.

**Spec:** `docs/superpowers/specs/2026-10-06-riconoscimento-utente-design.md`

## Global Constraints

- **Identità dichiarata, mai usata per autorizzare**: il cookie utente non dà accesso a `/admin` né ad alcuna azione. Scriverlo nei commenti di `identity` e di `/io`.
- Nessun criterio di filtro nel codice: di AD si legge solo il nome (`displayName`).
- `NTLM_DOMAIN` vuota = riconoscimento disattivato; attivo solo se c'è anche una directory (`LDAP_BIND_DN` impostato oppure `LDAP_HOST=mock`).
- Cookie `cruscotto_utente`: `Path=/`, `HttpOnly`, `SameSite=Lax`, `Secure` come il cookie admin; 30 giorni se riconosciuto, 24 ore se anonimo.
- `/io`: `Cache-Control: no-store`; 404 con riconoscimento disattivato.
- Nuova env var in tre posti: `docker-compose.yml`, `.env.example`, codice.
- CSP invariata: niente script/stili inline, niente `on*=`.
- Testi in italiano. `go test ./...`, `go vet ./...`, `gofmt -l internal/` vuoto.

## Review Focus

- Messaggio NTLM ostile (offset/lunghezze oltre il buffer, overflow di `offset+len`, lunghezza dispari): mai panic, risposta 400 → fuzz e casi in Task 2.
- Cookie utente valido ma richiesta a `/admin`: deve restare 303 al login → test in Task 5.
- Utente riconosciuto senza `displayName` in AD: solo il saluto, nessuna virgola orfana → test in Task 6.
- Browser che non completa NTLM (PC fuori dominio): cookie anonimo, nessun nuovo tentativo per 24 ore → test in Task 5 e 6.
- `NTLM_DOMAIN` impostato senza `LDAP_BIND_DN`: riconoscimento spento con warning, plancia normale → `TestIoDisabled`/wiring in Task 5.

> Revisione 2026-10-06: su richiesta dell'utente i filtri per ufficio (ex task 5, 7, 8) sono rimandati alla fase successiva, con criterio (gruppi AD o attributo) e regole "mostra solo a"/"nascondi a" configurati da interfaccia. Il Task 4 rimuove ciò che il Task 3 aveva aggiunto per i filtri.

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
	got, err := ParseAuthenticate(ntlmtest.Authenticate("COMUNE-MS", "mrossi", "PC-PROVA-001"))
	want := Login{Domain: "COMUNE-MS", User: "mrossi", Workstation: "PC-PROVA-001"}
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

### Task 4: directory AD (solo nome) e pulizia di `identity`

> Revisione 2026-10-06: i filtri sui contenuti passano alla fase successiva (spec §9). Questo task toglie da `identity` ciò che il Task 3 aveva aggiunto per i filtri (`User.Office`, `Concerns`).

**Files:**
- Modify: `internal/auth/ldap.go` (esportare `Dial`)
- Create: `internal/identity/directory.go`, `internal/identity/directory_test.go`
- Modify: `internal/identity/user.go`, `internal/identity/user_test.go` (via `Office` e `Concerns`)

**Interfaces:**
- Consumes: `config.LDAP`.
- Produces:
  - `auth.Dial(cfg config.LDAP) (*ldap.Conn, error)`
  - `identity.Person{Username, Name string}`
  - `identity.Directory` interface: `Lookup(username string) (Person, error)`
  - `identity.ErrUnknownUser`, `identity.NewLDAPDirectory(cfg config.LDAP) *LDAPDirectory`, `identity.MockDirectory{}`
  - `identity.User{Username, Name string; Anonymous bool}` (senza `Office`)

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
	f, err := userFilter("mrossi")
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
	if err != nil || p != (Person{Username: "mrossi", Name: "MRossi"}) {
		t.Fatalf("Lookup: %+v %v", p, err)
	}
	if _, err := d.Lookup("a b"); !errors.Is(err, ErrUnknownUser) {
		t.Fatal("username non valido accettato")
	}
}
```

In `user_test.go` togliere `TestConcerns` e il campo `Office` dall'utente di `TestCookieRoundTrip`.

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/identity/`
Expected: errori di compilazione (`userFilter`, `MockDirectory` non definiti).

- [ ] **Step 3: `auth.Dial`**

In `internal/auth/ldap.go` trasformare il metodo `dial` in funzione esportata `Dial(cfg config.LDAP) (*ldap.Conn, error)` (stesso corpo, `l.cfg` → `cfg`, commento: "stesse regole TLS del login admin, StartTLS fallito = errore") e in `Authenticate` usare `Dial(l.cfg)`.

- [ ] **Step 4: directory e pulizia**

`internal/identity/directory.go`:

```go
package identity

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-ldap/ldap/v3"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/auth"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

// ErrUnknownUser: utente inesistente, disattivato o con nome non ammesso.
var ErrUnknownUser = errors.New("identity: utente non trovato in AD")

// Person è un utente di AD come serve alla plancia.
type Person struct {
	Username string // sAMAccountName, minuscolo
	Name     string // displayName, "" se assente
}

// Directory cerca gli utenti riconosciuti via NTLM.
type Directory interface {
	Lookup(username string) (Person, error)
}

// Stessa regola dello username del login admin.
var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,128}$`)

// userFilter cerca per sAMAccountName (il nome che NTLM trasmette), solo utenti attivi.
func userFilter(username string) (string, error) {
	if !usernameRe.MatchString(username) {
		return "", ErrUnknownUser
	}
	return fmt.Sprintf("(&(objectCategory=person)(objectClass=user)(!(userAccountControl:1.2.840.113556.1.4.803:=2))(sAMAccountName=%s))",
		ldap.EscapeFilter(username)), nil
}

// LDAPDirectory interroga AD con l'account di servizio (LDAP_BIND_DN).
type LDAPDirectory struct{ cfg config.LDAP }

func NewLDAPDirectory(cfg config.LDAP) *LDAPDirectory { return &LDAPDirectory{cfg: cfg} }

func (d *LDAPDirectory) Lookup(username string) (Person, error) {
	filter, err := userFilter(username)
	if err != nil {
		return Person{}, err
	}
	conn, err := auth.Dial(d.cfg)
	if err != nil {
		return Person{}, err
	}
	defer conn.Close()
	if err := conn.Bind(d.cfg.BindDN, d.cfg.BindPassword); err != nil {
		return Person{}, fmt.Errorf("ldap bind di servizio: %w", err)
	}
	res, err := conn.Search(ldap.NewSearchRequest(d.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, 5, false,
		filter, []string{"sAMAccountName", "displayName"}, nil))
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
	}, nil
}

// MockDirectory: per LDAP_HOST=mock (solo sviluppo).
type MockDirectory struct{}

func (MockDirectory) Lookup(username string) (Person, error) {
	if !usernameRe.MatchString(username) {
		return Person{}, ErrUnknownUser
	}
	return Person{Username: strings.ToLower(username), Name: username}, nil
}
```

In `user.go`: togliere il campo `Office` e la funzione `Concerns`.

- [ ] **Step 5: verifica**

Run: `go test ./internal/identity/ ./internal/auth/ && go vet ./... && gofmt -l internal/`
Expected: PASS.

- [ ] **Step 6: commit**

```bash
git add internal/auth/ldap.go internal/identity
git commit -m "feat(identity): directory AD per il nome dell'utente; filtri rimandati"
```

---

### Task 5: endpoint `/io` e cookie dell'utente nel server

**Files:**
- Modify: `internal/web/server.go` (Options/Server: `Directory`, `directory`, `cookies`; route)
- Create: `internal/web/identity.go`, `internal/web/identity_test.go`
- Modify: `internal/web/server_test.go` (directory finta e `NTLMDomain` nei test)
- Modify: `cmd/server/main.go`

**Interfaces:**
- Consumes: `identity.*` (Task 2–4), `config.Config.NTLMDomain` (Task 1).
- Produces: `web.Options.Directory identity.Directory`; `s.recognitionEnabled() bool`; `s.viewer(r) (identity.User, bool)`; `s.setViewer(w, r, u)`; `s.cookies *identity.CookieCodec`; route `GET /io`; nei test `fakeDirectory{people map[string]identity.Person; err error}` e `testDirectory`.

- [ ] **Step 1: directory finta nei test**

In `internal/web/server_test.go`, vicino a `fakeAuth`:

```go
type fakeDirectory struct {
	people map[string]identity.Person
	err    error
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

var testDirectory = fakeDirectory{people: map[string]identity.Person{
	"mrossi":  {Username: "mrossi", Name: "Mario Rossi"},
	"senzanome": {Username: "senzanome"},
}}
```

In `newTestServerWith`, nelle `Options` di default `Directory: testDirectory,` e nella `config.Config` `NTLMDomain: "COMUNE-MS",`.

- [ ] **Step 2: test che falliscono**

`internal/web/identity_test.go`:

```go
package web

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity/ntlmtest"
)

func ntlmHeader(msg []byte) map[string]string {
	return map[string]string{"Authorization": "NTLM " + base64.StdEncoding.EncodeToString(msg)}
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
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
	if c := cookieNamed(rec, identity.CookieName); c == nil || c.MaxAge != int(identity.AnonymousTTL.Seconds()) {
		t.Fatal("passo 0: atteso il cookie anonimo da 24 ore")
	}

	rec = do(t, s, "GET", "/io", nil, nil, ntlmHeader(ntlmtest.Negotiate()))
	if rec.Code != http.StatusUnauthorized || !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "NTLM ") {
		t.Fatalf("tipo 1: %d %q", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}

	rec = do(t, s, "GET", "/io", nil, nil, ntlmHeader(ntlmtest.Authenticate("comune-ms", "MRossi", "PC-1")))
	c := cookieNamed(rec, identity.CookieName)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"riconosciuto":true`) || !strings.Contains(rec.Body.String(), `"nome":"Mario"`) || c == nil {
		t.Fatalf("tipo 3: %d %s", rec.Code, rec.Body)
	}
	if !c.HttpOnly || c.Path != "/" || c.SameSite != http.SameSiteLaxMode || c.MaxAge != int(identity.UserTTL.Seconds()) {
		t.Fatalf("attributi del cookie: %+v", c)
	}
	if u, ok := s.cookies.Decode(c.Value); !ok || u.Name != "Mario Rossi" || u.Anonymous {
		t.Fatalf("contenuto del cookie: %+v %v", u, ok)
	}
}

func TestIoRejections(t *testing.T) {
	s, _ := newTestServer(t, nil)
	anon := func(rec *httptest.ResponseRecorder) bool {
		c := cookieNamed(rec, identity.CookieName)
		if c == nil {
			return false
		}
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
	if rec.Code != http.StatusServiceUnavailable || cookieNamed(rec, identity.CookieName) != nil {
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
	v, _ := s.cookies.Encode(identity.User{Username: "mrossi", Name: "Mario Rossi"})
	rec := do(t, s, "GET", "/admin", nil, &http.Cookie{Name: identity.CookieName, Value: v}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("/admin con il solo cookie utente: atteso 303, ottenuto %d", rec.Code)
	}
}
```

- [ ] **Step 3: verifica che falliscano**

Run: `go test ./internal/web/ -run 'Io|ViewerCookie'`
Expected: errori di compilazione (`s.cookies`, `Options.Directory`).

- [ ] **Step 4: server**

`internal/web/server.go`: in `Options` `Directory identity.Directory // nil = riconoscimento spento`; in `Server` `directory identity.Directory` e `cookies *identity.CookieCodec`; in `New` `directory: o.Directory,` e, dopo `s.store = …`, `s.cookies = identity.NewCookieCodec(o.Config.SessionSecret)`; in `routes()` dopo `/health`: `s.mux.HandleFunc("GET /io", s.handleIo)`.

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
// chi è l'utente (nome da AD). Il nome NTLM NON è verificato.
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
		s.recognized(w, r, identity.User{Username: p.Username, Name: p.Name})
	}
}

func (s *Server) recognized(w http.ResponseWriter, r *http.Request, u identity.User) {
	s.setViewer(w, r, u)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"riconosciuto": !u.Anonymous, "nome": u.FirstName()})
}
```

`cmd/server/main.go`, prima di costruire le `web.Options`:

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

e `Directory: directory,` nel letterale.

- [ ] **Step 5: verifica**

Run: `go test ./... && go vet ./... && gofmt -l internal/ cmd/`
Expected: PASS.

- [ ] **Step 6: commit**

```bash
git add internal/web cmd/server/main.go
git commit -m "feat(web): /io riconosce l'utente via NTLM e salva il nome nel cookie"
```

---

### Task 6: saluto per nome in plancia

**Files:**
- Modify: `internal/web/dashboard.go`, `web/templates/dashboard.html`, `web/static/js/dashboard.js`
- Test: `internal/web/identity_test.go`

**Interfaces:**
- Consumes: `s.viewer`, `s.recognitionEnabled`, `s.cookies` (Task 5).
- Produces: in `dashboardView` i campi `User identity.User` e `Recognize bool`.

- [ ] **Step 1: test che falliscono**

In `internal/web/identity_test.go`:

```go
func TestDashboardGreetsRecognizedUser(t *testing.T) {
	s, _ := newTestServer(t, nil)
	v, _ := s.cookies.Encode(identity.User{Username: "mrossi", Name: "Mario Rossi"})
	body := do(t, s, "GET", "/", nil, &http.Cookie{Name: identity.CookieName, Value: v}, nil).Body.String()
	if !strings.Contains(body, `<span class="hello-name">, Mario</span>`) || strings.Contains(body, "data-riconosci") {
		t.Fatal("utente riconosciuto: saluto per nome e nessun nuovo tentativo")
	}
	v, _ = s.cookies.Encode(identity.User{Username: "senzanome"})
	if body := do(t, s, "GET", "/", nil, &http.Cookie{Name: identity.CookieName, Value: v}, nil).Body.String(); !strings.Contains(body, `<span class="hello-name"></span>`) {
		t.Fatal("senza nome visualizzato: solo il saluto")
	}
}

func TestDashboardRecognizeAttribute(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if body := do(t, s, "GET", "/", nil, nil, nil).Body.String(); !strings.Contains(body, "data-riconosci") {
		t.Fatal("senza cookie: data-riconosci atteso")
	}
	v, _ := s.cookies.Encode(identity.User{Anonymous: true})
	if body := do(t, s, "GET", "/", nil, &http.Cookie{Name: identity.CookieName, Value: v}, nil).Body.String(); strings.Contains(body, "data-riconosci") {
		t.Fatal("con il cookie anonimo non si ritenta")
	}
	s2, _ := newTestServerWith(t, nil, func(o *Options) { o.Config.NTLMDomain = "" })
	if body := do(t, s2, "GET", "/", nil, nil, nil).Body.String(); strings.Contains(body, "data-riconosci") {
		t.Fatal("riconoscimento spento: nessun tentativo")
	}
}
```

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/web/ -run 'Greets|RecognizeAttribute'`
Expected: FAIL (saluto e attributo assenti).

- [ ] **Step 3: implementazione**

`internal/web/dashboard.go`: in `dashboardView` aggiungere `User identity.User` e `Recognize bool`; in `handleDashboard`, prima del render, `u, known := s.viewer(r)` e nel letterale `User: u, Recognize: !known && s.recognitionEnabled(),`.

`web/templates/dashboard.html`:
- `<body class="plancia">` → `<body class="plancia"{{if .Recognize}} data-riconosci{{end}}>`
- `<span class="hello-name"></span>` → `<span class="hello-name">{{with .User.FirstName}}, {{.}}{{end}}</span>`

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
```

- [ ] **Step 4: verifica**

Run: `go test ./... && go vet ./... && gofmt -l internal/`
Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add internal/web web/templates/dashboard.html web/static/js/dashboard.js
git commit -m "feat(plancia): saluto per nome dell'utente riconosciuto"
```

---

### Task 7: verifica manuale e documentazione

**Files:**
- Modify: `CLAUDE.md`
- Modify: `docs/superpowers/specs/2026-10-06-riconoscimento-utente-design.md` (stato)

- [ ] **Step 1: prova manuale (mock)**

Avviare `LDAP_HOST=mock NTLM_DOMAIN=<dominio del PC> SECURE_COOKIES=false PORT=18091 DB_PATH=<scratch>/r.db UPLOAD_DIR=<scratch>/up` e aprire `http://localhost:18091/` con Edge headless (`--dump-dom`, come nella sonda) o con il browser: dopo la ricarica il saluto contiene lo username (in mock il nome è lo username). Con `NTLM_DOMAIN` diverso dal dominio del PC: nessun saluto, cookie anonimo.

- [ ] **Step 2: CLAUDE.md**

- Architettura, nuovo punto **`internal/identity`**: NTLM (identità **dichiarata**, mai per autorizzare), cookie `cruscotto_utente`, directory AD (solo nome) e mock; flusso `/io` chiamato da `dashboard.js`.
- Route pubbliche: aggiungere `/io`.
- Sezione "Identificazione utente": risultati della sonda (nginx `revprx01` → container; `RemoteAddr` sempre `10.89.11.8` con rootlessport → IP inutilizzabile; NTLM automatico da Edge; Kerberos no per SPN mancante e CNAME) e rimando alla spec §9 per i filtri.
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
git commit -m "docs: riconoscimento utente in CLAUDE.md"
```
