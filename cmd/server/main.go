package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"linksaverback/internal/config"
	"linksaverback/internal/database"
	"linksaverback/internal/router"
)

func main() {
	cfg := config.Load()

	log.Println("==================================================")
	log.Printf(" LinkSaverBack v%s Starting...", cfg.Version)
	log.Println(" Multiplatform Game Save Synchronization Engine")
	log.Println("==================================================")

	if err := cfg.EnsureDirectories(); err != nil {
		log.Fatalf("Fatal: Failed to create storage directories: %v", err)
	}

	db, err := database.InitDB(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("Fatal: Database initialization failed: %v", err)
	}

	engine, err := router.SetupRouter(cfg, db)
	if err != nil {
		log.Fatalf("Fatal: Router initialization failed: %v", err)
	}

	addr := fmt.Sprintf(":%s", cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      engine,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown channel
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("[Server] LinkSaverBack HTTP listening on %s (Web UI + API)", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-quit
	log.Println("[Server] Shutting down LinkSaverBack gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("[Server] LinkSaverBack stopped cleanly.")
}
