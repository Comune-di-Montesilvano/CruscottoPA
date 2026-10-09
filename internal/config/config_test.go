package config

import (
	"log/slog"
	"strings"
	"testing"
)

var allVars = []string{
	"PORT", "DB_PATH", "UPLOAD_DIR", "SESSION_SECRET", "SECURE_COOKIES", "LOG_LEVEL", "TZ",
	"LDAP_HOST", "LDAP_BASE_DN", "LDAP_USER_DN_TEMPLATE", "LDAP_STARTTLS", "LDAP_TLS_SKIP_VERIFY",
	"LDAP_BIND_DN", "LDAP_BIND_PASSWORD", "LDAP_REQUIRED_GROUP", "LDAP_ADMIN_GROUP", "ADMIN_USERS",
	"BACKUP_INTERVAL_HOURS", "NTLM_DOMAIN", "VAPID_SUBJECT", "GUIDE_REFRESH_HOURS",
	"OTRS_URL", "OTRS_ROUTE_CREATE", "OTRS_ROUTE_UPDATE", "OTRS_ROUTE_SEARCH", "OTRS_ROUTE_GET", "OTRS_USER", "OTRS_PASSWORD", "OTRS_QUEUE", "OTRS_FALLBACK_EMAIL",
}

func TestLoadOTRSDisabledByDefault(t *testing.T) {
	clearEnv(t)
	t.Setenv("LDAP_HOST", "mock")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OTRS.Enabled() || cfg.OTRS.RouteCreate != "/TicketCreate" || cfg.OTRS.RouteUpdate != "/TicketUpdate" {
		t.Fatalf("default OTRS: %+v", cfg.OTRS)
	}
	if cfg.OTRS.RouteSearch != "/TicketSearch" || cfg.OTRS.RouteGet != "/Ticket/:TicketID" {
		t.Fatalf("route di lettura: %+v", cfg.OTRS)
	}
}

func TestLoadOTRSValidation(t *testing.T) {
	base := func(t *testing.T) {
		clearEnv(t)
		t.Setenv("LDAP_HOST", "ldaps://dc.example.local:636")
		t.Setenv("SESSION_SECRET", strings.Repeat("a", 32))
		t.Setenv("OTRS_URL", "https://otrs.example.it/otrs/nph-genericinterface.pl/Webservice/Prova")
		t.Setenv("OTRS_USER", "agente")
		t.Setenv("OTRS_PASSWORD", "segreta")
		t.Setenv("OTRS_QUEUE", "Coda di prova")
	}
	base(t)
	cfg, err := Load()
	if err != nil || !cfg.OTRS.Enabled() || cfg.OTRS.Mock() {
		t.Fatalf("config valida: %v %+v", err, cfg.OTRS)
	}
	for _, tc := range []struct{ key, val, want string }{
		{"OTRS_URL", "http://otrs.example.it/x", "https"},
		{"OTRS_USER", "", "OTRS_USER"},
		{"OTRS_PASSWORD", "", "OTRS_PASSWORD"},
		{"OTRS_QUEUE", "", "OTRS_QUEUE"},
		{"OTRS_ROUTE_CREATE", "TicketCreate", "OTRS_ROUTE_CREATE"},
		{"OTRS_ROUTE_UPDATE", "x", "OTRS_ROUTE_UPDATE"},
		{"OTRS_ROUTE_SEARCH", "TicketSearch", "OTRS_ROUTE_SEARCH"},
		{"OTRS_ROUTE_GET", "Ticket/:TicketID", "OTRS_ROUTE_GET"},
		{"OTRS_URL", "mock", "mock"}, // mock solo con LDAP_HOST=mock
	} {
		base(t)
		t.Setenv(tc.key, tc.val)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s=%q: atteso errore con %q, ottenuto %v", tc.key, tc.val, tc.want, err)
		}
	}
}

func TestLoadOTRSMock(t *testing.T) {
	clearEnv(t)
	t.Setenv("LDAP_HOST", "mock")
	t.Setenv("OTRS_URL", "mock")
	cfg, err := Load()
	if err != nil || !cfg.OTRS.Mock() || !cfg.OTRS.Enabled() {
		t.Fatalf("mock: %v %+v", err, cfg.OTRS)
	}
}

