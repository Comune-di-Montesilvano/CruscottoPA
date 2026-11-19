# IP reale del PC dal reverse proxy

## Contesto

Finora l'IP del PC («Questo PC» in testata, dialog e blocco «Informazioni sul PC» dei ticket) veniva solo dal DNS di AD: nome della workstation NTLM + `PC_DNS_SUFFIX`. Si pensava che l'IP del client non arrivasse al container: `RemoteAddr` è sempre l'indirizzo della rete di Podman.

Prova del 2026-11-19 con `traefik/whoami` al posto del Cruscotto di test, dietro revprx01: nginx invia `X-Real-Ip` e `X-Forwarded-For` con l'IP vero del PC (confermato con `ipconfig`), nessun NAT tra gli uffici e il proxy. Solo `RemoteAddr` è inutile.

## Decisioni

- **Fiducia con un segreto condiviso.** `RemoteAddr` non distingue revprx01 da una connessione diretta alla porta pubblicata, quindi chiunque raggiunga `host:porta` potrebbe scrivere `X-Real-Ip`. nginx aggiunge `X-Proxy-Token: <segreto>`; il Cruscotto usa `X-Real-Ip` solo se il token coincide (`PROXY_TOKEN`, confronto a tempo costante). `PROXY_TOKEN` vuoto = header ignorati (fail-closed), resta il DNS.
- **Solo IPv4**, come il DNS (serve a VNC). Altro → ignorato.
- **IP mostrato** (testata, dialog, ticket): quello della connessione se c'è, altrimenti il DNS come oggi.
- **Incongruenza nei ticket.** Se entrambi ci sono e differiscono, il blocco «Informazioni sul PC» di apertura e risposta aggiunge una riga per l'operatore:
  `Attenzione: il DNS indica 192.0.2.20 per questo PC (VPN, cambio di rete o record DNS vecchio)`.
  Nessuna segnalazione all'utente (testata e dialog mostrano solo l'IP).
- Resta un'etichetta per l'assistenza, **mai per autorizzare**.

## Configurazione nginx (revprx01)

```nginx
proxy_set_header X-Real-IP $remote_addr;
proxy_set_header X-Proxy-Token "<segreto>";
```

`X-Real-Ip` c'è già; va aggiunto solo il token. Il segreto si mette anche in `PROXY_TOKEN` (`docker-compose.yml`, `.env.example`, config).
