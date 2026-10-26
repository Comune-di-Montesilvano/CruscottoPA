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

// canRecognize: riconoscimento attivo e cookie che il browser terrà davvero.
// Un cookie Secure su HTTP in chiaro verrebbe scartato e dashboard.js
// ricaricherebbe la pagina all'infinito.
func (s *Server) canRecognize(r *http.Request) bool {
	return s.recognitionEnabled() && !s.cookieWouldBeDropped(r)
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
	if !s.canRecognize(r) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	// aggiorna=1: chi è già riconosciuto rinnova il nome del PC (una volta per
	// sessione). Un handshake non riuscito non tocca il cookie.
	refresh := r.URL.Query().Get("aggiorna") == "1"
	authz := r.Header.Get("Authorization")
	if authz == "" {
		// Se il browser non prosegue resta anonimo per 24 ore, senza ritentare.
		if !refresh {
			s.setViewer(w, r, identity.User{Anonymous: true})
		}
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
		s.finishRecognition(w, r, msg, refresh)
	default:
		http.Error(w, "Messaggio NTLM non valido", http.StatusBadRequest)
	}
}

func (s *Server) finishRecognition(w http.ResponseWriter, r *http.Request, msg []byte, refresh bool) {
	login, err := identity.ParseAuthenticate(msg)
	if err != nil {
		http.Error(w, "Messaggio NTLM non valido", http.StatusBadRequest)
		return
	}
	if refresh {
		s.refreshRecognition(w, r, login)
		return
	}
	if !s.ntlmDomainOK(login) {
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
		// Via il cookie anonimo del primo passo: si ritenta alla prossima visita.
		http.SetCookie(w, &http.Cookie{Name: identity.CookieName, Value: "", Path: "/", MaxAge: -1,
			HttpOnly: true, Secure: s.secureRequest(r), SameSite: http.SameSiteLaxMode})
		http.Error(w, "Directory non disponibile", http.StatusServiceUnavailable)
	default:
		s.recognized(w, r, identity.User{Username: p.Username, Name: p.Name, GivenName: p.GivenName, PC: cleanPC(login.Workstation)})
	}
}

// refreshRecognition: come finishRecognition, ma ogni esito diverso dal
// riconoscimento lascia il cookie com'è (AD giù, dominio o utente diversi).
func (s *Server) refreshRecognition(w http.ResponseWriter, r *http.Request, login identity.Login) {
	if s.ntlmDomainOK(login) {
		if p, err := s.directory.Lookup(login.User); err == nil {
			s.recognized(w, r, identity.User{Username: p.Username, Name: p.Name, GivenName: p.GivenName, PC: cleanPC(login.Workstation)})
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"riconosciuto":false}`))
}

// ntlmDomainOK: dominio NetBIOS configurato, oppure dominio vuoto con un UPN
// (utente@dominio), che la directory cerca per userPrincipalName.
func (s *Server) ntlmDomainOK(login identity.Login) bool {
	if login.Domain == "" {
		return strings.Contains(login.User, "@")
	}
	return strings.EqualFold(login.Domain, s.cfg.NTLMDomain)
}

// cleanPC: nome NetBIOS del PC, solo lettere, cifre, '.', '_' e '-', max 63.
func cleanPC(ws string) string {
	var b strings.Builder
	for _, c := range ws {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			b.WriteRune(c)
			if b.Len() == 63 {
				break
			}
		}
	}
	return b.String()
}

func (s *Server) recognized(w http.ResponseWriter, r *http.Request, u identity.User) {
	s.setViewer(w, r, u)
	w.Header().Set("Content-Type", "application/json")
	// Niente nome nella risposta: chiunque in rete potrebbe usare /io per
	// scoprire i nomi dei colleghi. La ricarica della pagina lo mostra comunque.
	if err := json.NewEncoder(w).Encode(map[string]bool{"riconosciuto": !u.Anonymous}); err != nil {
		slog.Debug("risposta /io", "err", err)
	}
}
