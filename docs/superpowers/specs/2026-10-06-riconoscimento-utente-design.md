# Riconoscimento utente

Data: 2026-10-06 · Stato: implementata · Sotto-progetto 4, prima parte: riconoscimento e saluto

## Contesto e obiettivo

La plancia oggi non sa chi la sta guardando. Per personalizzarla (saluto oggi, filtri sui contenuti nella fase successiva) serve riconoscere l'utente senza fargli fare login.

**Obiettivo di questa parte:** riconoscere automaticamente chi apre la plancia da un PC del dominio e salutarlo per nome. I **filtri sui contenuti** sono la fase successiva (§9): criterio e regole si configureranno dall'interfaccia admin, nulla scritto nel codice.

## Cosa ha mostrato la sonda in produzione (2026-10-06)

Sonda usa-e-getta `/debug/whoami` su `cruscottopa.comune.montesilvano.pe.it`, branch `spike/whoami-prod`, rimossa dopo il test.

- Davanti al container c'è nginx (`revprx01`); il container vede come mittente sempre `10.89.11.8`, cioè l'indirizzo di Podman rootless, uguale per tutti. L'IP del client arriva solo in `X-Forwarded-For`/`X-Real-Ip`, ma non è distinguibile da una chiamata diretta alla porta: **l'IP non si usa**.
- Edge su un PC del dominio invia **NTLM** senza chiedere nulla (il sito è già in zona intranet). Dal messaggio NTLM di tipo 3 si legge `COMUNE-MS\mirko.daddiego` e la postazione.
- **Kerberos no**: manca l'SPN, e il nome è un CNAME di `revprx01`. Non serve per questo sotto-progetto.
- nginx lascia passare `Authorization` e `WWW-Authenticate`; verso il container chiude la connessione a ogni richiesta (HTTP/1.0), quindi l'handshake NTLM non è legato a una connessione.
- Per la fase filtri: in AD l'ufficio è in `physicalDeliveryOfficeName` (218 utenti attivi su 257, 20 valori distinti), `department` è vuoto per tutti, gli utenti hanno decine di gruppi (`memberOf`).

## Decisioni

