package web

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/errtrack"
)

// recoverPanic: un panic in un handler diventa un evento su GlitchTip (uno
// solo: il log su stdout porta SkipKey) e un 500. http.ErrAbortHandler non è
// un errore e prosegue come in net/http.
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			s.errs.CapturePanic(v, errtrack.Request{Method: r.Method, Path: r.URL.Path}, s.reportUser(r))
			slog.Error("panic", "valore", fmt.Sprint(v), "stack", string(debug.Stack()), errtrack.SkipKey, true)
			http.Error(w, "Si è verificato un errore. Riprova più tardi.", http.StatusInternalServerError)
		}()
		next.ServeHTTP(w, r)
	})
}

// reportUser: hash dell'utente per GlitchTip ("" se anonimo). Plancia: cookie
// dell'utente riconosciuto; admin: username della sessione.
func (s *Server) reportUser(r *http.Request) string {
	if !s.errs.Enabled() {
		return ""
	}
	if u, ok := s.viewer(r); ok && !u.Anonymous && u.Username != "" {
		return s.errs.UserID(u.Username)
	}
	if strings.HasPrefix(r.URL.Path, "/admin") {
		return s.errs.UserID(s.currentAdmin(r))
	}
	return ""
}
