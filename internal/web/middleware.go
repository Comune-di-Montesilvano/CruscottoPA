package web

import "net/http"

// cspPolicy: tutto self-hosted; img https per le icone da URL esterno;
// style-src-attr per lo style="background:#rrggbb" delle icone.
const cspPolicy = "default-src 'self'; img-src 'self' https: data:; style-src 'self'; " +
	"style-src-attr 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'; " +
	"base-uri 'self'; form-action 'self'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", cspPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
