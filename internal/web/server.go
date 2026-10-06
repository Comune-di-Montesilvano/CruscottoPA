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
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/backup"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type Options struct {
	DB     *database.DB
	Config config.Config
	Auth   auth.Authenticator
	Backup *backup.Service
	// RestoreDelay: attesa tra la risposta e lo swap del ripristino (0 → 500ms).
	RestoreDelay time.Duration
	Version      string
	WebDir       string
	Now          func() time.Time
}

type Server struct {
	db           *database.DB
	cfg          config.Config
	auth         auth.Authenticator
	limiter      *auth.RateLimiter
	backup       *backup.Service
	restoreDelay time.Duration
	tmpl         *template.Template
	store        *sessions.CookieStore
	version      string
	webDir       string
	now          func() time.Time
	mux          *http.ServeMux
}

func New(o Options) (*Server, error) {
	if o.WebDir == "" {
		o.WebDir = "web"
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.RestoreDelay == 0 {
		o.RestoreDelay = 500 * time.Millisecond
	}
	if o.Config.Location == nil {
		o.Config.Location = time.UTC
	}
	s := &Server{
		db:           o.DB,
		cfg:          o.Config,
		auth:         o.Auth,
		limiter:      auth.NewRateLimiter(5, 15*time.Minute),
		backup:       o.Backup,
		restoreDelay: o.RestoreDelay,
		version:      o.Version,
		webDir:       o.WebDir,
		now:          o.Now,
		mux:          http.NewServeMux(),
	}
	tmpl, err := template.New("").Funcs(s.funcs()).ParseGlob(filepath.Join(o.WebDir, "templates", "*.html"))
	if err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	s.tmpl = tmpl
	s.store = newSessionStore(o.Config.SessionSecret)
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
	s.mux.Handle("GET /static/", revalidate(http.StripPrefix("/static/", static)))
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /{$}", s.handleDashboard)
	s.mux.HandleFunc("GET /partials/alerts", s.handleAlertsPartial)
	s.mux.HandleFunc("GET /partials/calendario", s.handleCalendarPartial)
	s.mux.HandleFunc("GET /avvisi", s.handleAvvisi)
	s.mux.HandleFunc("GET /admin/login", s.handleLoginForm)
	s.mux.HandleFunc("POST /admin/login", s.handleLogin)
	s.mux.HandleFunc("POST /admin/logout", s.handleLogout)
	s.mux.HandleFunc("GET /admin", s.requireAdmin(s.handleOverview))
	s.mux.HandleFunc("GET /uploads/icons/{file}", s.handleUploadFile(uploadIcons))
	s.mux.HandleFunc("GET /uploads/branding/{file}", s.handleUploadFile(uploadBranding))
	s.mux.HandleFunc("GET /admin/icone", s.requireAdmin(s.handleIconSearch))
	s.mux.HandleFunc("GET /admin/app", s.requireAdmin(s.handleAppsPage))
	s.mux.HandleFunc("GET /admin/app/{id}/modifica", s.requireAdmin(s.handleAppEdit))
	s.mux.HandleFunc("POST /admin/app", s.requireAdmin(s.handleAppSave))
	s.mux.HandleFunc("POST /admin/app/{id}", s.requireAdmin(s.handleAppSave))
	s.mux.HandleFunc("POST /admin/app/{id}/elimina", s.requireAdmin(s.handleAppDelete))
	s.mux.HandleFunc("POST /admin/app/{id}/sposta", s.requireAdmin(s.handleAppMove))
	s.mux.HandleFunc("GET /admin/guide", s.requireAdmin(s.handleGuidesPage))
	s.mux.HandleFunc("GET /admin/guide/{id}/modifica", s.requireAdmin(s.handleGuideEdit))
	s.mux.HandleFunc("POST /admin/guide", s.requireAdmin(s.handleGuideSave))
	s.mux.HandleFunc("POST /admin/guide/{id}", s.requireAdmin(s.handleGuideSave))
	s.mux.HandleFunc("POST /admin/guide/{id}/elimina", s.requireAdmin(s.handleGuideDelete))
	s.mux.HandleFunc("POST /admin/guide/{id}/sposta", s.requireAdmin(s.handleGuideMove))
	s.mux.HandleFunc("GET /admin/avvisi", s.requireAdmin(s.handleAlertsPage))
	s.mux.HandleFunc("GET /admin/avvisi/{id}/modifica", s.requireAdmin(s.handleAlertEdit))
	s.mux.HandleFunc("POST /admin/avvisi", s.requireAdmin(s.handleAlertSave))
	s.mux.HandleFunc("POST /admin/avvisi/{id}", s.requireAdmin(s.handleAlertSave))
	s.mux.HandleFunc("POST /admin/avvisi/{id}/elimina", s.requireAdmin(s.handleAlertDelete))
	s.mux.HandleFunc("GET /admin/calendario", s.requireAdmin(s.handleCalendarPage))
	s.mux.HandleFunc("GET /admin/calendario/{id}/modifica", s.requireAdmin(s.handleCalendarEdit))
	s.mux.HandleFunc("POST /admin/calendario", s.requireAdmin(s.handleCalendarSave))
	s.mux.HandleFunc("POST /admin/calendario/{id}", s.requireAdmin(s.handleCalendarSave))
	s.mux.HandleFunc("POST /admin/calendario/{id}/elimina", s.requireAdmin(s.handleCalendarDelete))
	s.mux.HandleFunc("GET /admin/categorie", s.requireAdmin(s.handleCategoriesPage))
	s.mux.HandleFunc("GET /admin/categorie/{id}/modifica", s.requireAdmin(s.handleCategoryEdit))
	s.mux.HandleFunc("POST /admin/categorie", s.requireAdmin(s.handleCategorySave))
	s.mux.HandleFunc("POST /admin/categorie/{id}", s.requireAdmin(s.handleCategorySave))
	s.mux.HandleFunc("POST /admin/categorie/{id}/elimina", s.requireAdmin(s.handleCategoryDelete))
	s.mux.HandleFunc("POST /admin/categorie/{id}/sposta", s.requireAdmin(s.handleCategoryMove))
	s.mux.HandleFunc("GET /admin/backup", s.requireAdmin(s.handleBackupPage))
	s.mux.HandleFunc("POST /admin/backup", s.requireAdmin(s.handleBackupCreate))
	s.mux.HandleFunc("GET /admin/backup/{name}", s.requireAdmin(s.handleBackupDownload))
	s.mux.HandleFunc("POST /admin/backup/{name}/elimina", s.requireAdmin(s.handleBackupDelete))
	s.mux.HandleFunc("POST /admin/backup/{name}/ripristina", s.requireAdmin(s.handleBackupRestore))
	s.mux.HandleFunc("POST /admin/backup/upload", s.requireAdmin(s.handleBackupUploadStart))
	s.mux.HandleFunc("POST /admin/backup/upload/{id}/chunk", s.requireAdmin(s.handleBackupUploadChunk))
	s.mux.HandleFunc("POST /admin/backup/upload/{id}/fine", s.requireAdmin(s.handleBackupUploadFinish))
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

// revalidate impone al browser di ricontrollare gli statici a ogni uso
// (304 se invariati): dopo un aggiornamento nessuno resta con JS/CSS vecchi.
func revalidate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}
