// Package web contiene handler HTTP, template e middleware di CruscottoPA.
package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gorilla/sessions"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/auth"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type Options struct {
	DB      *database.DB
	Config  config.Config
	Auth    auth.Authenticator
	Version string
	WebDir  string
	Now     func() time.Time
}

type Server struct {
	db      *database.DB
	cfg     config.Config
	auth    auth.Authenticator
	limiter *auth.RateLimiter
	tmpl    *template.Template
	store   *sessions.CookieStore
	version string
	webDir  string
	now     func() time.Time
	mux     *http.ServeMux
}

func New(o Options) (*Server, error) {
	if o.WebDir == "" {
		o.WebDir = "web"
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Config.Location == nil {
		o.Config.Location = time.UTC
	}
	s := &Server{
		db:      o.DB,
		cfg:     o.Config,
		auth:    o.Auth,
		limiter: auth.NewRateLimiter(5, 15*time.Minute),
		version: o.Version,
		webDir:  o.WebDir,
		now:     o.Now,
		mux:     http.NewServeMux(),
	}
	tmpl, err := template.New("").Funcs(s.funcs()).ParseGlob(filepath.Join(o.WebDir, "templates", "*.html"))
	if err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	s.tmpl = tmpl
	s.routes()
	return s, nil
}

func (s *Server) loc() *time.Location { return s.cfg.Location }

// Handler applica, dall'esterno: header di sicurezza, protezione CSRF
// (Sec-Fetch-Site/Origin, solo metodi non sicuri), routing.
func (s *Server) Handler() http.Handler {
	return securityHeaders(http.NewCrossOriginProtection().Handler(s.mux))
}

func (s *Server) routes() {
	static := http.FileServer(http.Dir(filepath.Join(s.webDir, "static")))
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", static))
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /{$}", s.handleIndex)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status, code := "ok", http.StatusOK
	if err := s.db.PingContext(r.Context()); err != nil {
		status, code = "db unavailable", http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"status": status, "version": s.version})
}

// handleIndex è temporaneo: il Task 8 lo sostituisce con la plancia.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "index.html", map[string]any{"Version": s.version})
}
