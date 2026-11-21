// Command server avvia il portale Intranet CruscottoPA.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // fusi orari embedded: TZ funziona anche senza tzdata di sistema

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/auth"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/backup"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/errtrack"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/web"
)

// AppVersion è iniettata in fase di build:
//
//	go build -ldflags "-X main.AppVersion=1.2.3" ./cmd/server
//
// In CI (release.yml) coincide con il tag git; le build locali senza
// build-arg restano "dev" (o "dev-DEV" via docker-compose.override.yml).
var AppVersion = "dev"

func main() {
	healthcheck := flag.Bool("healthcheck", false, "esegue GET /health su localhost ed esce (usato dal HEALTHCHECK del container)")
	flag.Parse()

	// Il binario fa da healthcheck di sé stesso: l'immagine non dipende da wget/curl.
	// Non deve dipendere dalla validità del resto della configurazione.
	if *healthcheck {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		os.Exit(runHealthcheck(port))
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configurazione:", err)
		os.Exit(1)
	}

	// GlitchTip prima di tutto il resto: anche gli errori di avvio vi arrivano.
	tracker, terr := errtrack.New(errtrack.Options{
		DSN:         cfg.SentryDSN,
		Environment: cfg.SentryEnvironment,
		Release:     AppVersion,
		Secret:      cfg.SessionSecret,
	})
	logger := slog.New(tracker.Handler(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})))
	slog.SetDefault(logger)
	if terr != nil {
		slog.Warn("SENTRY_DSN non valida: errori non inviati a GlitchTip", "err", terr)
	}
	// fatal: log, invio degli eventi in coda, uscita.
	fatal := func(msg string, err error) {
		slog.Error(msg, "err", err)
		tracker.Flush(2 * time.Second)
		os.Exit(1)
	}
	if cfg.LDAP.Host == "mock" {
		slog.Warn("LDAP_HOST=mock: qualsiasi credenziale accede a /admin. Solo per sviluppo, MAI in produzione.")
	}
	for _, w := range cfg.Warnings() {
		slog.Warn(w)
	}

	db, err := database.Open(cfg.DBPath)
	if err != nil {
		fatal("inizializzazione database", err)
	}
	defer db.Close()

	if err := os.MkdirAll(cfg.UploadDir, 0o750); err != nil {
		fatal("creazione UPLOAD_DIR", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bk, err := backup.New(backup.Options{
		Store:      db,
		DBPath:     cfg.DBPath,
		UploadDir:  cfg.UploadDir,
		AppVersion: AppVersion,
		MaxSchema:  database.CurrentSchemaVersion(),
		Location:   cfg.Location,
	})
	if err != nil {
		fatal("inizializzazione backup", err)
	}
	go bk.Scheduler(ctx, time.Duration(cfg.BackupIntervalHours)*time.Hour)
	go bk.Janitor(ctx)

	// Riconoscimento utente in plancia: serve una directory per cercare il nome.
	var directory identity.Directory
	switch {
	case cfg.LDAP.Host == "mock":
		directory = identity.MockDirectory{}
	case cfg.LDAP.BindDN != "":
		directory = identity.NewLDAPDirectory(cfg.LDAP)
	}
	if cfg.NTLMDomain != "" && directory == nil {
		slog.Warn("NTLM_DOMAIN impostato ma LDAP_BIND_DN vuoto: riconoscimento utente disattivato")
	}

	var tickets otrs.Client
	if cfg.OTRS.Enabled() {
		tickets = otrs.New(cfg.OTRS, cfg.Location)
		if directory == nil || cfg.NTLMDomain == "" {
			slog.Warn("OTRS_URL impostato ma riconoscimento utente spento: nessuno potrà aprire ticket dalla plancia")
		}
	}

	srv, err := web.New(web.Options{
		DB:        db,
		Config:    cfg,
		Auth:      auth.NewLDAP(cfg.LDAP),
		Directory: directory,
		Backup:    bk,
		Version:   AppVersion,
		Tickets:   tickets,
		Errors:    tracker,
	})
	if err != nil {
		fatal("inizializzazione web", err)
	}
	srv.StartNotifications(ctx)
	srv.StartGuideRefresh(ctx)
	srv.StartPresenceCleanup(ctx)
	srv.StartTicketWatch(ctx)
	srv.StartTicketStats(ctx)

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("CruscottoPA avviato", "version", AppVersion, "port", cfg.Port, "db", cfg.DBPath, "ldap", cfg.LDAP.Host)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server HTTP", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Close() // chiude i flussi SSE, altrimenti Shutdown li aspetterebbe
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "err", err)
	}
	tracker.Flush(2 * time.Second)
}

func runHealthcheck(port string) int {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%s/health", port))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "status", resp.StatusCode)
		return 1
	}
	return 0
}
