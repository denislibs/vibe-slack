package main

import (
	"log"

	"github.com/messenger/backend/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	log.Printf("auth-service starting on %s", cfg.HTTPAddr)
	// Wiring is added in later tasks.
}
