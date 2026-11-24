package web

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// chatViewer: chi usa la chat. Verified = il browser ha fatto l'accesso con
// la password di dominio per questo stesso utente NTLM.
type chatViewer struct {
	Username, Name, PC string
	Verified           bool
}

// chatViewer: false se la chat è spenta, l'utente è anonimo o fuori dal
// pubblico della chat. Con AD giù resta solo chi è verificato (può leggere).
// Rinnova il cookie verificato una volta al giorno.
func (s *Server) chatViewer(w http.ResponseWriter, r *http.Request) (chatViewer, bool) {
	if !s.chatEnabled() {
		return chatViewer{}, false
	}
	u, ok := s.viewer(r)
	if !ok || u.Anonymous || u.Username == "" {
		return chatViewer{}, false
	}
	v := chatViewer{Username: strings.ToLower(u.Username), Name: u.Name, PC: u.PC}
	var sess identity.ChatSession
	if c, err := r.Cookie(identity.ChatCookieName); err == nil {
		if sess, ok = s.chatCookies.Decode(c.Value); ok && strings.EqualFold(sess.Username, v.Username) {
			v.Verified = true
		}
	}
	allowed, down := s.chatAllowed(v.Username)
	if !allowed && !(down && v.Verified) {
		return chatViewer{}, false
	}
	if v.Verified && s.now().Sub(time.Unix(sess.Issued, 0)) > identity.ChatRenewAfter {
		s.setChatCookie(w, r, v.Username)
	}
	return v, true
}

func (s *Server) setChatCookie(w http.ResponseWriter, r *http.Request, username string) {
	v, err := s.chatCookies.Encode(identity.ChatSession{Username: username})
	if err != nil {
		slog.Warn("cookie chat", "err", err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: identity.ChatCookieName, Value: v, Path: "/", MaxAge: int(identity.ChatTTL.Seconds()),
		HttpOnly: true, Secure: s.secureRequest(r), SameSite: http.SameSiteLaxMode})
}

// chatReply: sempre 200 (il proxy sostituirebbe i 4xx). Mai testo dei
// messaggi nei log: gli errori si scrivono con il solo codice.
func chatReply(w http.ResponseWriter, v map[string]any) {
	if _, ok := v["ok"]; !ok {
		v["ok"] = true
	}
	w.Header().Set("Cache-Control", "no-store")
	mediaJSON(w, v)
}

func chatFail(w http.ResponseWriter, code string) {
	chatReply(w, map[string]any{"ok": false, "errore": code})
}

// handleChatLogin: si chiede solo la password; lo username è quello NTLM
// (un campo username nel form è ignorato). Il bind LDAP verifica l'identità.
func (s *Server) handleChatLogin(w http.ResponseWriter, r *http.Request) {
	v, ok := s.chatViewer(w, r)
	if !ok {
		if u, _ := s.viewerName(r); u != "" {
			if _, down := s.chatAllowed(u); down {
				chatFail(w, "ad")
				return
			}
		}
		chatFail(w, "anonimo")
		return
	}
	key := "chat:" + v.Username
	if _, ok := s.limiter.Allow(key); !ok {
		chatFail(w, "bloccato")
		return
	}
	password := r.FormValue("password")
	if password == "" {
		chatFail(w, "credenziali")
		return
	}
	good, err := s.auth.Verify(v.Username, password)
	if err != nil {
		slog.Warn("chat: verifica LDAP", "err", err)
		chatFail(w, "ad")
		return
	}
	if !good {
		s.limiter.Fail(key)
		chatFail(w, "credenziali")
		return
	}
	s.limiter.Success(key)
	s.setChatCookie(w, r, v.Username)
	chatReply(w, map[string]any{})
}

func (s *Server) handleChatLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: identity.ChatCookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.secureRequest(r), SameSite: http.SameSiteLaxMode})
	chatReply(w, map[string]any{})
}
