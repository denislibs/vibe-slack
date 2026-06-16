package config

import (
	"fmt"
	"os"
)

// Config holds all runtime configuration, loaded from the environment.
type Config struct {
	HTTPAddr    string
	DatabaseURL string
	RedisURL    string

	// OPAQUE long-term server key material (base64), shared across all instances.
	OpaqueServerPrivateKey string
	OpaqueServerPublicKey  string
	OpaqueOPRFSeed         string
}

func Load() (*Config, error) {
	c := &Config{
		HTTPAddr:               getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:            os.Getenv("DATABASE_URL"),
		RedisURL:               os.Getenv("REDIS_URL"),
		OpaqueServerPrivateKey: os.Getenv("OPAQUE_SERVER_PRIVATE_KEY"),
		OpaqueServerPublicKey:  os.Getenv("OPAQUE_SERVER_PUBLIC_KEY"),
		OpaqueOPRFSeed:         os.Getenv("OPAQUE_OPRF_SEED"),
	}
	for k, v := range map[string]string{
		"DATABASE_URL":              c.DatabaseURL,
		"REDIS_URL":                 c.RedisURL,
		"OPAQUE_SERVER_PRIVATE_KEY": c.OpaqueServerPrivateKey,
		"OPAQUE_SERVER_PUBLIC_KEY":  c.OpaqueServerPublicKey,
		"OPAQUE_OPRF_SEED":          c.OpaqueOPRFSeed,
	} {
		if v == "" {
			return nil, fmt.Errorf("required env var %s is empty", k)
		}
	}
	return c, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
