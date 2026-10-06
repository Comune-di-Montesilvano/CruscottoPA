# Security Policy

## Segnalazione di una vulnerabilità

Se scopri una vulnerabilità di sicurezza in CruscottoPA, **non aprire una issue pubblica**.

Segnala privatamente via [GitHub Security Advisories](https://github.com/Comune-di-Montesilvano/CruscottoPA/security/advisories/new) — visibile solo ai maintainer finché non viene risolta e pubblicata.

## Versioni supportate

Solo l'ultima versione taggata riceve fix di sicurezza.

## Cosa aspettarsi

- Nessun bug bounty.
- Le vulnerabilità nelle dipendenze sono monitorate automaticamente (Dependabot + Trivy su go.mod/go.sum ad ogni push/PR + Trivy sull'immagine container ad ogni release, risultati nella tab [Security](https://github.com/Comune-di-Montesilvano/CruscottoPA/security)).
