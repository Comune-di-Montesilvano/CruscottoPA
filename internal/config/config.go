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

// OTRS: invio dei ticket dalla plancia (GenericInterface REST). URL vuota = modulo spento.
type OTRS struct {
	URL           string // base del web service, senza "/" finale; "mock" = client finto (solo sviluppo)
	RouteCreate   string // route POST di TicketCreate
	RouteUpdate   string // route PATCH di TicketUpdate
	RouteSearch   string // route POST di TicketSearch
	RouteGet      string // route GET di TicketGet: con :TicketID nel percorso, oppure senza (ID nel corpo)
	User          string
	Password      string
	Queue         string
	FallbackEmail string // casella mostrata se OTRS non risponde
}

func (o OTRS) Enabled() bool { return o.URL != "" }
func (o OTRS) Mock() bool    { return o.URL == "mock" }

func (o OTRS) validate(ldapMock bool) error {
	if !o.Enabled() {
		return nil
	}
	if o.Mock() {
		if !ldapMock {
			return errors.New("OTRS_URL=mock ammesso solo con LDAP_HOST=mock")
		}
		return nil
	}
	if !strings.HasPrefix(o.URL, "https://") {
		return errors.New("OTRS_URL deve iniziare con https://")
	}
	for _, r := range []struct{ name, val string }{{"OTRS_ROUTE_CREATE", o.RouteCreate}, {"OTRS_ROUTE_UPDATE", o.RouteUpdate},
		{"OTRS_ROUTE_SEARCH", o.RouteSearch}, {"OTRS_ROUTE_GET", o.RouteGet}} {
		if !strings.HasPrefix(r.val, "/") {
			return fmt.Errorf("%s deve iniziare con /", r.name)
		}
	}
	for _, r := range []struct{ name, val string }{{"OTRS_USER", o.User}, {"OTRS_PASSWORD", o.Password}, {"OTRS_QUEUE", o.Queue}} {
		if r.val == "" {
			return fmt.Errorf("%s obbligatorio quando OTRS_URL è impostato", r.name)
		}
	}
	return nil
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
	// GuideRefreshHours: ogni quante ore riscaricare le guide da GitHub (0 = solo a mano).
	GuideRefreshHours int
	LDAP              LDAP
	// NTLMDomain: dominio NetBIOS accettato da /io (vuoto = riconoscimento spento).
	NTLMDomain string
	// VAPIDSubject: contatto VAPID (mailto: o https:); vuoto = Web Push spento.
	VAPIDSubject string
	// OTRS: modulo ticket (vuoto = spento).
	OTRS OTRS
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
		NTLMDomain:   strings.TrimSpace(os.Getenv("NTLM_DOMAIN")),
		VAPIDSubject: strings.TrimSpace(os.Getenv("VAPID_SUBJECT")),
		OTRS: OTRS{
			URL:           strings.TrimRight(strings.TrimSpace(os.Getenv("OTRS_URL")), "/"),
			RouteCreate:   getEnv("OTRS_ROUTE_CREATE", "/TicketCreate"),
			RouteUpdate:   getEnv("OTRS_ROUTE_UPDATE", "/TicketUpdate"),
			RouteSearch:   getEnv("OTRS_ROUTE_SEARCH", "/TicketSearch"),
			RouteGet:      getEnv("OTRS_ROUTE_GET", "/Ticket/:TicketID"),
			User:          os.Getenv("OTRS_USER"),
			Password:      os.Getenv("OTRS_PASSWORD"),
			Queue:         strings.TrimSpace(os.Getenv("OTRS_QUEUE")),
			FallbackEmail: strings.TrimSpace(os.Getenv("OTRS_FALLBACK_EMAIL")),
		},
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
	if cfg.GuideRefreshHours, err = getEnvInt("GUIDE_REFRESH_HOURS", 6); err != nil {
		return Config{}, err
	}

	// Nessun default: "mock" rende admin chiunque, deve essere una scelta esplicita.
	if cfg.LDAP.Host == "" {
		return Config{}, errors.New("LDAP_HOST obbligatorio (ldap://… o ldaps://…; \"mock\" solo per sviluppo)")
	}
	if cfg.LDAP.Host != "mock" && len(cfg.SessionSecret) < 32 {
		return Config{}, errors.New("SESSION_SECRET obbligatorio (almeno 32 caratteri) quando LDAP_HOST non è mock")
	}
	if err := cfg.OTRS.validate(cfg.LDAP.Host == "mock"); err != nil {
		return Config{}, err
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

// Warnings: combinazioni ammesse ma probabilmente sbagliate, da segnalare
// all'avvio (il server parte comunque).
func (c Config) Warnings() []string {
	var w []string
	if c.LDAP.Host != "mock" && c.LDAP.BindDN != "" && strings.TrimSpace(c.LDAP.BaseDN) == "" {
		w = append(w, "LDAP_BIND_DN impostato ma LDAP_BASE_DN vuoto: le ricerche in AD (nome, gruppi, suggerimenti) falliranno")
	}
	if s := c.VAPIDSubject; s != "" && !strings.HasPrefix(s, "mailto:") && !strings.HasPrefix(s, "https:") {
		w = append(w, "VAPID_SUBJECT deve iniziare con mailto: o https:")
	}
	return w
}