- **Identità dichiarata, mai usata per autorizzare.** Il nome utente letto da NTLM non è verificato crittograficamente (servirebbe NETLOGON verso il domain controller): chiunque in rete può fabbricare un messaggio con il nome di un collega. L'identità serve **solo a personalizzare la vista**; non dà accesso all'admin né ad alcuna azione. Vincolo da scrivere anche nei commenti del codice.
- **Riconoscimento una tantum con cookie**: NTLM solo su un endpoint dedicato chiamato in background, mai sulla pagina principale (niente finestra di login sui PC fuori dominio all'apertura della plancia).
- **Nessun criterio di filtro nel codice**: niente attributo "ufficio" né gruppi in questa parte. Di AD si legge solo il nome visualizzato.
- **Niente proxy fidati.** L'IP non è affidabile dietro Podman rootless e il riconoscimento non ne ha bisogno. Il limitatore di login perde la chiave per IP (§6).

## 1. Configurazione

Nuova variabile d'ambiente (in `docker-compose.yml`, `.env.example`, `internal/config`):

| Variabile | Esempio | Significato |
|---|---|---|
| `NTLM_DOMAIN` | `COMUNE-MS` | Dominio NetBIOS accettato nel messaggio NTLM, confronto senza maiuscole/minuscole. **Vuota = riconoscimento disattivato.** |

Il riconoscimento è attivo solo se `NTLM_DOMAIN` è impostata **e** c'è un modo di cercare gli utenti: `LDAP_BIND_DN` impostato, oppure `LDAP_HOST=mock`. Se `NTLM_DOMAIN` è impostata ma `LDAP_BIND_DN` no: warning all'avvio e riconoscimento spento.

## 2. Pacchetto `internal/identity`

**NTLM** (`ntlm.go`):
- `Challenge() []byte`: messaggio di tipo 2 con sfida casuale e `TargetInfo` minimo.
- `ParseAuthenticate(msg []byte) (Login, error)`, `Login{Domain, User, Workstation}`: solo firma `NTLMSSP\0` e tipo 3; ogni campo (lunghezza, offset) controllato contro la dimensione del buffer, UTF-16LE di lunghezza dispari = errore, utente vuoto = errore. Nessuna verifica della risposta.
- `MessageType(msg []byte) int`: 1, 2, 3 oppure 0.

**Directory** (`directory.go`):

```go
type Person struct {
	Username string // sAMAccountName, minuscolo
	Name     string // displayName (vuoto se assente)
}

type Directory interface {
	Lookup(username string) (Person, error) // ErrUnknownUser se non trovato o disattivato
}
```

- LDAP: bind di servizio (`LDAP_BIND_DN`/`LDAP_BIND_PASSWORD`) con le stesse regole di connessione del login admin (`auth.Dial`). Ricerca per `sAMAccountName` (è il nome che NTLM trasmette), solo utenti attivi; username validato con la regola del login admin (`[A-Za-z0-9._@-]`).
- Mock (`LDAP_HOST=mock`): qualsiasi username valido → `Name` = username.

**Cookie dell'utente** (`user.go`):
- `User{Username, Name string; Anonymous bool}`, `FirstName()` = prima parola del nome.
- Cookie `cruscotto_utente` firmato e cifrato con chiavi derivate da `SESSION_SECRET` (diverse da quelle della sessione admin), `Path=/`, `HttpOnly`, `SameSite=Lax`, `Secure` con la regola del cookie admin. **30 giorni** se riconosciuto, **24 ore** se anonimo. Illeggibile, manomesso o scaduto = assente.

## 3. Flusso di riconoscimento

Endpoint `GET /io`, pubblico, sempre `Cache-Control: no-store`.

1. `GET /` viene servito subito; con il cookie "riconosciuto" la plancia saluta per nome.
2. Se il cookie manca e il riconoscimento è attivo, `<body>` porta `data-riconosci` e `dashboard.js` chiama `fetch("/io")` una volta.
3. `/io` senza `Authorization` → **401** `WWW-Authenticate: NTLM` e intanto cookie **anonimo** (24 ore): se il browser non continua non si ritenta per un giorno.
4. Tipo 1 → **401** `WWW-Authenticate: NTLM <sfida>`.
5. Tipo 3: dominio diverso o utente non trovato → **200** `{"riconosciuto":false}` + cookie anonimo, log `Info` (valori ripuliti con `SafeLog`); LDAP non raggiungibile → **503** senza cookie, log `Warn`; utente trovato → **200** `{"riconosciuto":true,"nome":"…"}` + cookie riconosciuto.
6. Con `riconosciuto:true` `dashboard.js` ricarica la pagina **una volta**.
7. Altro `Authorization` o NTLM malformato → **400**, mai panic.

Riconoscimento disattivato: `/io` → 404, niente `data-riconosci`.

Limiti accettati: il nome resta quello letto fino alla scadenza del cookie; su un PC fuori dominio con il sito in zona intranet la richiesta in background può mostrare **una volta** la finestra di login (annullandola, il cookie anonimo evita che ricompaia per 24 ore).

## 4. Saluto

Con l'utente riconosciuto `hello-name` contiene il nome di battesimo: "Buongiorno, Mirko". Senza nome visualizzato in AD resta il solo saluto.

## 5. Errori

| Caso | Comportamento |
|---|---|
| Messaggio NTLM troncato o con offset fuori buffer | 400, nessun panic (fuzz test) |
| Dominio NTLM diverso da `NTLM_DOMAIN` | cookie anonimo, log `Info` |
| Utente non in AD o disattivato | cookie anonimo, log `Info` |
| LDAP non raggiungibile | 503, nessun cookie, log `Warn` |
| Cookie manomesso o segreto cambiato | ignorato, si ritenta il riconoscimento |
| Riconoscimento disattivato | `/io` → 404, nessun tentativo |

## 6. Limitatore di login admin

Dietro Podman rootless tutte le richieste hanno lo stesso `RemoteAddr`: la chiave `ip:` del `RateLimiter` è di fatto un blocco globale. **Si toglie** e resta quella per username (contro i tentativi su molti username restano i blocchi account di AD). Il debito noto in `CLAUDE.md` si chiude.

## 7. Test

- `identity`: sfida (tipo 2, casuale), lettura del tipo 3, messaggi ostili (troncati, offset oltre, enormi, lunghezze dispari, utente vuoto) e fuzz; cookie (andata e ritorno, manomesso, altro segreto, vuoto); `FirstName`; filtro LDAP con escape; directory mock.
- `web`: handshake completo simulato su `/io` (cookie riconosciuto con attributi corretti, JSON con il nome); dominio sbagliato e utente sconosciuto → anonimo; LDAP giù → 503 senza cookie; `Authorization` malformato → 400; spento → 404; **cookie utente non apre `/admin`**; plancia: saluto per nome, `data-riconosci` solo senza cookie e con riconoscimento attivo; limitatore per username.

## 8. Documentazione

`CLAUDE.md` (pacchetto `identity`, flusso `/io`, vincolo "identità dichiarata", `NTLM_DOMAIN`, risultati della sonda nella sezione "Identificazione utente", via il debito sul limitatore per IP); `.env.example` e `docker-compose.yml`.

## 9. Fase successiva: filtri sui contenuti (da progettare)

Requisiti raccolti, **non** in questa parte:
- Il criterio si configura **dall'interfaccia admin**, nulla nel codice: destinatari scelti tra **gruppi AD** e/o **valori di un attributo** (es. l'ufficio in `physicalDeliveryOfficeName`), con elenchi letti da AD.
- Per ogni applicativo, guida o avviso una regola **"mostra solo a"** (allowlist) oppure **"nascondi a"** (denylist); senza regola = per tutti.
- Interruttore "Mostra tutto" per vedere anche ciò che il filtro esclude (il filtro è presentazione, non sicurezza: identità dichiarata).
- Gruppi e attributi dell'utente vanno riletti da AD lato server (con cache), non messi nel cookie (limite di 4 KB; un utente può avere decine di gruppi).