func TestLoadOTRSURLTrailingSlash(t *testing.T) {
	clearEnv(t)
	t.Setenv("LDAP_HOST", "mock")
	t.Setenv("OTRS_URL", "https://otrs.example.it/ws/Prova/")
	t.Setenv("OTRS_USER", "a")
	t.Setenv("OTRS_PASSWORD", "b")
	t.Setenv("OTRS_QUEUE", "c")
	cfg, err := Load()
	if err != nil || cfg.OTRS.URL != "https://otrs.example.it/ws/Prova" {
		t.Fatalf("slash finale non tolta: %v %q", err, cfg.OTRS.URL)
	}
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, v := range allVars {
		t.Setenv(v, "")
	}
}

func TestLoadRequiresLDAPHost(t *testing.T) {
	clearEnv(t)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "LDAP_HOST") {
		t.Fatalf("senza LDAP_HOST l'avvio deve fallire (mock solo esplicito), ottenuto %v", err)
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("LDAP_HOST", "mock")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != "8080" || cfg.DBPath != "cruscotto.db" || cfg.UploadDir != "uploads" {
		t.Fatalf("default inattesi: %+v", cfg)
	}
	if cfg.LDAP.Host != "mock" || !cfg.LDAP.StartTLS || !cfg.SecureCookies {
		t.Fatalf("default LDAP/cookie inattesi: %+v", cfg)
	}
	if cfg.Location.String() != "Europe/Rome" || cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("default TZ/log inattesi: %v %v", cfg.Location, cfg.LogLevel)
	}
	if len(cfg.SessionSecret) != 64 {
		t.Fatalf("in mock senza SESSION_SECRET serve un segreto casuale, ottenuto %q", cfg.SessionSecret)
	}
}

func TestLoadRealLDAPRequiresSecret(t *testing.T) {
	clearEnv(t)
	t.Setenv("LDAP_HOST", "ldaps://dc.example.local:636")
	t.Setenv("SESSION_SECRET", "troppo-corto")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Fatalf("atteso errore su SESSION_SECRET, ottenuto %v", err)
	}
	t.Setenv("SESSION_SECRET", strings.Repeat("a", 32))
	if _, err := Load(); err != nil {
		t.Fatalf("con segreto valido: %v", err)
	}
}

func TestLoadAdminUsers(t *testing.T) {
	clearEnv(t)
	t.Setenv("LDAP_HOST", "mock")
	t.Setenv("ADMIN_USERS", " mrossi ; gbianchi,, lverdi ")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mrossi", "gbianchi", "lverdi"}
	if strings.Join(cfg.LDAP.AdminUsers, "|") != strings.Join(want, "|") {
		t.Fatalf("ADMIN_USERS: atteso %v, ottenuto %v", want, cfg.LDAP.AdminUsers)
	}
}

func TestLoadInvalidValues(t *testing.T) {
	for _, tc := range []struct{ key, val string }{
		{"SECURE_COOKIES", "forse"},
		{"LDAP_STARTTLS", "boh"},
		{"TZ", "Marte/Olympus"},
		{"LOG_LEVEL", "verbose"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("LDAP_HOST", "mock")
			t.Setenv(tc.key, tc.val)
			if _, err := Load(); err == nil {
				t.Fatalf("%s=%q: atteso errore", tc.key, tc.val)
			}
		})
	}
}

func TestLoadBackupInterval(t *testing.T) {
	clearEnv(t)
	t.Setenv("LDAP_HOST", "mock")
	cfg, err := Load()
	if err != nil || cfg.BackupIntervalHours != 24 {
		t.Fatalf("default: atteso 24, ottenuto %d (%v)", cfg.BackupIntervalHours, err)
	}
	t.Setenv("BACKUP_INTERVAL_HOURS", "0")
	if cfg, err := Load(); err != nil || cfg.BackupIntervalHours != 0 {
		t.Fatalf("0 deve disattivare: %d %v", cfg.BackupIntervalHours, err)
	}
	for _, bad := range []string{"-1", "abc", "1.5"} {
		t.Setenv("BACKUP_INTERVAL_HOURS", bad)
		if _, err := Load(); err == nil {
			t.Errorf("BACKUP_INTERVAL_HOURS=%q: atteso errore", bad)
		}
	}
}

