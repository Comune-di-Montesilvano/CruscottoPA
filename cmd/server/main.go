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
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
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

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)
	if cfg.LDAP.Host == "mock" {
		slog.Warn("LDAP_HOST=mock: qualsiasi credenziale accede a /admin. Solo per sviluppo, MAI in produzione.")
	}

	db, err := database.Open(cfg.DBPath)
	if err != nil {
		slog.Error("inizializzazione database", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := os.MkdirAll(cfg.UploadDir, 0o750); err != nil {
		slog.Error("creazione UPLOAD_DIR", "err", err)
		os.Exit(1)
	}

	srv, err := web.New(web.Options{
		DB:      db,
		Config:  cfg,
		Auth:    auth.NewLDAP(cfg.LDAP),
		Version: AppVersion,
	})
	if err != nil {
		slog.Error("inizializzazione web", "err", err)
		os.Exit(1)
	}

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "err", err)
	}
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
