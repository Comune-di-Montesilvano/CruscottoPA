# Filtri sui contenuti per gruppi della plancia

Data: 2026-10-06 · Stato: in revisione · Sotto-progetto 4, seconda parte (segue `2026-10-06-riconoscimento-utente-design.md`, §9)

## Contesto e obiettivo

Dalla 0.4.0 la plancia riconosce l'utente (NTLM, identità dichiarata) e lo saluta per nome. Tutti però vedono gli stessi applicativi, guide e avvisi, anche quelli che riguardano un solo ufficio.

**Obiettivo:** permettere agli amministratori di decidere, **dall'interfaccia e senza nulla di fisso nel codice**, chi vede ogni contenuto, tramite **gruppi della plancia** definiti con regole su Active Directory.

## Decisioni

- **Gruppi della plancia** definiti in admin, ognuno con regole di appartenenza di quattro tipi: **valore di un attributo AD** (es. Ufficio = INFORMATIZZAZIONE), **gruppo AD**, **singolo utente**, **escludi utente**. Esempio: *CED* = ufficio INFORMATIZZAZIONE + utente `mario.rossi`, escluso `stagista1`.
- **Visibilità di ogni contenuto** (applicativo, guida, avviso): **Pubblico**, **Riservato a** uno o più gruppi, oppure **Nascosto a** uno o più gruppi.
- **Attributi AD utilizzabili** configurati in admin (nome AD + etichetta), non nel codice.
- **Filtro di presentazione, non protezione.** L'identità è dichiarata (NTLM non verificato) e c'è "Mostra tutto": un contenuto *Riservato* non è segreto. Va scritto nei form.
- Filtro **lato server**; "Mostra tutto" è un cookie di preferenza.

## 1. Dati (migrazione v5)

```sql
CREATE TABLE audience_attributes (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	name     TEXT NOT NULL UNIQUE COLLATE NOCASE, -- nome LDAP, es. physicalDeliveryOfficeName
	label    TEXT NOT NULL                        -- es. "Ufficio"
);

CREATE TABLE audience_groups (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	name       TEXT    NOT NULL UNIQUE COLLATE NOCASE,
	sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE audience_rules (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id INTEGER NOT NULL REFERENCES audience_groups(id) ON DELETE CASCADE,
	kind     TEXT    NOT NULL,          -- attr | adgroup | user | exclude (validato in Go)
	attr     TEXT    NOT NULL DEFAULT '', -- solo per kind=attr: nome LDAP
	value    TEXT    NOT NULL,          -- valore dell'attributo, DN del gruppo AD o username
	label    TEXT    NOT NULL DEFAULT '', -- per kind=adgroup: CN da mostrare
	UNIQUE (group_id, kind, attr, value)
);

CREATE TABLE content_audience (
	kind       TEXT    NOT NULL,        -- app | guide | alert
	content_id INTEGER NOT NULL,
	mode       TEXT    NOT NULL,        -- only | hide (uguale per tutte le righe del contenuto)
	group_id   INTEGER NOT NULL REFERENCES audience_groups(id),
	PRIMARY KEY (kind, content_id, group_id)
);
```

- Nessuna riga in `content_audience` per un contenuto = **Pubblico**.
- `content_audience.group_id` **senza** cascata: un gruppo in uso non si può eliminare (§4).
- Le righe di un contenuto eliminato si cancellano in `DeleteApp`/`DeleteGuide`/`DeleteAlert`, nella stessa transazione. Le guide di un'app eliminata diventano generali (`ON DELETE SET NULL`) e tengono la loro visibilità.
- Lo username nelle regole `user`/`exclude` è salvato minuscolo; i confronti sono senza maiuscole/minuscole e spazi ai bordi.

## 2. Appartenenza e visibilità (logica pura)

Pacchetto `internal/audience`, senza dipendenze da DB o LDAP:

