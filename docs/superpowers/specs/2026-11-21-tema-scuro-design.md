# Tema scuro automatico

## Perché

I colleghi usano Windows in modalità scura. La plancia, l'admin e l'editor restano chiari e stonano. Il tema deve seguire il sistema da solo.

## Obiettivi

- Plancia, pagine secondarie (avvisi, guide, ticket), admin, login ed editor seguono `prefers-color-scheme`.
- Il cambio di tema di Windows si applica senza ricaricare la pagina (lo fa già la media query).
- Il contrasto resta leggibile (WCAG AA sul testo) in entrambi i temi.

## Non obiettivi

- Nessun interruttore per l'utente e nessuna preferenza salvata: segue sempre il sistema.
- Nessun colore dedicato al tema scuro per singola app nell'admin.

## Anti-obiettivi

- Testo scuro su fondo scuro, o riquadri bianchi accecanti sparsi nella plancia scura.
- Tile pastello che al buio diventano chiarissime.
- Selettori CSS duplicati in un secondo file che si disallinea.

## Approcci

- **A. Token + media query + `color-mix()` (scelto).** Ogni colore fisso diventa un token. I token si ridefiniscono una sola volta sotto `@media (prefers-color-scheme: dark)`. Le tile calcolano il pastello in CSS dal colore dell'app. Una sola fonte. Richiede `color-mix` (Edge/Chrome 111+, presente sui PC).
- B. Come A, ma Go calcola due tinte (`--tint-l`/`--tint-d`). Più logica di presentazione nel server, inutile con Edge aggiornato.
- C. `dark.css` separato con `media`. Selettori riscritti due volte, si disallinea. Scartato.

## Design

- **Token**: `app.css` (`--bg`, `--card`, `--ink`, …) e `plancia.css` (`--p-*`) diventano le uniche fonti di colore. I circa 104 colori fissi di `plancia.css` e i 14 di `admin.css` passano a token, nuovi dove serve: superficie, superficie in rilievo, bordo, ombre, colori di stato con il loro sfondo. Banda blu e testata: blu più profondi al buio.
- **Media query**: un blocco `@media (prefers-color-scheme: dark)` in `app.css` per i token condivisi e uno in `plancia.css` per i `--p-*`. `:root { color-scheme: light dark; }` per scrollbar, campi, `<dialog>` e `<details>` nativi.
- **Tile**: il template scrive `style="--c:{{.IconColor}}"` (colore validato, grigio se non valido, come oggi). Il CSS usa `background: color-mix(in srgb, var(--c) 15%, var(--tile-base))`, con `--tile-base` bianco al chiaro e superficie scura al buio. `tint` in Go va rimossa o ridotta alla validazione del colore, con i test aggiornati.
- **Riquadro icona**: `icon_bg` vuoto resta bianco anche al buio (i loghi caricati sono pensati per il bianco). Le icone `pack` col colore pieno non cambiano.
- **Logo ente**: sempre su fondo chiaro (piccolo riquadro chiaro dove serve).
- **Logo CruscottoPA** (footer, login): `<picture>` con `logo-on-dark.svg` sotto `prefers-color-scheme: dark`.
- **`theme-color`**: due `<meta>` con `media`, chiaro e scuro.
- **Markdown**: testo, link, tabelle e codice prendono i token. Le immagini restano invariate.
- **Guide illustrate** (`img/guida/*.svg`): restano chiare, perché rappresentano l'interfaccia del browser. Bordo e fondo coerenti col tema scuro.
- **Editor TipTap**: `editor.css` (sorgente in `web/editor`) usa i token dell'admin.
- **Avvisi/stati**: urgente, manutenzione e novità hanno varianti scure (fondo scuro tinto, testo chiaro tinto). L'avviso rosso «Notifiche disattivate» resta ben visibile.
- **Ombre**: al buio più leggere o sostituite da un bordo.
- **Test**: test Go che leggono il CSS (come quelli esistenti) per verificare che esistano i blocchi dark, che `color-scheme` sia dichiarato e che non restino colori fissi fuori dai token, con un'eccezione ristretta e motivata. Prova visiva con Playwright `colorScheme: 'dark'` su plancia, avvisi, guida, ticket, admin e login: screenshot da rivedere a mano.

## Lasciato al builder

Valori esatti della palette scura, nel rispetto di AA. Nomi dei nuovi token. Quali eccezioni ammettere al test dei colori fissi.
