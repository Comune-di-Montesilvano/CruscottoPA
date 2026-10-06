package web

import (
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
)

type loginView struct {
	Username string
	Error    string
	Version  string
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if s.currentAdmin(r) != "" {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	s.render(w, http.StatusOK, "admin_login.html", loginView{Version: s.version})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	user := strings.TrimSpace(r.FormValue("username"))
	pass := r.FormValue("password")
	view := loginView{Username: user, Version: s.version}
	// Finché il sotto-progetto 4 non definisce i proxy fidati, l'IP può essere
	// quello del reverse proxy: il limite per username resta comunque efficace.
	keys := []string{"u:" + strings.ToLower(user), "ip:" + clientIP(r)}

	if wait, ok := s.limiter.Allow(keys...); !ok {
		secs := int(math.Ceil(wait.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		view.Error = fmt.Sprintf("Troppi tentativi falliti. Riprova tra %d secondi.", secs)
		s.render(w, http.StatusTooManyRequests, "admin_login.html", view)
		return
	}

	ok, admin, err := s.auth.Authenticate(user, pass)
	if err != nil {
		slog.Warn("login: errore di autenticazione", "user", user, "err", err)
	}
	if err != nil || !ok {
		s.limiter.Fail(keys...)
		view.Error = "Credenziali non valide."
		if err != nil {
			view.Error = "Accesso non riuscito. Verifica le credenziali o riprova più tardi."
		}
		s.render(w, http.StatusUnauthorized, "admin_login.html", view)
		return
	}
	s.limiter.Success(keys...)

	if !admin {
		slog.Info("login: utente non amministratore", "user", user)
		view.Error = "Accesso non autorizzato: il tuo account non è amministratore di CruscottoPA."
		s.render(w, http.StatusForbidden, "admin_login.html", view)
		return
	}
	if err := s.startSession(w, r, user); err != nil {
		s.serverError(w, err)
		return
	}
	slog.Info("login amministratore", "user", user)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.endSession(w, r)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}
