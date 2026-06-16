package config

import "testing"

func TestLoadFromEnvDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/as?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("OPAQUE_SERVER_PRIVATE_KEY", "AAAA")
	t.Setenv("OPAQUE_SERVER_PUBLIC_KEY", "BBBB")
	t.Setenv("OPAQUE_OPRF_SEED", "CCCC")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseURL == "" || cfg.RedisURL == "" {
		t.Fatal("expected DB and Redis URLs to be populated")
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("expected default HTTPAddr :8080, got %q", cfg.HTTPAddr)
	}
}

func TestLoadMissingRequiredFails(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when DATABASE_URL is missing")
	}
}
