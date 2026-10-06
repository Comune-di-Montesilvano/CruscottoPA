# Riconoscimento utente e filtri per ufficio

Data: 2026-10-06 · Stato: in revisione · Sotto-progetto 4 (prima parte: solo personalizzazione)

## Contesto e obiettivo

La plancia oggi è uguale per tutti. Gli applicativi, le guide e gli avvisi però riguardano spesso un solo ufficio (Polizia Locale, Tributi, Demografici…): ogni dipendente vede anche ciò che non gli serve.

**Obiettivo:** riconoscere automaticamente chi apre la plancia da un PC del dominio, salutarlo per nome e mostrargli i contenuti per tutti più quelli del suo ufficio, con un interruttore "Mostra tutto".

Fuori ambito (sotto-progetti successivi): preferenze salvate per utente, redattori per ufficio, ticket, note. Richiedono un'identità verificata.

## Cosa ha mostrato la sonda in produzione (2026-10-06)

Sonda usa-e-getta `/debug/whoami` su `cruscottopa.comune.montesilvano.pe.it`, branch `spike/whoami-prod`, rimossa dopo il test.

- Davanti al container c'è nginx (`revprx01`); il container vede come mittente sempre `10.89.11.8`, cioè l'indirizzo di Podman rootless, uguale per tutti. L'IP del client arriva solo in `X-Forwarded-For`/`X-Real-Ip`, ma non è distinguibile da una chiamata diretta alla porta: **l'IP non si usa**.
- Edge su un PC del dominio invia **NTLM** senza chiedere nulla (il sito è già in zona intranet). Dal messaggio NTLM di tipo 3 si legge `COMUNE-MS\mirko.daddiego` e la postazione.
- **Kerberos no**: manca l'SPN, e il nome è un CNAME di `revprx01`. Non serve per questo sotto-progetto.
- nginx lascia passare `Authorization` e `WWW-Authenticate`; verso il container chiude la connessione a ogni richiesta (HTTP/1.0), quindi l'handshake NTLM non è legato a una connessione.
- L'ufficio è in AD in **`physicalDeliveryOfficeName`**: compilato per 218 utenti attivi su 257, 20 valori distinti (es. `POLIZIA LOCALE`, `LLPP`, `TRIBUTI`, `INFORMATIZZAZIONE`). `department` è vuoto per tutti.

## Decisioni

