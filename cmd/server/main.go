// Command server avvia il portale Intranet CruscottoPA.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// AppVersion è iniettata in fase di build:
//
//	go build -ldflags "-X main.AppVersion=1.2.3" ./cmd/server
//
// In CI (release.yml) coincide con il tag git; le build locali senza
// build-arg restano "dev" (o "dev-DEV" via docker-compose.override.yml).
var AppVersion = "dev"

type config struct {
	Port   string
	DBPath string
}

func loadConfig() config {
	return config{
		Port:   getEnv("PORT", "8080"),
		DBPath: getEnv("DB_PATH", "cruscotto.db"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "esegue GET /health su localhost ed esce (usato dal HEALTHCHECK del container)")
	flag.Parse()

	cfg := loadConfig()

	// Il binario fa da healthcheck di sé stesso: l'immagine non dipende da wget/curl.
	if *healthcheck {
		os.Exit(runHealthcheck(cfg.Port))
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	db, err := database.InitDB(cfg.DBPath)
	if err != nil {
		slog.Error("inizializzazione database", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	tmpl := template.Must(template.ParseGlob("web/templates/*.html"))

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	mux.HandleFunc("GET /health", handleHealth(db))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		data := map[string]any{"Version": AppVersion}
		if err := tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
			slog.Error("render index", "err", err)
		}
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("CruscottoPA avviato", "version", AppVersion, "port", cfg.Port, "db", cfg.DBPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server HTTP", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "err", err)
	}
}

func handleHealth(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, code := "ok", http.StatusOK
		if err := db.PingContext(r.Context()); err != nil {
			status, code = "db unavailable", http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]string{"status": status, "version": AppVersion})
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
