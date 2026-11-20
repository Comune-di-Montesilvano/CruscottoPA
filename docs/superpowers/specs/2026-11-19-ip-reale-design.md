# IP reale del PC dal reverse proxy

## Contesto

Finora l'IP del PC («Questo PC» in testata, dialog e blocco «Informazioni sul PC» dei ticket) veniva solo dal DNS di AD: nome della workstation NTLM + `PC_DNS_SUFFIX`. Si pensava che l'IP del client non arrivasse al container: `RemoteAddr` è sempre l'indirizzo della rete di Podman.

Prova del 2026-11-19 con `traefik/whoami` al posto del Cruscotto di test, dietro revprx01: nginx invia `X-Real-Ip` e `X-Forwarded-For` con l'IP vero del PC (confermato con `ipconfig`), nessun NAT tra gli uffici e il proxy. Solo `RemoteAddr` è inutile.

## Decisioni

- **IP mostrato** (testata, dialog, ticket): `X-Real-Ip` se è un IPv4 valido, altrimenti il DNS come prima.
- **Header accettato così com'è**, senza segreti con il proxy: l'IP resta un'etichetta per l'assistenza, con la stessa fiducia del nome NTLM, **mai per autorizzare**.
- **Incongruenza nei ticket.** Se c'è anche l'IP del DNS ed è diverso, il blocco «Informazioni sul PC» di apertura e risposta aggiunge una riga per l'operatore:
  `Attenzione: il record DNS del PC indica 192.0.2.20, diverso dall'IP della connessione (VPN, cambio di rete o record vecchio)`.
  Nessuna segnalazione all'utente (testata e dialog mostrano solo l'IP).
- Nessuna nuova variabile d'ambiente.
