# Chat interna — piano di implementazione

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** chat informale 1:1 tra dipendenti dentro CruscottoPA: scrittura con l'identità NTLM dichiarata, lettura solo dopo un login LDAP per browser, messaggi cancellati dopo 30 giorni.

**Architecture:** tabelle nuove in SQLite (migrazione v19) con un file `internal/database/chat.go`; cookie `cruscotto_chat` (verificato) accanto a `cruscotto_utente` (dichiarato); route pubbliche `/chat/*` sempre 200 + JSON; arrivo via l'hub SSE esistente (`Kind: "chat"`) e Web Push senza testo; pubblico della chat definito in `/admin/chat` riusando `content_audience` (`kind = 'chat'`, `content_id = 1`). UI: widget in plancia + `dialog.chat` + pagina `/chat`, tutto costruito da `chat.js` con `textContent`.

**Tech Stack:** Go (`net/http`, `html/template`, `modernc.org/sqlite`, `gorilla/securecookie`, `go-ldap`), HTMX per il widget, JS vanilla.

**Spec:** `docs/superpowers/specs/2026-11-24-chat-interna-design.md`

## Global Constraints

- Ogni route `/chat/*` e `/partials/chat` risponde **200**, anche agli errori (JSON `{"ok":false,"errore":"<codice>"}`); mai 4xx: il proxy li sostituisce.
- Testo dei messaggi **mai** in log, GlitchTip, evento SSE, payload push.
- IP e PC dei messaggi **mai** in nessuna risposta HTTP (né utente né admin).
- Senza cookie chat valido: **nessun testo** di messaggio in nessuna risposta.
- Messaggio: max **2000** caratteri (rune, dopo `TrimSpace`), non vuoto, testo semplice.
- Conservazione **30 giorni** (`chatRetention = 30 * 24 * time.Hour`).
- Cookie chat: **180 giorni**, rinnovato al massimo una volta ogni 24 ore; valido solo se `Username` = username di `cruscotto_utente`.
- Limiti per mittente: **30/minuto**, **300/ora** messaggi; **20/giorno** conversazioni nuove.
- Paginazione thread: **50** messaggi.
- `CHAT_ENABLED` (bool, default `false`) in tre posti: `docker-compose.yml`, `.env.example`, `internal/config`.
- CSP stretta: niente inline; colori solo nei token (`TestThemeCSS`, `TestThemeTokensHaveDark`).
- Nuova route `POST /admin/*` = voce in `auditRoutes` (`TestAuditRoutesCover`).
- Mai modificare migrazioni esistenti; dati di test solo `mrossi`, `example.it`, `192.0.2.x`, `PC-PROVA-001`.
- Test di `internal/web` nel container (vedi CLAUDE.md, «Test»).

## Review Focus

1. **Cookie chat di un altro utente sullo stesso PC** (NTLM `lbianchi`, cookie chat `mrossi`): deve valere come «non verificato», mai mostrare i messaggi di `mrossi`. → test in Task 4.
2. **Mittente bloccato che ricarica il thread**: vede i propri messaggi come prima; il destinatario non li vede, nessun non letto, nessun evento. → test in Task 5.
3. **Utente uscito dal pubblico della chat** con conversazioni aperte: niente invio, niente lettura, niente widget; con AD giù l'invio è rifiutato (`ad`) ma la lettura dei verificati resta. → test in Task 5 e 6.
4. **Testo con HTML/URL ostili** (`<img onerror>`, `javascript:`): JSON lo porta intatto, il JS lo inserisce solo con `textContent`, link solo `http(s)`. → test Go che legge `chat.js` (Task 8).
5. **Gruppo usato dalla chat eliminato dall'admin**: deve essere `ErrInUse` come per le app. → test in Task 1.

---

### Task 1: Schema e accesso ai dati

**Files:**
- Modify: `internal/database/migrations.go` (aggiungere `migrateV19Chat` in coda)
- Create: `internal/database/chat.go`
- Modify: `internal/database/audience.go` (`ContentChat`, `GroupUses`)
- Test: `internal/database/chat_test.go`