- **Identità dichiarata, mai usata per autorizzare.** Il nome utente letto da NTLM non è verificato crittograficamente: verificarlo richiederebbe NETLOGON verso il domain controller. Chiunque in rete può fabbricare un messaggio con il nome di un collega. Per questo l'identità serve **solo a personalizzare la vista**: un'identità falsificata mostra al massimo la plancia di un altro ufficio, che non contiene nulla di riservato. Non dà accesso all'admin né ad alcuna azione. Questo vincolo va scritto anche nei commenti del codice.
- **Riconoscimento una tantum con cookie**: NTLM solo su un endpoint dedicato chiamato in background, mai sulla pagina principale (niente finestra di login sui PC fuori dominio all'apertura della plancia).
- **Filtro "nasconde, con Mostra tutto"**: i contenuti senza ufficio li vedono tutti; quelli assegnati a uno o più uffici solo chi è di quegli uffici, a meno di attivare "Mostra tutto".
- **Uffici letti da AD**, mai scritti a mano.
- **Niente proxy fidati.** L'IP non è affidabile dietro Podman rootless e il riconoscimento non ne ha bisogno. Il limitatore di login perde la chiave per IP (vedi §7).

## 1. Configurazione

Nuova variabile d'ambiente (in `docker-compose.yml`, `.env.example`, `internal/config`):

| Variabile | Esempio | Significato |
|---|---|---|
| `NTLM_DOMAIN` | `COMUNE-MS` | Dominio NetBIOS accettato nel messaggio NTLM, confronto senza maiuscole/minuscole. **Vuota = riconoscimento disattivato.** |

Il riconoscimento è attivo solo se `NTLM_DOMAIN` è impostata **e** c'è un modo di cercare gli utenti: `LDAP_BIND_DN` impostato, oppure `LDAP_HOST=mock`. Se `NTLM_DOMAIN` è impostata ma `LDAP_BIND_DN` no, all'avvio compare un warning e il riconoscimento resta spento.

## 2. Pacchetto `internal/identity`

Tre parti indipendenti, testabili da sole.

**NTLM** (`ntlm.go`):
- `Challenge() []byte`: messaggio di tipo 2 con flag `UNICODE | REQUEST_TARGET | NTLM | ALWAYS_SIGN | TARGET_TYPE_DOMAIN | EXTENDED_SESSIONSECURITY | TARGET_INFO`, challenge casuale, `TargetInfo` minimo.
- `ParseAuthenticate(msg []byte) (Login, error)`, con `Login{Domain, User, Workstation string}`: accetta solo messaggi con firma `NTLMSSP\0` e tipo 3. Ogni campo (lunghezza e offset) è controllato contro la dimensione del buffer; UTF-16LE con lunghezza dispari = errore. Nessuna verifica della risposta: il commento lo dichiara.
- `MessageType(msg []byte) int`: 1, 2, 3 oppure 0 se non valido.

**Directory** (`directory.go`):

```go
type Person struct {
	Username string // sAMAccountName, minuscolo
	Name     string // displayName
	Office   string // physicalDeliveryOfficeName, spazi ai bordi rimossi
}

type Directory interface {
	Lookup(username string) (Person, error) // ErrUnknownUser se non trovato o disattivato
	Offices() ([]string, error)            // valori distinti, ordinati, utenti attivi
}
```

- Implementazione LDAP: bind con `LDAP_BIND_DN`/`LDAP_BIND_PASSWORD`, stessi parametri di connessione di `internal/auth` (StartTLS, skip verify, base DN), riusando la connessione esistente. Filtro utente: `(&(objectCategory=person)(objectClass=user)(sAMAccountName=<escapato>)(!(userAccountControl:1.2.840.113556.1.4.803:=2)))`. Username validato con la stessa regola del login admin (`[A-Za-z0-9._@-]`).
- `Offices()` ha una cache in memoria di 6 ore. In caso di errore restituisce l'ultimo valore valido, se esiste.
- Implementazione mock (`LDAP_HOST=mock`): qualsiasi username → `Name` = username, `Office` = `INFORMATIZZAZIONE`; `Offices()` = elenco fisso di 3 uffici.

**Cookie dell'utente** (`cookie.go`):
- Nome `cruscotto_utente`, firmato e cifrato con `SESSION_SECRET` (`gorilla/securecookie`, già dipendenza indiretta), `Path=/`, `HttpOnly`, `SameSite=Lax`, `Secure` con la stessa regola del cookie admin.
- Contenuto: `{Username, Name, Office, Anonymous bool}`.
- Durata: **30 giorni** se riconosciuto, **24 ore** se anonimo.
- Cookie illeggibile (manomesso, segreto cambiato, scaduto) = assente.

## 3. Flusso di riconoscimento

Endpoint `GET /io`, pubblico, sempre con `Cache-Control: no-store`.

1. `GET /` viene servito subito. Se il cookie dice "riconosciuto", la plancia esce già personalizzata.
2. Se il cookie manca e il riconoscimento è attivo, la pagina porta `data-riconosci` sul `<body>` e `dashboard.js` chiama `fetch("/io", {credentials: "same-origin"})` una volta.
3. `/io` senza `Authorization: NTLM …` → **401** `WWW-Authenticate: NTLM` e intanto imposta il cookie **anonimo** (24 ore). Se il browser non continua, resta anonimo e per un giorno non si ritenta.
4. Messaggio di tipo 1 → **401** `WWW-Authenticate: NTLM <sfida>`.
5. Messaggio di tipo 3:
   - dominio diverso da `NTLM_DOMAIN`, oppure utente non trovato → **200** `{"riconosciuto":false}`, cookie anonimo, log `Info` con username e dominio ripuliti da `SafeLog`;
   - LDAP non raggiungibile → **503** senza cookie (si ritenterà alla prossima visita), log `Warn`;
   - utente trovato → **200** `{"riconosciuto":true,"nome":"…"}` e cookie riconosciuto.
6. Se la risposta dice `riconosciuto:true`, `dashboard.js` ricarica la pagina **una volta**.
7. Qualsiasi altro `Authorization` o un messaggio NTLM malformato → **400**, senza panic.

Con il riconoscimento disattivato, `/io` risponde 404 e la pagina non porta `data-riconosci`.

Limite accettato: nome e ufficio restano quelli letti al riconoscimento fino alla scadenza del cookie (30 giorni). Un cambio d'ufficio in AD si vede dopo la scadenza, oppure subito cancellando i cookie del sito.

Limite accettato: su un PC fuori dominio con un browser che considera il sito intranet, la richiesta in background può mostrare **una volta** la finestra di login. Annullandola, il cookie anonimo impedisce che ricompaia per 24 ore.

## 4. Visibilità per ufficio

**Dati** (migrazione **v5**, in coda):

```sql
CREATE TABLE app_offices   (app_id   INTEGER NOT NULL REFERENCES apps(id)   ON DELETE CASCADE, office TEXT NOT NULL, PRIMARY KEY (app_id, office));
CREATE TABLE guide_offices (guide_id INTEGER NOT NULL REFERENCES guides(id) ON DELETE CASCADE, office TEXT NOT NULL, PRIMARY KEY (guide_id, office));
CREATE TABLE alert_offices (alert_id INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE, office TEXT NOT NULL, PRIMARY KEY (alert_id, office));
```

L'ufficio è il valore AD così com'è. Il confronto con l'ufficio dell'utente ignora maiuscole/minuscole e spazi ai bordi.

**Regole:**
- elemento senza uffici → **per tutti**;
- elemento con uffici → **del suo ufficio** se l'ufficio dell'utente è tra quelli, altrimenti **di altri uffici**;
- guida collegata a un applicativo: è "di altri uffici" se lo è l'applicativo **oppure** se lo sono i suoi uffici;
- utente anonimo o senza ufficio in AD: tutto ciò che ha uffici è "di altri uffici".

**Rendering** (il filtro è presentazione, non sicurezza):
- Il **server** esclude i contenuti "di altri uffici" da plancia, `/avvisi` e `/partials/alerts`, a meno che l'utente abbia scelto "Mostra tutto". Una categoria rimasta senza app non compare; il conteggio nell'intestazione è quello delle app mostrate. Ricerca, carosello e conteggi lavorano quindi solo su ciò che è in pagina, senza logica in più nel JS.
- **Avvisi urgenti**: il popup a tutto schermo compare solo per gli urgenti "per tutti" o "del suo ufficio", anche con "Mostra tutto" attivo (con "Mostra tutto" quelli degli altri uffici restano nel carosello).

**Interruttore "Mostra tutto"**: pulsante sotto la ricerca, presente solo se il filtro ha escluso almeno un contenuto oppure se "Mostra tutto" è già attivo (per poterlo spegnere). Testo: "Mostra anche i contenuti degli altri uffici (N)" / "Mostra solo il mio ufficio". La scelta è un cookie di preferenza `cruscotto_tutto=1` (`Path=/`, 1 anno, `SameSite=Lax`, non `HttpOnly`) impostato da `dashboard.js`, che poi ricarica la pagina. Senza JS l'interruttore non compare.

**Saluto**: con l'utente riconosciuto, `hello-name` contiene il nome di battesimo (prima parola di `displayName`), es. "Buongiorno, Mirko". Sotto la data compare una riga piccola con l'ufficio, es. "Ufficio INFORMATIZZAZIONE".

## 5. Admin

- Form di applicativi, guide e avvisi: fieldset **"Uffici"** con una casella per ogni ufficio di `Directory.Offices()`. Nessuna casella spuntata = per tutti (scritto nell'hint).
- Uffici già assegnati ma non più presenti in AD: mostrati spuntati con l'etichetta "(non più in AD)", e si possono togliere.
- Elenco AD non disponibile: compaiono solo gli uffici già assegnati, con l'avviso "Elenco uffici da AD non disponibile". Il salvataggio funziona comunque.
- Negli elenchi admin, una colonna o un'etichetta mostra gli uffici dell'elemento, oppure "Tutti".
- Validazione: ogni ufficio inviato deve essere in `Offices()` oppure già assegnato all'elemento; massimo 30 uffici per elemento.
- Il backup non cambia: le nuove tabelle sono nel DB.

## 6. Errori

| Caso | Comportamento |
|---|---|
| Messaggio NTLM troncato o con offset fuori buffer | 400, nessun panic (coperto da fuzz test) |
| Dominio NTLM diverso da `NTLM_DOMAIN` | cookie anonimo, log `Info` |
| Utente non in AD o disattivato | cookie anonimo, log `Info` |
| LDAP non raggiungibile in `/io` | 503, nessun cookie, log `Warn` |
| LDAP non raggiungibile in admin | elenco uffici con i soli già assegnati e avviso |
| Cookie manomesso o con segreto cambiato | ignorato, si ritenta il riconoscimento |
| Riconoscimento disattivato | `/io` → 404, plancia anonima senza tentativi |
| Ufficio dell'utente vuoto in AD | riconosciuto con nome, vede solo i contenuti "per tutti" |

## 7. Limitatore di login admin

Dietro Podman rootless tutte le richieste hanno lo stesso `RemoteAddr`: la chiave `ip:` del `RateLimiter` diventa un blocco globale, e 5 password sbagliate di chiunque bloccano tutti gli admin. **Si toglie la chiave per IP** e resta quella per username. Contro i tentativi distribuiti su molti username resta il blocco degli account di AD. Il debito noto in `CLAUDE.md` si chiude.

## 8. Test

`internal/identity`:
- `Challenge`: tipo 2 valido, challenge diverso a ogni chiamata;
- `ParseAuthenticate` con un tipo 3 costruito nel test (dominio, utente e postazione in UTF-16LE): campi letti correttamente;
- messaggi troncati, offset oltre il buffer, lunghezze dispari, firma sbagliata, tipo sbagliato → errore; `FuzzParseAuthenticate` senza panic;
- cookie: andata e ritorno, cookie manomesso ignorato, durata 30 giorni / 24 ore;
- directory mock: lookup e uffici; filtro LDAP con escape dello username (unità sulla costruzione del filtro).

`internal/database`:
- migrazione v5;
- salvataggio e lettura degli uffici di app, guide e avvisi; cancellazione a cascata;
- regole di visibilità: per tutti, del suo ufficio, di altri uffici, guida che eredita dall'applicativo, utente anonimo, confronto senza maiuscole e spazi.

`internal/web`:
- `/io`: handshake completo simulato (tipo 1 → sfida → tipo 3) → cookie riconosciuto e JSON con il nome; dominio sbagliato → cookie anonimo; utente sconosciuto → anonimo; LDAP giù → 503 senza cookie; `Authorization` malformato → 400; riconoscimento disattivato → 404;
- plancia con il cookie: saluto per nome, ufficio, contenuti di altri uffici esclusi (e inclusi con `cruscotto_tutto=1`), categoria vuota nascosta, popup urgente solo se pertinente, interruttore con il conteggio presente solo se serve;
- plancia anonima: `data-riconosci` presente solo con il riconoscimento attivo e senza cookie;
- **un cookie utente non apre `/admin`** (303 al login);
- form admin: caselle degli uffici, salvataggio, "(non più in AD)", elenco AD non disponibile, ufficio non ammesso → 422;
- limitatore: 5 errori su un username non bloccano un altro username.

## 9. Documentazione

- `CLAUDE.md`: pacchetto `identity`, flusso `/io`, vincolo "identità dichiarata, mai per autorizzare", tabelle `*_offices` (v5), `NTLM_DOMAIN`, regola di visibilità; via il debito sul limitatore per IP; nella sezione "Identificazione utente" i risultati della sonda (IP inutilizzabile dietro rootlessport, NTLM sì, Kerberos no).
- `.env.example` e `docker-compose.yml`: `NTLM_DOMAIN`.
