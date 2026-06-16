package main

import (
	"context"
	"encoding/base64"
	"log"
	"net/http"
	"time"

	"github.com/messenger/backend/internal/as"
	"github.com/messenger/backend/internal/config"
	"github.com/messenger/backend/internal/devices"
	"github.com/messenger/backend/internal/httpapi"
	"github.com/messenger/backend/internal/keypackages"
	"github.com/messenger/backend/internal/opaque"
	"github.com/messenger/backend/internal/platform/postgres"
	"github.com/messenger/backend/internal/platform/redis"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx := context.Background()

	pool, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()
	if err := postgres.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	rdb, err := redis.NewClient(ctx, cfg.RedisURL)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}

	dec := base64.StdEncoding.DecodeString
	priv, _ := dec(cfg.OpaqueServerPrivateKey)
	pub, _ := dec(cfg.OpaqueServerPublicKey)
	seed, _ := dec(cfg.OpaqueOPRFSeed)
	osrv, err := opaque.NewServer(priv, pub, seed, []byte("messenger-as"))
	if err != nil {
		log.Fatalf("opaque: %v", err)
	}

	sess := session.NewManager(rdb, 24*time.Hour)
	rl := session.NewRateLimiter(rdb, 10, time.Minute)
	svc := as.NewService(osrv, store.NewUserRepo(pool), sess, rdb)
	devSvc := devices.NewService(store.NewDeviceRepo(pool))
	kpSvc := keypackages.NewService(store.NewKeyPackageRepo(pool))

	handler := httpapi.NewRouterFull(svc, sess, devSvc, kpSvc, rl)

	log.Printf("auth-service listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, handler); err != nil {
		log.Fatalf("server: %v", err)
	}
}
