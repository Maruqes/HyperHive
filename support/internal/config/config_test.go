package config

import (
	"strings"
	"testing"
)

func TestLoadDefaultsAndValidation(t *testing.T) {
	t.Setenv("SUPPORT_LISTEN", "")
	t.Setenv("SUPPORT_DB_PATH", "")
	t.Setenv("SUPPORT_PROXY_SECRET", strings.Repeat("s", 32))
	cfg, err := Load()
	if err != nil || cfg.Listen != "127.0.0.1:8091" || cfg.DatabasePath != "./support.db" {
		t.Fatalf("defaults: %+v %v", cfg, err)
	}
	t.Setenv("SUPPORT_LISTEN", "missing-port")
	if _, err := Load(); err == nil {
		t.Fatal("invalid listener accepted")
	}
	t.Setenv("SUPPORT_LISTEN", "127.0.0.1:8091")
	t.Setenv("SUPPORT_PROXY_SECRET", "short")
	if _, err := Load(); err == nil {
		t.Fatal("short secret accepted")
	}
}
