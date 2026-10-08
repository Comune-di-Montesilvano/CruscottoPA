// Package web contiene handler HTTP, template e middleware di CruscottoPA.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/gorilla/sessions"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/auth"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/backup"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/guidesrc"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/notify"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

// repoURL: repository del progetto, linkato dal footer della plancia.
const repoURL = "https://github.com/Comune-di-Montesilvano/CruscottoPA"

type Options struct {
	DB     *database.DB
	Config config.Config
	Auth   auth.Authenticator
	// Directory: ricerca degli utenti riconosciuti via NTLM (nil = riconoscimento spento).
	Directory identity.Directory
	Backup    *backup.Service
	// RestoreDelay: attesa tra la risposta e lo swap del ripristino (0 → 500ms).
	RestoreDelay time.Duration
	Version      string
	WebDir       string
	Now          func() time.Time
	// GuideFetch scarica un file raw da GitHub (nil = guidesrc.NewFetcher().Fetch).
	GuideFetch func(ctx context.Context, rawURL string) (string, error)
	// Tickets: invio dei ticket a OTRS (nil = modulo spento).
	Tickets otrs.Client
}

type Server struct {
	db           *database.DB
	cfg          config.Config
	auth         auth.Authenticator
	directory    identity.Directory
	cookies      *identity.CookieCodec
	profiles     *profileCache
	membersCache *membersCache
	limiter      *auth.RateLimiter
	media        mediaUploads  // caricamenti a pezzi in corso (immagini e PDF)
	ticketFiles  ticketUploads // allegati dei ticket in caricamento
	backup       *backup.Service
	restoreDelay time.Duration
	branding     atomic.Pointer[database.Branding] // cache: caricata in New, aggiornata a ogni salvataggio
	tmpl         *template.Template
	store        *sessions.CookieStore
	version      string
	webDir       string
	now          func() time.Time
	fetchGuide   func(ctx context.Context, rawURL string) (string, error)
	mux          *http.ServeMux
	hub          *notify.Hub   // plance collegate a /eventi
	pusher       notify.Pusher // nil = Web Push spento
	vapidPublic  string
	notifyDone   chan struct{} // chiuso quando il dispatcher è terminato
	tickets      otrs.Client   // nil = modulo ticket spento
}