func TestNTLMDomain(t *testing.T) {
	t.Setenv("LDAP_HOST", "mock")
	t.Setenv("NTLM_DOMAIN", " COMUNE-MS ")
	cfg, err := Load()
	if err != nil || cfg.NTLMDomain != "COMUNE-MS" {
		t.Fatalf("NTLMDomain = %q (%v)", cfg.NTLMDomain, err)
	}
}

func TestWarnings(t *testing.T) {
	t.Setenv("LDAP_HOST", "ldap://dc.local")
	t.Setenv("SESSION_SECRET", strings.Repeat("s", 32))
	t.Setenv("LDAP_BIND_DN", "svc@local")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	w := cfg.Warnings()
	if len(w) == 0 || !strings.Contains(strings.Join(w, " "), "LDAP_BASE_DN") {
		t.Fatalf("atteso un avviso su LDAP_BASE_DN vuoto, ottenuto %v", w)
	}
	t.Setenv("LDAP_BASE_DN", "dc=local")
	cfg, _ = Load()
	for _, s := range cfg.Warnings() {
		if strings.Contains(s, "LDAP_BASE_DN") {
			t.Fatalf("avviso inatteso: %v", s)
		}
	}
}

func TestVAPIDSubject(t *testing.T) {
	clearEnv(t)
	t.Setenv("LDAP_HOST", "mock")
	t.Setenv("VAPID_SUBJECT", " mailto:supporto@example.it ")
	cfg, err := Load()
	if err != nil || cfg.VAPIDSubject != "mailto:supporto@example.it" || len(cfg.Warnings()) != 0 {
		t.Fatalf("VAPIDSubject = %q, warnings %v (%v)", cfg.VAPIDSubject, cfg.Warnings(), err)
	}
	t.Setenv("VAPID_SUBJECT", "supporto@example.it")
	cfg, _ = Load()
	if w := strings.Join(cfg.Warnings(), " "); !strings.Contains(w, "VAPID_SUBJECT") {
		t.Fatalf("atteso avviso su VAPID_SUBJECT senza mailto:/https:, ottenuto %q", w)
	}
}

func TestLoadGuideRefresh(t *testing.T) {
	clearEnv(t)
	t.Setenv("LDAP_HOST", "mock")
	if cfg, err := Load(); err != nil || cfg.GuideRefreshHours != 6 {
		t.Fatalf("default: atteso 6, ottenuto %d (%v)", cfg.GuideRefreshHours, err)
	}
	t.Setenv("GUIDE_REFRESH_HOURS", "0")
	if cfg, err := Load(); err != nil || cfg.GuideRefreshHours != 0 {
		t.Fatalf("0 = solo manuale: %d %v", cfg.GuideRefreshHours, err)
	}
	t.Setenv("GUIDE_REFRESH_HOURS", "-1")
	if _, err := Load(); err == nil {
		t.Error("GUIDE_REFRESH_HOURS=-1: atteso errore")
	}
}

// TicketGet su una route senza parametro (es. /TicketGet): il TicketID va nel corpo.
func TestLoadOTRSRouteGetWithoutParam(t *testing.T) {
	clearEnv(t)
	t.Setenv("LDAP_HOST", "mock")
	t.Setenv("OTRS_URL", "https://otrs.example.it/ws/Prova")
	t.Setenv("OTRS_USER", "a")
	t.Setenv("OTRS_PASSWORD", "b")
	t.Setenv("OTRS_QUEUE", "c")
	t.Setenv("OTRS_ROUTE_GET", "/TicketGet")
	if cfg, err := Load(); err != nil || cfg.OTRS.RouteGet != "/TicketGet" {
		t.Fatalf("route senza parametro: %v %+v", err, cfg.OTRS)
	}
}
