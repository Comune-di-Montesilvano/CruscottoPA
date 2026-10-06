// Package config legge la configurazione del server dalle variabili d'ambiente.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// LDAP raccoglie i parametri di connessione ad Active Directory.
type LDAP struct {
	Host           string // obbligatorio: ldap://host:389, ldaps://host:636 o "mock" (solo sviluppo)
	BaseDN         string
	UserDNTemplate string // %s = username, es. "%s@comune.local"
	StartTLS       bool
	TLSSkipVerify  bool
	BindDN         string
	BindPassword   string
	RequiredGroup  string
	AdminGroup     string
	AdminUsers     []string
}

// Config è la configurazione completa del server.
type Config struct {
	Port                string
	DBPath              string
	UploadDir           string
	SessionSecret       string
	SecureCookies       bool
	LogLevel            slog.Level
	Location            *time.Location
	BackupIntervalHours int
	LDAP                LDAP
	// NTLMDomain: dominio NetBIOS accettato da /io (vuoto = riconoscimento spento).
	NTLMDomain string
}

// Load legge le variabili d'ambiente, applica i default e valida i valori.
func Load() (Config, error) {
	cfg := Config{
		Port:          getEnv("PORT", "8080"),
		DBPath:        getEnv("DB_PATH", "cruscotto.db"),
		UploadDir:     getEnv("UPLOAD_DIR", "uploads"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
		LDAP: LDAP{
			Host:           os.Getenv("LDAP_HOST"),
			BaseDN:         os.Getenv("LDAP_BASE_DN"),
			UserDNTemplate: getEnv("LDAP_USER_DN_TEMPLATE", "%s"),
			BindDN:         os.Getenv("LDAP_BIND_DN"),
			BindPassword:   os.Getenv("LDAP_BIND_PASSWORD"),
			RequiredGroup:  os.Getenv("LDAP_REQUIRED_GROUP"),
			AdminGroup:     os.Getenv("LDAP_ADMIN_GROUP"),
			AdminUsers:     splitList(os.Getenv("ADMIN_USERS")),
		},
		NTLMDomain: strings.TrimSpace(os.Getenv("NTLM_DOMAIN")),
	}

	var err error
	if cfg.SecureCookies, err = getEnvBool("SECURE_COOKIES", true); err != nil {
		return Config{}, err
	}
	if cfg.LDAP.StartTLS, err = getEnvBool("LDAP_STARTTLS", true); err != nil {
		return Config{}, err
	}
	if cfg.LDAP.TLSSkipVerify, err = getEnvBool("LDAP_TLS_SKIP_VERIFY", false); err != nil {
		return Config{}, err
	}
	if cfg.Location, err = time.LoadLocation(getEnv("TZ", "Europe/Rome")); err != nil {
		return Config{}, fmt.Errorf("TZ: %w", err)
	}
	if err = cfg.LogLevel.UnmarshalText([]byte(getEnv("LOG_LEVEL", "info"))); err != nil {
		return Config{}, fmt.Errorf("LOG_LEVEL: %w", err)
	}
	if cfg.BackupIntervalHours, err = getEnvInt("BACKUP_INTERVAL_HOURS", 24); err != nil {
		return Config{}, err
	}

	// Nessun default: "mock" rende admin chiunque, deve essere una scelta esplicita.
	if cfg.LDAP.Host == "" {
		return Config{}, errors.New("LDAP_HOST obbligatorio (ldap://… o ldaps://…; \"mock\" solo per sviluppo)")
	}
	if cfg.LDAP.Host != "mock" && len(cfg.SessionSecret) < 32 {
		return Config{}, errors.New("SESSION_SECRET obbligatorio (almeno 32 caratteri) quando LDAP_HOST non è mock")
	}
	if cfg.SessionSecret == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return Config{}, fmt.Errorf("generazione SESSION_SECRET: %w", err)
		}
		cfg.SessionSecret = hex.EncodeToString(b)
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: valore booleano non valido %q", key, v)
	}
	return b, nil
}

// splitList divide una lista separata da ';' o ',' scartando spazi e voci vuote.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getEnvInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s: atteso un intero >= 0, ottenuto %q", key, v)
	}
	return n, nil
}
