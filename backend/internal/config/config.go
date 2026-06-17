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

	// KTSigningKey is the base64-encoded Ed25519 private key used to sign STHs.
	KTSigningKey string

	// ComplianceDeviceID, if set, designates a device whose KeyPackages back
	// the compliance KeyPackage endpoint. Optional; empty disables it.
	ComplianceDeviceID string

	// CookieSecure gates the Secure attribute on the session cookie.
	// Dev over http = false; prod = true.
	CookieSecure bool
}

func Load() (*Config, error) {
	c := &Config{
		HTTPAddr:               getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:            os.Getenv("DATABASE_URL"),
		RedisURL:               os.Getenv("REDIS_URL"),
		OpaqueServerPrivateKey: os.Getenv("OPAQUE_SERVER_PRIVATE_KEY"),
		OpaqueServerPublicKey:  os.Getenv("OPAQUE_SERVER_PUBLIC_KEY"),
		OpaqueOPRFSeed:         os.Getenv("OPAQUE_OPRF_SEED"),
		KTSigningKey:           os.Getenv("KT_SIGNING_KEY"),
		ComplianceDeviceID:     os.Getenv("COMPLIANCE_DEVICE_ID"),
		CookieSecure:           getenvBool("COOKIE_SECURE", true),
	}
	for k, v := range map[string]string{
		"DATABASE_URL":              c.DatabaseURL,
		"REDIS_URL":                 c.RedisURL,
		"OPAQUE_SERVER_PRIVATE_KEY": c.OpaqueServerPrivateKey,
		"OPAQUE_SERVER_PUBLIC_KEY":  c.OpaqueServerPublicKey,
		"OPAQUE_OPRF_SEED":          c.OpaqueOPRFSeed,
		"KT_SIGNING_KEY":            c.KTSigningKey,
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

func getenvBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v != "false" && v != "0"
}