**Interfaces:**
- Produces:
  - `const ContentChat ContentKind = "chat"`; `const ChatAudienceID int64 = 1`
  - `type ChatMessage struct { ID, ConversationID int64; Sender, Body string; Verified, Hidden bool; IP, PC string; CreatedAt time.Time }`
  - `type ChatConversation struct { ID int64; Other string; LastAt time.Time; Unread int }` (Other = l'altro partecipante rispetto a chi chiede)
  - `func (db *DB) ChatConversationID(a, b string, create bool) (id int64, created bool, err error)` — coppia ordinata (`user_a < user_b`), username in minuscolo; `create=false` e assente → `ErrNotFound`
  - `func (db *DB) AddChatMessage(m ChatMessage) (int64, error)` — inserisce, aggiorna `last_at`; se `!m.Hidden` incrementa `chat_unread` del destinatario (l'altro partecipante)
  - `func (db *DB) ListChatConversations(user string, limit int) ([]ChatConversation, error)` — ordine `last_at` decrescente; esclude conversazioni in cui l'utente vede zero messaggi (tutti `hidden` scritti dall'altro)
  - `func (db *DB) ChatMessages(convID int64, viewer string, before int64, limit int) ([]ChatMessage, error)` — ordine crescente di ID; `before=0` = più recenti; esclude `hidden=1 AND sender <> viewer`; errore `ErrNotFound` se `viewer` non partecipa
  - `func (db *DB) ChatConversationOther(convID int64, user string) (string, error)` — `ErrNotFound` se non partecipa
  - `func (db *DB) MarkChatRead(user string, convID int64) error`
  - `func (db *DB) ChatUnreadTotal(user string) (int, error)`
  - `func (db *DB) SetChatBlock(blocker, blocked string, on bool) error`; `func (db *DB) ChatBlocked(blocker, blocked string) (bool, error)`; `func (db *DB) ChatBlockedList(blocker string) ([]string, error)`
  - `func (db *DB) CleanupChat(before time.Time) error` — messaggi con `created_at < before`, poi conversazioni senza messaggi; righe `chat_unread` ricalcolate o cancellate di conseguenza
  - `func (db *DB) ChatUsers() ([]string, error)` — tutti gli username che compaiono in conversazioni o blocchi
  - `func (db *DB) DeleteChatUser(user string) error` — conversazioni (cascade messaggi e non letti) e blocchi in cui compare

- [ ] **Step 1: Test**

`chat_test.go` con `newTestDB(t)`:
- `TestChatConversationPairOrdered`: `ChatConversationID("mrossi","abianchi",true)` e `("ABIANCHI","mrossi",false)` → stesso ID, `created` vero solo la prima volta.
- `TestChatHiddenMessages`: `AddChatMessage` con `Hidden:true` da `mrossi` → `ChatMessages(id,"mrossi",0,50)` lo contiene, `ChatMessages(id,"abianchi",0,50)` no; `ChatUnreadTotal("abianchi") == 0`; `ListChatConversations("abianchi",20)` vuota.
- `TestChatUnreadAndRead`: due messaggi visibili da `mrossi` → `ChatUnreadTotal("abianchi") == 2`, dopo `MarkChatRead` 0; `ChatUnreadTotal("mrossi") == 0`.
- `TestChatMessagesPaging`: 60 messaggi → prima pagina 50 (ID 11..60), `before=11` → 10.
- `TestChatNotParticipant`: `ChatMessages(id,"lverdi",0,50)` → `ErrNotFound`.
- `TestCleanupChat`: messaggio a `T-31g` e uno a `T-1g`; `CleanupChat(T-30g)` lascia solo il secondo; conversazione con solo messaggi vecchi sparisce.
- `TestDeleteChatUser`: dopo `DeleteChatUser("mrossi")` nessuna conversazione né blocco con `mrossi`.
- `TestGroupUsedByChat`: `SetContentAudience(ContentChat, ChatAudienceID, {Mode: only, Groups:[g]})` → `DeleteAudienceGroup(g)` restituisce `ErrInUse`; `GroupUses(g)` contiene `"Chat"`.

- [ ] **Step 2: Lanciarli e vederli fallire**

Run: `go test ./internal/database/ -run 'Chat|GroupUsedByChat'`
Expected: FAIL (simboli non definiti).

- [ ] **Step 3: Implementare**

Migrazione v19:
```sql
CREATE TABLE chat_conversations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_a TEXT NOT NULL, user_b TEXT NOT NULL,
	created_at TEXT NOT NULL, last_at TEXT NOT NULL,
	UNIQUE (user_a, user_b), CHECK (user_a < user_b)
);
CREATE TABLE chat_messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	conversation_id INTEGER NOT NULL REFERENCES chat_conversations(id) ON DELETE CASCADE,
	sender TEXT NOT NULL, body TEXT NOT NULL,
	verified INTEGER NOT NULL DEFAULT 0, hidden INTEGER NOT NULL DEFAULT 0,
	ip TEXT NOT NULL DEFAULT '', pc TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);
CREATE INDEX idx_chat_messages_conv ON chat_messages(conversation_id, id);
CREATE INDEX idx_chat_messages_created ON chat_messages(created_at);
CREATE TABLE chat_unread (
	username TEXT NOT NULL,
	conversation_id INTEGER NOT NULL REFERENCES chat_conversations(id) ON DELETE CASCADE,
	count INTEGER NOT NULL,
	PRIMARY KEY (username, conversation_id)
);
CREATE TABLE chat_blocks (
	blocker TEXT NOT NULL, blocked TEXT NOT NULL, created_at TEXT NOT NULL,
	PRIMARY KEY (blocker, blocked)
);
```
Date nel formato del package (testo UTC). `GroupUses`: aggiungere `UNION ALL SELECT 'Chat' FROM content_audience c WHERE c.kind = 'chat' AND c.group_id = ?`. Verificare che `DeleteAudienceGroup` usi già il conteggio di `content_audience` (in quel caso `ErrInUse` arriva da solo).

- [ ] **Step 4: Test verdi**

Run: `go test ./internal/database/`
Expected: PASS (anche i test esistenti sulla versione dello schema).

- [ ] **Step 5: Commit** — `git commit -m "Chat: schema e accesso ai dati"`

---

### Task 2: Verifica LDAP per la chat e cookie verificato

**Files:**
- Modify: `internal/auth/ldap.go`
- Create: `internal/identity/chat.go`
- Modify: `internal/web/server_test.go` (`fakeAuth`)
- Test: `internal/auth/ldap_test.go`, `internal/identity/chat_test.go`

**Interfaces:**
- Produces:
  - `Authenticator` acquista `Verify(username, password string) (ok bool, err error)`; `LDAP.Verify` = stessa validazione di username e bind di `Authenticate`, **senza** controlli di gruppo; in mock `true` con password non vuota.
  - `fakeAuth.Verify` restituisce `f.ok, f.err`.
  - `identity.ChatCookieName = "cruscotto_chat"`, `identity.ChatTTL = 180 * 24 * time.Hour`, `identity.ChatRenewAfter = 24 * time.Hour`
  - `type ChatSession struct { Username string; Issued, Expires int64 }`
  - `func NewChatCodec(secret string) *ChatCodec`; `(c *ChatCodec) Encode(s ChatSession) (string, error)` (imposta `Issued`, `Expires` da `now`); `(c *ChatCodec) Decode(v string) (ChatSession, bool)` (scaduto → false); chiavi `sha256("chat-auth:"+secret)`, `sha256("chat-enc:"+secret)`.

- [ ] **Step 1: Test**
- `TestVerifyMockIgnoresGroups` (auth): config mock con `RequiredGroup` e `AdminGroup` impostati → `Verify("mrossi","x")` ok; password vuota o username `"a b"` → false.
- `TestChatCodecRoundTrip`, `TestChatCodecExpired` (now spostato oltre 180 giorni → false), `TestChatCodecNotUserCookie` (un valore di `CookieCodec.Encode` non decodifica con `ChatCodec`).

- [ ] **Step 2: Fallire** — `go test ./internal/auth/ ./internal/identity/` → FAIL.
- [ ] **Step 3: Implementare** come da Interfaces.
- [ ] **Step 4: Verdi** — `go test ./internal/auth/ ./internal/identity/ && go vet ./...` → PASS.
- [ ] **Step 5: Commit** — `"Chat: verifica LDAP senza gruppi e cookie verificato"`

---

### Task 3: Configurazione, pubblico della chat, pagina admin

**Files:**
- Modify: `internal/config/config.go`, `internal/config/config_test.go`, `docker-compose.yml`, `.env.example`
- Create: `internal/web/chat_access.go`, `internal/web/admin_chat.go`, `web/templates/admin_chat.html`
- Modify: `internal/web/server.go` (campo `chatCookies *identity.ChatCodec`, route), `internal/web/audit.go`, `web/templates/admin_base.html` (voce «Chat», icona `forum`, solo con `chatEnabled`)
- Test: `internal/web/chat_access_test.go`, `internal/web/admin_chat_test.go`

**Interfaces:**
- Consumes: `database.ContentChat`, `ChatAudienceID`, `visibilityField`, `parseVisibility`, `checkGroups`, `groupsFor`.
- Produces:
  - `config.Config.ChatEnabled bool` (`getEnvBool("CHAT_ENABLED", false)`)
  - `func (s *Server) chatEnabled() bool` = `cfg.ChatEnabled && recognitionEnabled()`
  - `func (s *Server) chatAllowed(username string) (allowed, down bool)` — `content_audience` della chat vuota → allowed se `profileFor` ok; altrimenti `audience.Visible(mode, groups, memberOf, known)` con `groupsFor`; AD giù → `false, true`
  - Funzione di template `chatOn` (= `chatEnabled`) per `admin_base.html` e la plancia.
  - Route: `GET /admin/chat` (`handleAdminChatPage`), `POST /admin/chat` (`handleAdminChatSave`, audit `{"Chat","modificato"}`), registrate solo se `chatEnabled()`.

- [ ] **Step 1: Test**
- `TestChatEnabledFlag` (config): assente → false; `"true"` → true; `"boh"` → errore.
- `TestChatAllowedPublic`: senza righe → `chatAllowed("mrossi") == (true,false)`; utente sconosciuto alla directory → false.
- `TestChatAllowedGroups`: gruppo con regola che include `mrossi`, `only` → `mrossi` true, `senzanome` false; `hide` → invertito.
- `TestAdminChatSave`: login admin, `POST /admin/chat` con `visibilita=only&gruppi=<id>` → 200, `GetContentAudience(ContentChat,1)` aggiornato, una riga in `admin_log` sezione `Chat`; senza gruppi → `invalid(rec)`.
- `TestChatRoutesOffWhenDisabled`: `ChatEnabled=false` → `GET /admin/chat` 404 e nessuna voce «Chat» nel menu.

- [ ] **Step 2: Fallire** (container) — `go test ./internal/config/ ./internal/web/ -run 'Chat'` → FAIL.
- [ ] **Step 3: Implementare**. Pagina admin: shell `admin_top`/`admin_bottom`, sezione `chat_section` con `visibility_fieldset` e una frase che spiega «Chi è fuori non può scrivere né ricevere messaggi». Nei test abilitare con `newTestServerWith(..., func(o *Options){ o.Config.ChatEnabled = true })`.
- [ ] **Step 4: Verdi** — `go test ./...` in container → PASS (incluso `TestAuditRoutesCover`).
- [ ] **Step 5: Commit** — `"Chat: interruttore, pubblico e pagina admin"`

---

### Task 4: Accesso alla chat (login LDAP del browser)

**Files:**
- Create: `internal/web/chat_session.go`
- Modify: `internal/web/server.go` (route)
- Test: `internal/web/chat_session_test.go`

**Interfaces:**
- Consumes: `identity.ChatCodec`, `auth.Authenticator.Verify`, `s.limiter`, `viewerName`, `chatAllowed`.
- Produces:
  - `type chatViewer struct { Username, Name string; Verified bool }`
  - `func (s *Server) chatViewer(w http.ResponseWriter, r *http.Request) (chatViewer, bool)` — false se chat spenta, anonimo o `chatAllowed` falso (con AD giù: `true` solo se verificato, così la lettura resta); `Verified` = cookie chat valido **e** `Username` uguale; se `Issued` è più vecchio di `ChatRenewAfter` riscrive il cookie (scadenza rinnovata)
  - `POST /chat/accesso` (`username`, `password`) → `{"ok":true}` | `{"ok":false,"errore":"credenziali"|"altro_utente"|"bloccato"|"ad"|"anonimo"}`; chiavi rate limit `"chat:"+username`
  - `POST /chat/esci` → cancella il cookie, `{"ok":true}`
  - Cookie: `Path=/`, `HttpOnly`, `SameSite=Lax`, `Secure` = `secureRequest(r)`, `MaxAge` = `ChatTTL`

- [ ] **Step 1: Test** (cookie utente da `s.cookies.Encode(identity.User{Username:"mrossi", Name:"Mario Rossi"})`)
- `TestChatLoginOK`: `POST /chat/accesso` come `mrossi`/`mrossi` → `ok`, cookie `cruscotto_chat` impostato.
- `TestChatLoginOtherUser`: NTLM `mrossi`, login `abianchi` → `altro_utente`, nessun cookie.
- `TestChatLoginWrongPassword`: `fakeAuth{ok:false}` → `credenziali`; dopo 5 → `bloccato`.
- `TestChatCookieOtherNTLMUser` (Review Focus 1): cookie chat di `mrossi` con cookie utente `abianchi` → `chatViewer.Verified == false`.
- `TestChatCookieRenewed`: `Issued` di 2 giorni fa → risposta con nuovo `Set-Cookie`; di 1 ora fa → nessuno.
- `TestChatLoginAlways200`: ogni esito sopra con `rec.Code == 200`.

- [ ] **Step 2: Fallire** → FAIL. **Step 3: Implementare.** **Step 4: Verdi** in container.
- [ ] **Step 5: Commit** — `"Chat: accesso con le credenziali di dominio"`

---

### Task 5: Invio dei messaggi e notifiche

**Files:**
- Create: `internal/web/chat_send.go`, `internal/web/chat_limits.go`
- Modify: `internal/web/server.go` (route, campo `chatLimits *chatLimits`), `internal/errtrack/scrub.go` (chiavi personali `body`, `testo`, `chat`)
- Test: `internal/web/chat_send_test.go`

**Interfaces:**
- Consumes: Task 1 (DB), Task 4 (`chatViewer`), `s.directory.Lookup`, `chatAllowed`, `s.realIP`, `s.hub.Broadcast`, `s.pusher`, `db.ListPushSubscriptionsFor`.
- Produces:
  - `POST /chat/messaggi` (`a` = username destinatario, `testo`) → `{"ok":true,"conversazione":<id>,"id":<msgID>}` | `{"ok":false,"errore":"anonimo"|"destinatario"|"vuoto"|"lungo"|"limite"|"ad"}`
  - `type chatLimits` con `allowMessage(user string, now time.Time) bool` (30/min, 300/h) e `allowNewConversation(user string, now time.Time) bool` (20 al giorno di Roma); in memoria, potatura delle chiavi vecchie.
  - `func (s *Server) notifyChat(ctx context.Context, to string, convID int64, fromName string, verified bool)` — evento `notify.Event{ID: convID, Kind: "chat", Title: "Nuovo messaggio da "+fromName (+ " (non verificato)"), URL: "/chat?c=<id>"}` a `to` e, senza titolo né push, al mittente stesso (per gli altri suoi browser); push con `{"title", "body": "", "url", "tag": "chat-<id>"}`.
- Regole dell'invio, nell'ordine: viewer valido (`chatViewer`) → testo (`vuoto`/`lungo`) → limiti → destinatario: minuscolo, ≠ mittente, `Lookup` ok (`ErrUnknownUser` → `destinatario`, altro errore → `ad`), `chatAllowed(dest)` (falso → `destinatario`, down → `ad`) → blocco (`ChatBlocked(dest, mittente)` → `Hidden: true`, risposta `ok` identica, nessuna notifica) → `AddChatMessage` con `Verified`, `IP = realIP(r)`, `PC` dal cookie utente.

- [ ] **Step 1: Test**
- `TestChatSendDeclared`: solo cookie utente → `ok`, messaggio salvato con `Verified=false`, `IP="192.0.2.15"` (header `X-Real-Ip`), `PC="PC-PROVA-001"`.
- `TestChatSendVerified`: con cookie chat → `Verified=true`.
- `TestChatSendBlocked` (Review Focus 2): `abianchi` blocca `mrossi`; `mrossi` scrive → `ok`, `Hidden=true`, `ChatUnreadTotal("abianchi")==0`, nessun evento ricevuto da un client `hub.Subscribe("abianchi")`.
- `TestChatSendRecipientOutside` (Review Focus 3): destinatario fuori dal pubblico → `destinatario`; mittente fuori → `anonimo`.
- `TestChatSendADDown`: directory che restituisce errore su `Lookup` → `ad`.
- `TestChatSendValidation`: `""`, 2001 rune, a se stessi → `vuoto`, `lungo`, `destinatario`; 2000 rune `"è"` → `ok`.
- `TestChatSendLimits`: 31° messaggio nel minuto → `limite`; 21ª conversazione nuova del giorno → `limite`.
- `TestChatEventNoText`: client hub di `abianchi` riceve `Kind=="chat"`, `Body==""`, titolo senza il testo; il payload del pusher finto non contiene il testo.

- [ ] **Step 2: Fallire.** **Step 3: Implementare.** **Step 4: Verdi** in container.
- [ ] **Step 5: Commit** — `"Chat: invio dei messaggi e notifiche"`

---

### Task 6: Lettura, non letti, blocchi, ricerca colleghi

**Files:**
- Create: `internal/web/chat_read.go`
- Modify: `internal/web/server.go`, `internal/web/rubrica.go` (campo `chat` nelle persone)
- Test: `internal/web/chat_read_test.go`, `internal/web/rubrica_test.go`

**Interfaces:**
- Consumes: Task 1, Task 4, `s.directory.SearchUsers`, `s.directory.Lookup`.
- Produces (tutte 200 + JSON; senza `Verified` → `{"ok":false,"errore":"accesso"}` tranne dove indicato):
  - `GET /chat/conversazioni` → `{"ok":true,"conversazioni":[{"id","con","nome","ultimo","nuovi"}]}` (`nome` da `Lookup` con cache dei profili; `ultimo` = RFC 3339)
  - `GET /chat/conversazioni/{id}?prima=<msgID>` → `{"ok":true,"con","nome","bloccato":bool,"messaggi":[{"id","mio":bool,"testo","verificato":bool,"quando"}],"altri":bool}`; partecipante assente → `errore: "conversazione"`. **Mai `ip`/`pc`.**
  - `GET /chat/con/{username}` → come sopra per la conversazione con quel collega (`conversazione` assente = lista vuota, `id` 0)
  - `POST /chat/conversazioni/{id}/letto` → `MarkChatRead`
  - `POST /chat/blocchi` (`utente`, `blocca=1|0`) → `SetChatBlock`
  - `GET /chat/persone?q=` (anche non verificati: serve per scrivere) → `{"ok":true,"persone":[{"utente","nome"}]}`: `SearchUsers`, max 8, solo `chatAllowed`, escluso se stesso; `q` < 2 caratteri → lista vuota
  - `GET /chat/stato` (anche non verificati) → `{"ok":true,"verificato":bool,"nuovi":<int>}`; `nuovi` = `ChatUnreadTotal` (solo il numero, anche ai non verificati: niente nomi né testo)
  - Rubrica: `rubrica.Person` in risposta avvolta in `struct{ rubrica.Person; Chat bool \`json:"chat"\` }`; `Chat` = chat attiva, viewer ammesso e `chatAllowed(p.UID)`.

- [ ] **Step 1: Test**
- `TestChatReadNeedsVerified`: senza cookie chat ogni GET di lettura → `accesso` e il corpo non contiene il testo del messaggio di prova.
- `TestChatReadNoIPOrPC`: con cookie chat, il JSON del thread non contiene `192.0.2.15` né `PC-PROVA-001`.
- `TestChatReadOtherConversation`: `lverdi` verificato chiede la conversazione `mrossi`–`abianchi` → `conversazione`.
- `TestChatReadADDownStillWorks` (Review Focus 3): directory giù, verificato → `ok`.
- `TestChatStatoCount`: non verificato con 2 non letti → `nuovi: 2`, nessun altro campo.
- `TestChatPersoneFiltered`: utente fuori dal pubblico non compare; se stesso non compare.
- `TestRubricaChatFlag`: chat attiva → `"chat":true` per un collega ammesso; chat spenta → `false`.

- [ ] **Step 2: Fallire.** **Step 3: Implementare.** **Step 4: Verdi** in container.
- [ ] **Step 5: Commit** — `"Chat: lettura, blocchi e ricerca dei colleghi"`

---

### Task 7: Pulizia

**Files:**
- Modify: `internal/web/presence.go` (`StartPresenceCleanup`, `CleanupPresence`)
- Test: `internal/web/presence_test.go`

**Interfaces:**
- Consumes: `CleanupChat`, `ChatUsers`, `DeleteChatUser`, `chatRetention`.
- Produces: `func (s *Server) cleanupChat()` chiamata nel giro giornaliero: `CleanupChat(now - 30g)` sempre (anche a chat spenta: i dati non restano oltre i 30 giorni); poi per ogni `ChatUsers()` `Lookup` → `ErrUnknownUser` = `DeleteChatUser`, altro errore = stop del giro.

- [ ] **Step 1: Test** — `TestCleanupChatRetention` (messaggio a 31 giorni sparisce, a 29 resta); `TestCleanupChatUnknownUser` (utente che la directory non conosce → via conversazioni e blocchi; directory giù → niente).
- [ ] **Step 2–4**: fallire, implementare, verdi in container.
- [ ] **Step 5: Commit** — `"Chat: pulizia a 30 giorni e utenti usciti da AD"`

---

### Task 8: Interfaccia (widget, pannello, pagina, notifiche, ricerca)

**Files:**
- Create: `web/templates/partials_chat.html` (`widget_chat`, `chat_dialog`), `web/templates/chat.html` (pagina `/chat`), `web/static/js/chat.js`, `internal/web/chat_page.go`
- Modify: `web/templates/dashboard.html` (widget dopo `widget_assist`, dialog in fondo, solo con `chatOn` e viewer ammesso), `web/templates/partials_brand.html` o il template di `plancia_head` (script `chat.js`), `web/static/js/notifiche.js` (evento `chat`), `web/static/js/dashboard.js` (voce «Scrivi» nel gruppo Persone), `web/static/css/plancia.css` (token chiari e scuri), `internal/web/dashboard.go` (dato `Chat` nella vista)
- Test: `internal/web/chat_page_test.go`

**Interfaces:**
- Consumes: tutte le route di Task 4–6.
- Produces:
  - `GET /partials/chat` → `widget_chat` (HTMX, `hx-trigger="every 300s, chat from:body"`): titolo «Messaggi», badge non letti, bottone «Nuovo messaggio», e per i verificati le ultime 5 conversazioni (nome + «N nuovi»); per i non verificati solo il numero e «Accedi per leggerli».
  - `GET /chat` (pagina; `?c=<id>` o `?a=<username>` apre quella conversazione); non ammesso → pagina con «La chat non è disponibile» (200).
  - Markup che `chat.js` aggancia: `dialog.chat` con `[data-chat-list]`, `[data-chat-thread]`, `[data-chat-form]`, `[data-chat-login]`, `[data-chat-notice]`; apertura da qualunque elemento `[data-chat-open]` (valore = username o vuoto).
  - Avviso fisso, testo esatto: «Chat informale tra colleghi. Non usarla per comunicazioni ufficiali né per dati personali di cittadini. I messaggi si cancellano dopo 30 giorni. Per i casi di abuso restano registrati PC e indirizzo di rete di chi scrive: apri un ticket all'assistenza.»
  - Riquadro login, testo: «Per leggere i messaggi accedi con le credenziali di Windows (una volta sola su questo PC).»
  - Etichetta messaggi non verificati: «⚠ non verificato», `title`: «Scritto prima dell'accesso alla chat: non possiamo garantire che l'abbia scritto davvero lui o lei».
  - `notifiche.js`: listener `chat` come `ticket` (dispatch `Event("chat")` su `body`, notifica con `tag: "chat-"+id` se la scheda non è visibile, inoltro via `BroadcastChannel` con `kind: "chat"`).
  - `dashboard.js`: per ogni persona con `chat: true` una seconda voce subito sotto, icona `chat`, testo «Scrivi a <nome>», `href="/chat?a=<uid>"`, `data-chat-open="<uid>"` (se il dialog è in pagina `chat.js` intercetta il clic e apre il pannello).
  - `chat.js`: testo dei messaggi solo via `textContent`; link resi spezzando il testo su `https?://\S+` e creando `<a>` con `href` validato da `new URL()` e protocollo `http:`/`https:`, `target="_blank"`, `rel="noopener noreferrer"`; contatore caratteri (2000); invio con Invio, a capo con Maiuscolo+Invio; errori con messaggio generico per codice (mai il corpo della risposta); dopo un login riuscito ricarica il pannello; «Blocca/Sblocca» nel menu del thread; `POST …/letto` quando il thread è visibile.

- [ ] **Step 1: Test**
- `TestChatWidgetDeclared`: widget per non verificato con 2 non letti → contiene `2` e «Accedi per leggerli», nessun nome di mittente.
- `TestChatWidgetHiddenOutside`: viewer fuori dal pubblico → la plancia non contiene `widget-chat` né `chat.js` attivo (`data-chat` assente).
- `TestChatNoticeText`: la plancia contiene l'avviso fisso esatto.
- `TestChatJSSafeDOM` (Review Focus 4): il sorgente di `chat.js` non contiene `innerHTML`, `insertAdjacentHTML`, `outerHTML`, `document.write`; contiene `textContent` e il controllo `protocol` su `http:`/`https:`.
- `TestNotificheJSChatEvent`: `notifiche.js` contiene `addEventListener("chat"` e `"chat-"`.
- I test esistenti `TestThemeCSS` e `TestThemeTokensHaveDark` devono restare verdi con il CSS nuovo.

- [ ] **Step 2: Fallire.** **Step 3: Implementare.** Sintassi JS: `node -e "new Function(require('fs').readFileSync('web/static/js/chat.js','utf8'))"` (e così per `notifiche.js`, `dashboard.js`).
- [ ] **Step 4: Verdi** in container (`go test ./...`).
- [ ] **Step 5: Prova nel browser** — Playwright in container (vedi CLAUDE.md), mock con `CHAT_ENABLED=true`, due cookie (`mrossi`, `abianchi` generati con la sonda `cmd/cookieprobe`, da cancellare): `mrossi` scrive senza login → `abianchi` vede badge 1, fa l'accesso, legge con «⚠ non verificato»; risposta; blocco. Screenshot chiaro e scuro.
- [ ] **Step 6: Commit** — `"Chat: widget, pannello e notifiche"`

---

### Task 9: Documentazione

**Files:**
- Modify: `CLAUDE.md` (progetto: chat fatta; Architettura: tabelle v19, route pubbliche `/chat/*`, `/partials/chat`, cookie `cruscotto_chat`, `CHAT_ENABLED`, regole su testo/IP/PC), `publiccode.yml` solo al rilascio.

- [ ] **Step 1:** aggiornare `CLAUDE.md` in modo conciso, nello stile delle sezioni esistenti.
- [ ] **Step 2:** `go build ./... && go vet ./...` e test completi in container → PASS.
- [ ] **Step 3: Commit** — `"Chat: documentazione"`