```go
type Profile struct {
	Username string            // minuscolo
	Attrs    map[string]string // nome attributo (minuscolo) → valore
	Groups   []string          // DN dei gruppi AD, annidati compresi
}

type Rule struct{ Kind, Attr, Value string }

// Member: almeno una regola tra attr/adgroup/user soddisfatta E nessuna exclude.
func Member(p Profile, rules []Rule) bool

type Mode string // "" (pubblico) | "only" | "hide"

// Visible: pubblico → sempre; utente non noto (anonimo o profilo non
// disponibile) → solo i pubblici; only → in almeno uno dei gruppi;
// hide → in nessuno.
func Visible(mode Mode, groups []int64, memberOf map[int64]bool, known bool) bool
```

- **Utente anonimo o profilo non disponibile: solo i contenuti pubblici** (anche i *Nascosto a* restano nascosti), per scelta esplicita.
- Guida collegata a un applicativo: visibile solo se è visibile **anche** l'applicativo.

## 3. Profilo dell'utente da AD

`identity.Directory` si estende:

```go
type Directory interface {
	Lookup(username string) (Person, error)
	Profile(username string, attrs []string) (audience.Profile, error)
	SearchGroups(q string) ([]ADGroup, error)          // per nome (cn), max 20
	SearchUsers(q string) ([]Person, error)            // per nome o username, max 20
	AttributeValues(attr string) ([]string, error)     // valori distinti tra gli utenti attivi
	Members(rules []audience.Rule, attrs []string) (count int, sample []Person, err error) // anteprima
}

type ADGroup struct{ DN, Name string }
```

- `Profile`: legge gli attributi configurati dell'utente e i gruppi, **annidati compresi**, con una ricerca `(member:1.2.840.113556.1.4.1941:=<DN utente>)` sui gruppi.
- **Cache dei profili** nel server: in memoria per username, 15 minuti. AD non disponibile → nessun profilo (vede solo i pubblici), riprovando alla richiesta successiva dopo 1 minuto (cache negativa breve, così un AD giù non viene interrogato a ogni pagina).
- `AttributeValues`: cache 6 ore per attributo, come previsto per gli uffici.
- `Members` (anteprima): utenti attivi che soddisfano le regole; massimo 30 nomi, numero totale.
- Nomi degli attributi validati: solo `^[A-Za-z][A-Za-z0-9-]{0,63}$`; valori e query sempre con `ldap.EscapeFilter`.
- Mock (`LDAP_HOST=mock`): profilo fisso con `physicalDeliveryOfficeName = INFORMATIZZAZIONE` e un gruppo finto; ricerche su un elenco fisso. Solo per lo sviluppo.

## 4. Admin

**Rail:** nuova voce **"Gruppi"** (`/admin/gruppi`, icona `groups`).

**Attributi AD utilizzabili** (riquadro in cima alla pagina):
- elenco nome LDAP + etichetta, aggiungi, togli;
- un attributo usato da qualche regola non si può togliere (errore con il nome dei gruppi).

**Gruppi della plancia:**
- elenco con nome, numero di regole, numero di contenuti collegati; nuovo, rinomina, sposta su/giù, elimina;
- **eliminazione bloccata** se il gruppo è usato da qualche contenuto: messaggio con l'elenco ("Usato da: Webmail, Avviso cedolini…").

**Modifica di un gruppo** (`/admin/gruppi/{id}/modifica`):
- elenco delle regole con etichetta leggibile ("Ufficio = INFORMATIZZAZIONE", "Gruppo AD SHARE_PNRR_RW", "Utente mario.rossi", "Escludi stagista1") e pulsante "Togli";
- aggiunta di una regola: tipo (Attributo / Gruppo AD / Utente / Escludi utente); per *Attributo* si sceglie prima l'attributo tra quelli configurati; campo valore con **suggerimenti HTMX** (`/admin/ad/valori?attr=…&q=…`, `/admin/ad/gruppi?q=…`, `/admin/ad/utenti?q=…`, risposta: lista di opzioni cliccabili, nessun JS inline);
- **"Anteprima membri"** su richiesta (`POST /admin/gruppi/{id}/anteprima`): numero di utenti e primi 30 nomi; "Nessun utente corrisponde" se zero.

