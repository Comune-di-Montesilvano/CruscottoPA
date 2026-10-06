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
	"BACKUP_INTERVAL_HOURS",
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
