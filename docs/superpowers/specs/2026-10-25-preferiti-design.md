# Preferiti e ordine personale della plancia

Data: 2026-10-25. Stato: bozza da approvare.

## Intento

L'ente non può sapere quali applicativi usa di più ciascun dipendente, e alcune categorie hanno più di 10 applicativi. Ognuno deve poter avere a portata di mano i suoi, **senza fatica**: una sezione «Preferiti» che si riempie da sola con quello che usa di più, che può fissare e riordinare, e la possibilità di riordinare applicativi e categorie. **La UI deve essere semplice**: chi non vuole personalizzare non deve accorgersi di nulla di nuovo, se non dei preferiti che compaiono da soli.

## Obiettivi

1. **Sezione «Preferiti»** in cima alla colonna degli applicativi, **sopra il carosello delle news**, al massimo **6** applicativi.
   - **Fissati** dall'utente, nell'ordine scelto da lui.
   - I posti liberi si riempiono **da soli** con gli applicativi che quell'utente apre di più.
2. **Modalità «Personalizza»** (pulsante a destra del titolo «Preferiti»): in modifica si trascinano i preferiti, gli applicativi dentro ogni categoria e le categorie stesse; si fissa/sblocca un preferito con la puntina; «Fatto» esce dalla modifica; «Ripristina» torna all'ordine dell'admin.
3. Tutto salvato **nel DB per utente** (non in cookie né `localStorage`): stesso risultato su ogni browser e PC.

## Non obiettivi

- Nascondere applicativi o categorie.
- Rinominare, cambiare colori o icone.
- Riordinare le guide o i widget della colonna destra.
- Spostare un applicativo da una categoria a un'altra.
- Personalizzazione per gli anonimi.

## Anti-obiettivi (se succede, abbiamo sbagliato anche se funziona)

- **Spostamenti per sbaglio**: fuori da «Personalizza» un clic apre l'applicativo come oggi, niente si trascina.
- **Pagina diversa da prima per chi non personalizza**, a parte la sezione Preferiti che si riempie da sola.
- **Dati d'uso per persona visibili ad altri**, admin compreso; nessuna data o orario nel conteggio.
- **Uno stile diverso** da quello della plancia attuale.

## Decisioni prese con l'utente

- Modalità con pulsante **«Personalizza»** (opzione B delle bozze), non trascinamento sempre attivo.
- Preferiti: **6** posti, fissati + riempiti in automatico con i più aperti dall'utente.
- Si riordinano **preferiti fissati, applicativi dentro le categorie e categorie**.
- Conteggio delle aperture **per utente e per applicativo**: accettato.
- In modalità modifica i riquadri **tremano** (come le icone dei telefoni) con un bordo azzurro; con «effetti di animazione» spenti nel sistema (`prefers-reduced-motion: reduce`) **niente tremolio, solo il bordo**.
- **Stile coerente con la plancia attuale**: riquadri pastello, riquadro bianco dell'icona, icone Material già self-hosted, colori e font di `app.css`/`plancia.css`. Le bozze erano solo indicative.

## Comportamento

### Sezione Preferiti

- Compare solo agli **utenti riconosciuti** (cookie `cruscotto_utente`, non anonimo) e solo se ha **almeno un elemento** (un fissato o un applicativo aperto almeno una volta). Un utente nuovo non la vede finché non apre o fissa qualcosa. In modalità modifica si vede comunque (vuota, con la spiegazione), così si può fissare qualcosa subito.
- Ordine: prima i **fissati** nell'ordine dell'utente, poi gli **automatici** per numero di aperture decrescente, a parità nell'ordine della plancia (categoria, poi applicativo, come li vede l'utente).
- Solo applicativi **destinati all'utente** (attivi, con URL, destinati a lui: la stessa regola di `appVisibleTo` per i clic). «Mostra tutto» non cambia i preferiti: un applicativo non destinato non si conta, non si fissa e non entra nei preferiti. Un fissato che non è più visibile (disattivato, senza URL, non più destinato) **non occupa un posto** e non si vede; resta salvato e ricompare se torna visibile. Un applicativo eliminato sparisce dai fissati (cascade).
- Un applicativo nei preferiti **resta anche nella sua categoria**. Il riquadro copiato nei preferiti non ripete gli `id` HTML dell'originale (es. `guide-app-N`): servono `id` diversi.
- Il riquadro di un preferito è lo stesso riquadro della plancia (`a.tile-main` con icona, colore, guide e assistenza come nella categoria), così si apre e si comporta come l'originale. Sui fissati, fuori dalla modifica, una puntina piccola.

