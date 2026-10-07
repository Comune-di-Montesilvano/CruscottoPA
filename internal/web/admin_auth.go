package web

import (
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/auth"
)

type loginView struct {
	Username string
	Error    string
	Version  string
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if s.currentAdmin(r) != "" {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	s.render(w, http.StatusOK, "admin_login.html", loginView{Version: s.version})
}

// handleLogin risponde sempre 200 con l'errore in pagina: il reverse proxy in
// produzione sostituisce le risposte 4xx/5xx con una pagina di cortesia.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	user := strings.TrimSpace(r.FormValue("username"))
	pass := r.FormValue("password")
	view := loginView{Username: user, Version: s.version}
	// Solo per username: dietro Podman rootless RemoteAddr è uguale per tutti e
	// una chiave per IP bloccherebbe ogni admin. Restano i blocchi account di AD.
	keys := []string{"u:" + strings.ToLower(user)}

	if wait, ok := s.limiter.Allow(keys...); !ok {
		secs := int(math.Ceil(wait.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		view.Error = fmt.Sprintf("Troppi tentativi falliti. Riprova tra %d secondi.", secs)
		s.render(w, http.StatusOK, "admin_login.html", view)
		return
	}

	ok, admin, err := s.auth.Authenticate(user, pass)
	if err != nil {
		slog.Warn("login: errore di autenticazione", "user", auth.SafeLog(user), "err", err)
	}
	if err != nil || !ok {
		s.limiter.Fail(keys...)
		view.Error = "Credenziali non valide."
		if err != nil {
			view.Error = "Accesso non riuscito. Verifica le credenziali o riprova più tardi."
		}
		s.render(w, http.StatusOK, "admin_login.html", view)
		return
	}
	s.limiter.Success(keys...)

	if !admin {
		slog.Info("login: utente non amministratore", "user", auth.SafeLog(user))
		view.Error = "Accesso non autorizzato: il tuo account non è amministratore di CruscottoPA."
		s.render(w, http.StatusOK, "admin_login.html", view)
		return
	}
	if s.cookieWouldBeDropped(r) {
		slog.Warn("login: cookie Secure richiesto su HTTP in chiaro", "host", auth.SafeLog(r.Host))
		view.Error = "Il cookie di sessione richiede HTTPS: accedi tramite https:// oppure, solo su rete interna fidata, imposta SECURE_COOKIES=false."
		s.render(w, http.StatusOK, "admin_login.html", view)
		return
	}
	if err := s.startSession(w, r, user); err != nil {
		s.serverError(w, err)
		return
	}
	slog.Info("login amministratore", "user", auth.SafeLog(user))
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.endSession(w, r)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}