func New(o Options) (*Server, error) {
	if o.WebDir == "" {
		o.WebDir = "web"
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.GuideFetch == nil {
		o.GuideFetch = guidesrc.NewFetcher().Fetch
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
		directory:    o.Directory,
		limiter:      auth.NewRateLimiter(5, 15*time.Minute),
		media:        mediaUploads{byID: map[string]*mediaUpload{}},
		ticketFiles:  ticketUploads{byID: map[string]*ticketUpload{}},
		backup:       o.Backup,
		restoreDelay: o.RestoreDelay,
		version:      strings.TrimPrefix(o.Version, "v"), // tag "v0.3.0": la "v" la aggiungono i template
		webDir:       o.WebDir,
		now:          o.Now,
		fetchGuide:   o.GuideFetch,
		tickets:      o.Tickets,
		mux:          http.NewServeMux(),
	}
	b, err := o.DB.GetBranding()
	if err != nil {
		return nil, fmt.Errorf("branding: %w", err)
	}
	s.branding.Store(&b)
	tmpl, err := template.New("").Funcs(s.funcs()).ParseGlob(filepath.Join(o.WebDir, "templates", "*.html"))
	if err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	s.tmpl = tmpl
	s.store = newSessionStore(o.Config.SessionSecret)
	s.cookies = identity.NewCookieCodec(o.Config.SessionSecret)
	s.profiles = newProfileCache(o.Now)
	s.membersCache = newMembersCache(o.Now)
	s.hub = notify.NewHub(2000)
	if o.Config.VAPIDSubject != "" {
		pub, priv, err := o.DB.EnsureVAPIDKeys(webpush.GenerateVAPIDKeys)
		if err != nil {
			return nil, fmt.Errorf("chiavi VAPID: %w", err)
		}
		s.vapidPublic = pub
		s.pusher = &notify.WebPusher{Subject: o.Config.VAPIDSubject, PublicKey: pub, PrivateKey: priv}
	}
	s.routes()
	// Allegati dei ticket lasciati a metà da un riavvio: nessuno li userà più.
	os.RemoveAll(s.ticketTmpDir())
	return s, nil
}

// StartNotifications avvia il dispatcher delle notifiche (ogni 30 s).
func (s *Server) StartNotifications(ctx context.Context) {
	d := &notify.Dispatcher{Store: s.db, Hub: s.hub, Pusher: s.pusher, Visible: s.alertVisibleTo, Now: s.now}
	s.notifyDone = make(chan struct{})
	go func() {
		defer close(s.notifyDone)
		d.Run(ctx, 30*time.Second)
	}()
}

// Close chiude i flussi SSE aperti (senza, lo shutdown attenderebbe ogni
// client) e aspetta che il dispatcher, fermato dal ctx di StartNotifications,
// finisca il ciclo in corso: dopo si può chiudere il database.
func (s *Server) Close() {
	s.hub.Close()
	if s.notifyDone == nil {
		return
	}
	select {
	case <-s.notifyDone:
	case <-time.After(5 * time.Second):
		slog.Warn("notifiche: dispatcher ancora attivo allo spegnimento")
	}
}

func (s *Server) loc() *time.Location { return s.cfg.Location }

// ente restituisce il branding corrente. Il ripristino di un backup fa
// ripartire il processo, quindi la cache non resta mai indietro rispetto al DB.
func (s *Server) ente() database.Branding { return *s.branding.Load() }

// Handler applica, dall'esterno: header di sicurezza, protezione CSRF
// (Sec-Fetch-Site/Origin, solo metodi non sicuri), routing.
func (s *Server) Handler() http.Handler {
	return securityHeaders(http.NewCrossOriginProtection().Handler(s.mux))
}

func (s *Server) routes() {
	static := http.FileServer(http.Dir(filepath.Join(s.webDir, "static")))
	s.mux.Handle("GET /static/", revalidate(noListing(http.StripPrefix("/static/", static))))
	s.mux.Handle("GET /favicon.ico", revalidate(http.HandlerFunc(s.handleFavicon)))
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /io", s.handleIo)
	s.mux.HandleFunc("GET /eventi", s.handleEvents)
	s.mux.HandleFunc("GET /push/chiave", s.handlePushKey)
	s.mux.HandleFunc("POST /push/iscrizioni", s.handlePushSubscribe)
	s.mux.HandleFunc("POST /push/ricevuta", s.handlePushReceipt)
	s.mux.HandleFunc("POST /push/iscrizioni/rimuovi", s.handlePushUnsubscribe)
	s.mux.HandleFunc("GET /sw.js", s.handleServiceWorker)
	s.mux.HandleFunc("GET /manifest.webmanifest", s.handleManifest)
	s.mux.HandleFunc("GET /{$}", s.handleDashboard)
	s.mux.HandleFunc("GET /partials/alerts", s.handleAlertsPartial)
	s.mux.HandleFunc("GET /partials/calendario", s.handleCalendarPartial)
	s.mux.HandleFunc("GET /avvisi", s.handleAvvisi)
	s.mux.HandleFunc("GET /avvisi/{id}", s.handleAvviso)
	s.mux.HandleFunc("POST /avvisi/{id}/letto", s.handleAlertRead)
	s.mux.HandleFunc("POST /presenza", s.handlePresence)
	s.mux.HandleFunc("POST /ticket", s.handleTicketSend)
	s.mux.HandleFunc("POST /ticket/allegati", s.handleTicketFileStart)
	s.mux.HandleFunc("POST /ticket/allegati/{id}/pezzo", s.handleTicketFileChunk)
	s.mux.HandleFunc("POST /ticket/allegati/{id}/fine", s.handleTicketFileFinish)
	s.mux.HandleFunc("GET /guide/{id}", s.handleGuidePage)
	s.mux.HandleFunc("GET /guide/{id}/pdf", s.handleGuidePDF)
	s.mux.HandleFunc("GET /admin/login", s.handleLoginForm)
	s.mux.HandleFunc("POST /admin/login", s.handleLogin)
	s.mux.HandleFunc("POST /admin/logout", s.handleLogout)
	s.mux.HandleFunc("GET /admin", s.requireAdmin(s.handleOverview))
	s.mux.HandleFunc("GET /admin/ente", s.requireAdmin(s.handleBrandingPage))
	s.mux.HandleFunc("GET /admin/gruppi", s.requireAdmin(s.handleAudiencePage))
	s.mux.HandleFunc("POST /admin/gruppi", s.requireAdmin(s.handleAudienceGroupCreate))
	s.mux.HandleFunc("POST /admin/gruppi/attributi", s.requireAdmin(s.handleAttributeAdd))
	s.mux.HandleFunc("POST /admin/gruppi/attributi/{id}/elimina", s.requireAdmin(s.handleAttributeDelete))
	s.mux.HandleFunc("POST /admin/gruppi/attributi/{id}/testata", s.requireAdmin(s.handleAttributeHero))
	s.mux.HandleFunc("POST /admin/gruppi/attributi/{id}/sposta", s.requireAdmin(s.handleAttributeHeroMove))
	s.mux.HandleFunc("GET /admin/gruppi/{id}/modifica", s.requireAdmin(s.handleAudienceGroupEdit))
	s.mux.HandleFunc("POST /admin/gruppi/{id}", s.requireAdmin(s.handleAudienceGroupRename))
	s.mux.HandleFunc("POST /admin/gruppi/{id}/elimina", s.requireAdmin(s.handleAudienceGroupDelete))
	s.mux.HandleFunc("POST /admin/gruppi/{id}/sposta", s.requireAdmin(s.handleAudienceGroupMove))
	s.mux.HandleFunc("POST /admin/gruppi/{id}/regole", s.requireAdmin(s.handleRuleAdd))
	s.mux.HandleFunc("POST /admin/gruppi/{id}/regole/{rid}/elimina", s.requireAdmin(s.handleRuleDelete))
	s.mux.HandleFunc("POST /admin/gruppi/{id}/anteprima", s.requireAdmin(s.handleAudiencePreview))
	s.mux.HandleFunc("GET /admin/gruppi/{id}/anteprima", s.requireAdmin(s.handleAudiencePreview))
	s.mux.HandleFunc("GET /admin/gruppi/{id}/membri", s.requireAdmin(s.handleMemberCount))
	s.mux.HandleFunc("GET /admin/gruppi/{id}/bozza", s.requireAdmin(s.handleDraftPreview))
	s.mux.HandleFunc("GET /admin/ad/attributi", s.requireAdmin(s.handleSuggestAttributes))
	s.mux.HandleFunc("GET /admin/ad/suggerimenti", s.requireAdmin(s.handleSuggest))
	s.mux.HandleFunc("GET /admin/ad/valori", s.requireAdmin(s.handleSuggestValues))
	s.mux.HandleFunc("GET /admin/ad/gruppi", s.requireAdmin(s.handleSuggestGroups))
	s.mux.HandleFunc("GET /admin/ad/utenti", s.requireAdmin(s.handleSuggestUsers))
	s.mux.HandleFunc("POST /admin/ente", s.requireAdmin(s.handleBrandingSave))
	s.mux.HandleFunc("GET /uploads/icons/{file}", s.handleUploadFile(uploadIcons))
	s.mux.HandleFunc("GET /uploads/branding/{file}", s.handleUploadFile(uploadBranding))
	s.mux.HandleFunc("GET /uploads/guide/{file}", s.handleGuideImage)
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
	s.mux.HandleFunc("POST /admin/guide/{id}/aggiorna", s.requireAdmin(s.handleGuideRefresh))
	s.mux.HandleFunc("GET /admin/avvisi", s.requireAdmin(s.handleAlertsPage))
	s.mux.HandleFunc("GET /admin/avvisi/{id}/modifica", s.requireAdmin(s.handleAlertEdit))
	s.mux.HandleFunc("GET /admin/avvisi/{id}/letture", s.requireAdmin(s.handleAlertReaders))
	s.mux.HandleFunc("GET /admin/utenti", s.requireAdmin(s.handleUsers))
	s.mux.HandleFunc("POST /admin/avvisi", s.requireAdmin(s.handleAlertSave))
	s.mux.HandleFunc("POST /admin/notifiche/prova", s.requireAdmin(s.handlePushTest))
	s.mux.HandleFunc("POST /admin/avvisi/{id}", s.requireAdmin(s.handleAlertSave))
	s.mux.HandleFunc("POST /admin/avvisi/{id}/elimina", s.requireAdmin(s.handleAlertDelete))
	s.mux.HandleFunc("GET /admin/calendario", s.requireAdmin(s.handleCalendarPage))
	s.mux.HandleFunc("GET /admin/calendario/{id}/modifica", s.requireAdmin(s.handleCalendarEdit))
	s.mux.HandleFunc("POST /admin/calendario", s.requireAdmin(s.handleCalendarSave))
	s.mux.HandleFunc("POST /admin/calendario/{id}", s.requireAdmin(s.handleCalendarSave))
	s.mux.HandleFunc("POST /admin/calendario/{id}/elimina", s.requireAdmin(s.handleCalendarDelete))
	s.mux.HandleFunc("GET /admin/assistenza", s.requireAdmin(s.handleSupportPage))
	s.mux.HandleFunc("GET /admin/ticket", s.requireAdmin(s.handleAdminTickets))
	s.mux.HandleFunc("GET /admin/assistenza/{id}/modifica", s.requireAdmin(s.handleSupportEdit))
	s.mux.HandleFunc("POST /admin/assistenza", s.requireAdmin(s.handleSupportSave))
	s.mux.HandleFunc("POST /admin/assistenza/{id}", s.requireAdmin(s.handleSupportSave))
	s.mux.HandleFunc("POST /admin/assistenza/{id}/elimina", s.requireAdmin(s.handleSupportDelete))
	s.mux.HandleFunc("POST /admin/assistenza/{id}/sposta", s.requireAdmin(s.handleSupportMove))
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
	s.mux.HandleFunc("POST /admin/anteprima", s.requireAdmin(s.handlePreview))
	s.mux.HandleFunc("POST /admin/media", s.requireAdmin(s.handleMediaStart))
	s.mux.HandleFunc("POST /admin/media/{id}/pezzo", s.requireAdmin(s.handleMediaChunk))
	s.mux.HandleFunc("POST /admin/media/{id}/fine", s.requireAdmin(s.handleMediaFinish))
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

// handleFavicon serve la favicon fissa di CruscottoPA a chi la chiede senza
// leggere l'HTML. Il tipo è esplicito: .ico non è nella tabella MIME di Go.
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/x-icon")
	http.ServeFile(w, r, filepath.Join(s.webDir, "static", "img", "favicon.ico"))
}

// noListing: niente elenco del contenuto delle cartelle di /static/.
func noListing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