### Conteggio delle aperture

- Si conta un'apertura per ogni clic (anche tasto centrale) su un riquadro di un applicativo, nei preferiti o nelle categorie, e per ogni apertura di un applicativo dalla **ricerca a tendina**.
- Si salva **solo** `username`, `app_id`, `opens` (numero). Nessuna data, nessun orario.
- Lo stesso beacon di oggi (`POST /app/{id}/clic`, sempre 200) fa anche questo conteggio, solo per utenti riconosciuti e applicativi visibili a loro. Il conteggio personale **non dipende da `STATS_ENABLED`**: è personalizzazione, non statistica. Il conteggio aggregato «Noi del Comune» (`app_clicks`, un clic per utente/app/giorno) resta com'è.
- Per non crescere senza limiti per errore: al massimo un incremento per utente e app ogni 10 secondi (in memoria, indipendente dal dedup giornaliero del conteggio aggregato).
- La ricerca a tendina oggi non conosce l'ID dell'applicativo: `searchItem` riceve l'ID per le voci `app`, e `dashboard.js` manda il beacon quando si apre una voce `app`.
- Pulizia: quando un utente esce da `user_presence` perché AD non lo conosce più (pulizia giornaliera esistente), si cancellano anche i suoi conteggi e il suo ordine personale.

### Modalità «Personalizza»

