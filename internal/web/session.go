package web

import (
	"crypto/sha256"
	"net"
	"net/http"
	"strings"

	"github.com/gorilla/sessions"
)

const (
	sessionName   = "cruscotto_admin"
	sessionMaxAge = 8 * 60 * 60
)

// newSessionStore deriva da SESSION_SECRET una chiave di firma e una di cifratura (AES-256).
func newSessionStore(secret string) *sessions.CookieStore {
	hashKey := sha256.Sum256([]byte("auth:" + secret))
	encKey := sha256.Sum256([]byte("enc:" + secret))
	st := sessions.NewCookieStore(hashKey[:], encKey[:])
	st.Options = &sessions.Options{
		Path:     "/admin",
		MaxAge:   sessionMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	st.MaxAge(sessionMaxAge)
	return st
}

// secureRequest: dietro reverse proxy decide X-Forwarded-Proto; altrimenti TLS diretto o SECURE_COOKIES.
func (s *Server) secureRequest(r *http.Request) bool {
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		return strings.EqualFold(p, "https")
	}
	return r.TLS != nil || s.cfg.SecureCookies
}

func (s *Server) sessionOptions(r *http.Request, maxAge int) *sessions.Options {
	opts := *s.store.Options
	opts.Secure = s.secureRequest(r)
	opts.MaxAge = maxAge
	return &opts
}

// currentAdmin restituisce lo username in sessione, "" se assente o non valida.
func (s *Server) currentAdmin(r *http.Request) string {
	sess, err := s.store.Get(r, sessionName)
	if err != nil {
		return ""
	}
	user, _ := sess.Values["user"].(string)
	return user
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user string) error {
	sess, _ := s.store.New(r, sessionName) // un cookie vecchio/illeggibile viene sostituito
	sess.Values["user"] = user
	sess.Options = s.sessionOptions(r, sessionMaxAge)
	return sess.Save(r, w)
}

func (s *Server) endSession(w http.ResponseWriter, r *http.Request) {
	sess, _ := s.store.Get(r, sessionName)
	sess.Options = s.sessionOptions(r, -1)
	sess.Save(r, w)
}

// requireAdmin: senza sessione 303 al login; per HTMX 401 + HX-Redirect,
// così il login non finisce dentro un frammento della pagina.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.currentAdmin(r) == "" {
			if r.Header.Get("HX-Request") == "true" {
				// 200, non 401: il reverse proxy in produzione sostituisce le
				// risposte 4xx/5xx con una pagina di cortesia e toglie HX-Redirect.
				w.Header().Set("HX-Redirect", "/admin/login")
				w.WriteHeader(http.StatusOK)
				return
			}
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// cookieWouldBeDropped: cookie Secure su HTTP in chiaro verso un host diverso da
// localhost — il browser lo scarta e il login tornerebbe in silenzio alla pagina d'accesso.
func (s *Server) cookieWouldBeDropped(r *http.Request) bool {
	if !s.secureRequest(r) || r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host != "localhost" && host != "127.0.0.1" && host != "::1"
}