**Form di applicativi, guide e avvisi:** fieldset **"Visibilità"**:
- radio *Pubblico* / *Riservato a* / *Nascosto a* e caselle con i gruppi della plancia (attive solo con *Riservato a*/*Nascosto a*; con *Pubblico* le caselle sono ignorate);
- *Riservato a*/*Nascosto a* senza gruppi spuntati → 422 "Scegli almeno un gruppo";
- avviso fisso: "Filtro di visualizzazione, non una protezione: chi conosce l'indirizzo può comunque aprire l'applicativo e chiunque può scegliere «Mostra tutto»";
- negli elenchi: etichetta "Pubblico", "Riservato: CED, Ragioneria" o "Nascosto a: Polizia Locale".

Tutte le azioni seguono il pattern admin esistente (sezione intera restituita, 200/422).

## 5. Plancia

- Filtro lato server su plancia, `/avvisi` e `/partials/alerts`, in base al profilo (§3) e alla visibilità (§2). Categorie rimaste vuote nascoste; il conteggio della categoria è quello delle app mostrate.
- **"Mostra tutto"**: cookie `cruscotto_tutto=1` (`Path=/`, 1 anno, `SameSite=Lax`, non `HttpOnly`) impostato da `dashboard.js`, che ricarica. Pulsante sotto la ricerca solo se il filtro ha escluso qualcosa o se è già attivo: "Mostra anche i contenuti non destinati a te (N)" / "Mostra solo i miei contenuti". Senza JS non compare.
- **Popup degli urgenti** solo per gli avvisi visibili all'utente, anche con "Mostra tutto".
- Utente anonimo o profilo non disponibile: solo i pubblici (§2).

## 6. Errori

| Caso | Comportamento |
|---|---|
| AD giù in plancia | nessun profilo: solo i pubblici; riprova dopo 1 minuto |
| AD giù in admin (suggerimenti, anteprima) | messaggio "AD non disponibile"; salvataggio regole funziona |
| Valore che non esiste più in AD | la regola resta; l'anteprima dice "Nessun utente corrisponde" |
| Gruppo in uso da eliminare | 422 con l'elenco dei contenuti |
| Attributo in uso da togliere | 422 con l'elenco dei gruppi |
| Nome attributo non valido | 422 "Nome di attributo LDAP non valido" |
| *Riservato a*/*Nascosto a* senza gruppi | 422 "Scegli almeno un gruppo" |

## 7. Test

- `audience`: `Member` (oppure tra regole, exclude vince, maiuscole e spazi, attributo assente, gruppo annidato via DN), `Visible` (pubblico, only, hide, anonimo: solo pubblici), ereditarietà guida → app.
- `identity`: costruzione dei filtri LDAP (escape, nome attributo validato), mock esteso.
- Cache dei profili con orologio finto: 15 minuti, cache negativa 1 minuto.
- `database`: migrazione v5, CRUD gruppi/regole/attributi, `content_audience` (set e lettura, pulizia alla cancellazione di app/guida/avviso), gruppo in uso non eliminabile.
- `web`: pagina gruppi e regole (aggiunta, rimozione, anteprima con directory finta, AD giù), form di contenuto con visibilità (422 senza gruppi), plancia filtrata per un utente membro/non membro/anonimo, *Nascosto a*, "Mostra tutto", popup urgente, `/avvisi` e partial.

## 8. Documentazione

`CLAUDE.md`: pacchetto `audience`, tabelle v5, profilo e cache, pagina Gruppi, "filtro non protezione". Nessuna nuova variabile d'ambiente.