- Pulsante **«Personalizza»** a destra del titolo «Preferiti» (icona `tune`). Se la sezione Preferiti non è visibile (utente nuovo), il pulsante sta comunque in testa alla colonna degli applicativi, al posto della sezione.
- In modifica:
  - il pulsante diventa **«Fatto»** (primario, icona `check`) e compare **«Ripristina»** (icona `restart_alt`, stile discreto) con conferma;
  - una riga di spiegazione sotto il titolo;
  - riquadri con bordo azzurro, maniglia `drag_indicator` e tremolio leggero; titolo delle categorie con la maniglia;
  - sui preferiti un bottone tondo con la puntina: blu = fissato («Sblocca»), grigio = automatico («Fissa»);
  - sui riquadri delle categorie un bottone puntina «Aggiungi ai preferiti» (fissa l'applicativo, in fondo ai fissati; se era già un automatico diventa fissato al posto in cui si trova); su un applicativo già fissato la puntina è blu e lo sblocca;
  - un clic sul riquadro **non apre** l'applicativo; i badge di guide/assistenza e l'apertura della tile sono disattivati;
  - le news e i widget restano, ma il carosello non ruota;
  - se «Mostra tutto» è attivo, entrando in modifica i riquadri non destinati all'utente si nascondono finché si resta in modifica (non si contano, non si fissano, non si riordinano).
- Trascinamenti:
  - nei preferiti: riordina; **un automatico trascinato diventa fissato** in quel posto;
  - dentro una categoria: riordina gli applicativi di quella categoria;
  - categorie: si trascinano dal titolo, riordinano le categorie;
  - **non** si trascina tra contenitori diversi (es. da una categoria ai preferiti: per quello c'è la puntina).
- **Preferiti pieni** (6 fissati): «Fissa» e «Aggiungi ai preferiti» sono disattivati con il suggerimento «Sblocca prima un preferito». Gli automatici scendono a 6 − fissati.
- **Salvataggio**: ogni modifica (spostamento, fissa/sblocca) si salva subito con una richiesta al server; «Fatto» esce solo dalla modifica. Se il salvataggio fallisce, messaggio generico e la pagina si ricarica dall'ultimo stato salvato, **tornando in modifica** (segno in `sessionStorage`, solo per quella ricarica).
- **«Ripristina»**: dopo conferma toglie i fissati e l'ordine personale di applicativi e categorie (i conteggi delle aperture restano, quindi gli automatici restano).
- Esc esce dalla modifica come «Fatto».
- Deve funzionare **con mouse e touch** (tablet, PC touch): il drag and drop nativo HTML5 non funziona su touch, quindi trascinamento con Pointer Events, scritto a mano in un nuovo file JS (`web/static/js/personalizza.js`). Su touch il trascinamento parte tenendo premuto ~300 ms, così lo scorrimento della pagina resta possibile.
- Accessibilità: riquadri e titoli trascinabili raggiungibili da tastiera in modifica; con il focus, frecce per spostare (su/giù/sinistra/destra = posizione precedente/successiva nello stesso contenitore), annunciato con un testo `aria-live`.

### Ordine personale di applicativi e categorie

- L'utente che non ha mai riordinato vede l'ordine dell'admin (`sort_order`).
- Ordine personale = elenco di ID salvato per utente: per le categorie, e per gli applicativi di ogni categoria.
- **Applicativi o categorie nuovi** (aggiunti dall'admin dopo che l'utente ha riordinato): in fondo, nel loro ordine admin. Elementi eliminati: spariscono (cascade).
- Un applicativo spostato dall'admin in un'altra categoria: compare nella nuova categoria, in fondo per chi l'ha riordinata (al cambio di categoria si cancellano le sue righe in `user_app_order`).
- La ricerca a tendina non cambia ordine.

## Dati

Migrazione **v15** (in coda, come sempre):

- `user_app_opens(username TEXT, app_id INTEGER REFERENCES apps(id) ON DELETE CASCADE, opens INTEGER NOT NULL, PRIMARY KEY(username, app_id))`
- `user_favorites(username TEXT, app_id INTEGER REFERENCES apps(id) ON DELETE CASCADE, position INTEGER NOT NULL, PRIMARY KEY(username, app_id))` — solo i fissati.
- `user_category_order(username TEXT, category_id INTEGER REFERENCES categories(id) ON DELETE CASCADE, position INTEGER NOT NULL, PRIMARY KEY(username, category_id))`
- `user_app_order(username TEXT, app_id INTEGER REFERENCES apps(id) ON DELETE CASCADE, position INTEGER NOT NULL, PRIMARY KEY(username, app_id))` — posizione nella categoria attuale dell'applicativo.

Username sempre minuscolo come nel resto del package. Le tabelle finiscono nel backup come tutto il DB.

## Elementi nascosti (fissati o ordinati ma ora non visibili)

Un applicativo o una categoria può sparire per un po' (disattivato, senza URL, non più destinato all'utente) e poi tornare. Regola unica:

- Ogni salvataggio manda l'elenco di **quello che l'utente vede**. Il server riscrive le posizioni di quegli elementi e **tiene le righe degli elementi non visibili**, mettendole dopo i visibili nel loro ordine relativo di prima. Quando tornano visibili ricompaiono **in fondo**.
- Il limite di **6 fissati** vale su quelli **visibili**. Se un fissato nascosto torna e i fissati visibili diventano 7, si mostrano i primi 6 in ordine; il settimo resta salvato e ricompare quando si libera un posto.

## Endpoint

Tutti per utenti riconosciuti, **sempre 200** con `{"ok":true|false}` (il proxy sostituisce i 4xx/5xx), protetti dalla `CrossOriginProtection` esistente:

- `POST /preferiti` — salva i fissati visibili: elenco ordinato di ID (max 6, solo applicativi destinati all'utente; il resto si scarta). I fissati non visibili restano (vedi sopra).
- `POST /ordine/categorie` — elenco ordinato di ID categoria.
- `POST /ordine/categorie/{id}` — elenco ordinato degli ID applicativo di quella categoria.
- `POST /ordine/ripristina` — cancella fissati e ordine personale.

Il formato del corpo (form `id=1&id=2…` o JSON) è scelta del builder, coerente con gli altri endpoint della plancia. ID sconosciuti, ripetuti o non visibili si scartano senza errore. Anonimo o non riconosciuto: `{"ok":false}` senza scrivere nulla.

## Vincoli

- CSP stretta: niente JS o CSS inline; nuovo JS in file, stili in `plancia.css`.
- Stile e componenti della plancia attuale (riquadri `.tile`, colori `tint`, icone Material self-hosted).
- Identità dichiarata (NTLM non verificato): chi falsifica il nome di un collega può cambiargli l'ordine. Danno minimo, accettato come per il resto della personalizzazione.
- Il refresh HTMX del carosello (`/partials/alerts`) non deve toccare preferiti e ordine.
- La pagina deve restare veloce: preferiti e ordine si leggono con poche query per caricamento, dentro `handleDashboard`.

## Lasciato al builder

- Formato dei corpi delle richieste e nomi delle funzioni.
- Dettagli dell'animazione (ampiezza e durata del tremolio, che deve restare leggero).
- Testo esatto della riga di spiegazione e della conferma di «Ripristina».
- Come gestire il posto vuoto mentre si trascina (segnaposto).
