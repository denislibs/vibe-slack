package main

import (
	"context"
	"encoding/base64"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/messenger/backend/internal/as"
	"github.com/messenger/backend/internal/config"
	"github.com/messenger/backend/internal/delivery"
	"github.com/messenger/backend/internal/devices"
	"github.com/messenger/backend/internal/fanout"
	"github.com/messenger/backend/internal/httpapi"
	"github.com/messenger/backend/internal/hub"
	"github.com/messenger/backend/internal/keypackages"
	"github.com/messenger/backend/internal/opaque"
	"github.com/messenger/backend/internal/platform/postgres"
	"github.com/messenger/backend/internal/platform/redis"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
	"github.com/messenger/backend/internal/ws"
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
	priv, err := dec(cfg.OpaqueServerPrivateKey)
	if err != nil {
		log.Fatalf("decode OPAQUE_SERVER_PRIVATE_KEY: %v", err)
	}
	pub, err := dec(cfg.OpaqueServerPublicKey)
	if err != nil {
		log.Fatalf("decode OPAQUE_SERVER_PUBLIC_KEY: %v", err)
	}
	seed, err := dec(cfg.OpaqueOPRFSeed)
	if err != nil {
		log.Fatalf("decode OPAQUE_OPRF_SEED: %v", err)
	}
	osrv, err := opaque.NewServer(priv, pub, seed, []byte("messenger-as"))
	if err != nil {
		log.Fatalf("opaque: %v", err)
	}

	sess := session.NewManager(rdb, 24*time.Hour)
	rl := session.NewRateLimiter(rdb, 10, time.Minute)
	svc := as.NewService(osrv, store.NewUserRepo(pool), sess, rdb)
	devSvc := devices.NewService(store.NewDeviceRepo(pool))
	kpSvc := keypackages.NewService(store.NewKeyPackageRepo(pool))
	rosterRepo := store.NewRosterRepo(pool)

	// Delivery service + websocket gateway.
	hubReg := hub.New(256)
	fan := fanout.New(rdb, func(deviceID string, payload []byte) { hubReg.Deliver(deviceID, payload) })
	defer fan.Close()
	deliverySvc := delivery.NewService(store.NewMessageRepo(pool), rosterRepo, store.NewCursorRepo(pool), fan)
	nodeID := os.Getenv("NODE_ID")
	if nodeID == "" {
		nodeID = "node"
	}
	gw := ws.NewGateway(sess, deliverySvc, hubReg, fan, nodeID)

	apiHandler := httpapi.NewRouterFull(svc, sess, devSvc, kpSvc, rl, rosterRepo)
	root := http.NewServeMux()
	root.Handle("/", apiHandler)
	root.HandleFunc("/ws", gw.Handle)

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: root}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()
	log.Printf("auth+delivery service listening on %s", cfg.HTTPAddr)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
