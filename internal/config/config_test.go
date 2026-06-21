package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadServerConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	data := []byte(`{
		"address": "localhost:9090",
		"restore": false,
		"store_interval": "1s",
		"store_file": "/tmp/metrics.json",
		"database_dsn": "postgres://user:pass@localhost:5432/metrics",
		"key": "secret",
		"crypto_key": "/tmp/private.pem",
		"trusted_subnet": "192.168.0.0/24",
		"audit_file": "/tmp/audit.log",
		"audit_url": "http://localhost:9091/audit"
	}`)

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("failed to load server config: %v", err)
	}

	if cfg.Address == nil || *cfg.Address != "localhost:9090" {
		t.Fatalf("unexpected address: %v", cfg.Address)
	}
	if cfg.Restore == nil || *cfg.Restore {
		t.Fatalf("unexpected restore: %v", cfg.Restore)
	}
	if cfg.StoreInterval == nil || cfg.StoreInterval.Int() != 1 {
		t.Fatalf("unexpected store interval: %v", cfg.StoreInterval)
	}
	if cfg.StoreFileValue() == nil || *cfg.StoreFileValue() != "/tmp/metrics.json" {
		t.Fatalf("unexpected store file: %v", cfg.StoreFileValue())
	}
	if !cfg.HasFileStorageSettings() {
		t.Fatal("expected file storage settings to be detected")
	}
	if cfg.DatabaseDSN == nil || *cfg.DatabaseDSN == "" {
		t.Fatalf("unexpected database dsn: %v", cfg.DatabaseDSN)
	}
	if cfg.Key == nil || *cfg.Key != "secret" {
		t.Fatalf("unexpected key: %v", cfg.Key)
	}
	if cfg.CryptoKey == nil || *cfg.CryptoKey != "/tmp/private.pem" {
		t.Fatalf("unexpected crypto key: %v", cfg.CryptoKey)
	}
	if cfg.TrustedSubnet == nil || *cfg.TrustedSubnet != "192.168.0.0/24" {
		t.Fatalf("unexpected trusted subnet: %v", cfg.TrustedSubnet)
	}
	if cfg.AuditFile == nil || *cfg.AuditFile != "/tmp/audit.log" {
		t.Fatalf("unexpected audit file: %v", cfg.AuditFile)
	}
	if cfg.AuditURL == nil || *cfg.AuditURL != "http://localhost:9091/audit" {
		t.Fatalf("unexpected audit url: %v", cfg.AuditURL)
	}
}

func TestLoadAgentConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	data := []byte(`{
		"address": "localhost:9090",
		"report_interval": "3s",
		"poll_interval": 2,
		"rate_limit": 4,
		"key": "secret",
		"crypto_key": "/tmp/public.pem"
	}`)

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("failed to load agent config: %v", err)
	}

	if cfg.Address == nil || *cfg.Address != "localhost:9090" {
		t.Fatalf("unexpected address: %v", cfg.Address)
	}
	if cfg.ReportInterval == nil || cfg.ReportInterval.Int() != 3 {
		t.Fatalf("unexpected report interval: %v", cfg.ReportInterval)
	}
	if cfg.PollInterval == nil || cfg.PollInterval.Int() != 2 {
		t.Fatalf("unexpected poll interval: %v", cfg.PollInterval)
	}
	if cfg.RateLimit == nil || *cfg.RateLimit != 4 {
		t.Fatalf("unexpected rate limit: %v", cfg.RateLimit)
	}
	if cfg.Key == nil || *cfg.Key != "secret" {
		t.Fatalf("unexpected key: %v", cfg.Key)
	}
	if cfg.CryptoKey == nil || *cfg.CryptoKey != "/tmp/public.pem" {
		t.Fatalf("unexpected crypto key: %v", cfg.CryptoKey)
	}
}

func TestResolvePrecedence(t *testing.T) {
	configValue := "from-config"

	if got := ResolveString([]string{"CONFIG_TEST_VALUE"}, false, "", &configValue, "default"); got != "from-config" {
		t.Fatalf("expected config value, got %q", got)
	}

	if got := ResolveString([]string{"CONFIG_TEST_VALUE"}, true, "from-flag", &configValue, "default"); got != "from-flag" {
		t.Fatalf("expected flag value, got %q", got)
	}

	t.Setenv("CONFIG_TEST_VALUE", "from-env")
	if got := ResolveString([]string{"CONFIG_TEST_VALUE"}, true, "from-flag", &configValue, "default"); got != "from-env" {
		t.Fatalf("expected env value, got %q", got)
	}
}

func TestResolveBoolUsesConfigFalse(t *testing.T) {
	configValue := false

	got, err := ResolveBool([]string{"CONFIG_TEST_BOOL"}, false, true, &configValue, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Fatal("expected config false value")
	}
}

func TestResolveSecondsAcceptsEnvDuration(t *testing.T) {
	configValue := DurationSeconds(2)
	t.Setenv("CONFIG_TEST_SECONDS", "5s")

	got, err := ResolveSeconds([]string{"CONFIG_TEST_SECONDS"}, true, 3, &configValue, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 5 {
		t.Fatalf("expected env duration to win, got %d", got)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(path, []byte(`{"unknown": "value"}`), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	if _, err := LoadAgent(path); err == nil {
		t.Fatal("expected unknown field error")
	}
}
