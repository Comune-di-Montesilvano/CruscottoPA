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
	authz := r.Header.Get("Authorization")
	if authz == "" {
		// Se il browser non prosegue resta anonimo per 24 ore, senza ritentare.
		s.setViewer(w, r, identity.User{Anonymous: true})
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
		s.finishRecognition(w, r, msg)
	default:
		http.Error(w, "Messaggio NTLM non valido", http.StatusBadRequest)
	}
}

func (s *Server) finishRecognition(w http.ResponseWriter, r *http.Request, msg []byte) {
	login, err := identity.ParseAuthenticate(msg)
	if err != nil {
		http.Error(w, "Messaggio NTLM non valido", http.StatusBadRequest)
		return
	}
	if !strings.EqualFold(login.Domain, s.cfg.NTLMDomain) {
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
		s.recognized(w, r, identity.User{Username: p.Username, Name: p.Name, GivenName: p.GivenName})
	}
}

func (s *Server) recognized(w http.ResponseWriter, r *http.Request, u identity.User) {
	s.setViewer(w, r, u)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"riconosciuto": !u.Anonymous, "nome": u.FirstName()})
}
