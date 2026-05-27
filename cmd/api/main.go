package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopengai/internal/agent"
	"gopengai/internal/api"
	"gopengai/internal/config"
	"gopengai/internal/db"
	"gopengai/internal/history"
	"gopengai/internal/llm"
	"gopengai/internal/tools"
)

func main() {
	cfgPath := "gopengai.json"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// Open database
	database, err := db.Open(cfg.DataDir + "/gopengai.db")
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	// Run migrations
	if err := db.Migrate(database); err != nil {
		database.Close()
		log.Fatalf("run migrations: %v", err)
	}
	log.Printf("database migrated: %s", cfg.DataDir+"/gopengai.db")

	// Initialize sqlc-generated queries
	queries := db.New(database)

	client := llm.NewClientFromConfig(cfg.LLM)

	// Initialize agent registry.
	agentReg := agent.NewRegistry()
	if _, err := agentReg.InitializeFromDir(cfg.AgentsDir); err != nil {
		log.Printf("warning: loading agents from %s: %v (continuing with empty registry)", cfg.AgentsDir, err)
	}

	// Initialize tool registry with built-in tools.
	toolReg := tools.NewRegistry()
	toolReg.Register(&tools.WebFetchTool{})
	toolReg.Register(&tools.MemorySave{})
	toolReg.Register(&tools.MemoryRecall{})
	toolReg.Register(&tools.DelegateTool{})
	log.Printf("registered %d tools", toolReg.Size())

	// Initialize history repository.
	histRepo := history.NewRepository(queries, database)

	// Initialize event bus.
	eventBus := api.NewEventBus()

	// Initialize agent engine.
	engine := agent.NewEngine(client, toolReg, histRepo, agentReg, database, queries, cfg, eventBus)

	handler := &api.Handler{
		LLM:      client,
		DB:       queries,
		SQLDB:    database,
		Config:   cfg,
		Engine:   engine,
		EventBus: eventBus,
	}

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, handler)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Channel to listen for OS signals for graceful shutdown.
	quit := make(chan os.Signal, 2) // buffer of 2: first = graceful, second = force-kill
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine.
	go func() {
		log.Printf("GoPengAI listening on %s", addr)
		log.Printf("Endpoints:")
		log.Printf("  POST /session                     create session")
		log.Printf("  GET  /session                     list sessions")
		log.Printf("  GET  /session/{id}                get session + messages")
		log.Printf("  DELETE /session/{id}              delete session")
		log.Printf("  POST /session/{id}/message        send message (history-aware)")
		log.Printf("  POST /v1/chat/completions         OpenAI-compatible (no history)")
		log.Printf("  GET  /health                      health check")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("server error: %v", err)
			os.Exit(1)
		}
	}()

	// Block until signal received.
	sig := <-quit
	log.Printf("received signal %v, shutting down...", sig)

	// If a second signal arrives during graceful shutdown, force exit immediately.
	go func() {
		sig2 := <-quit
		log.Printf("received second signal %v, forcing exit", sig2)
		os.Exit(1)
	}()

	// Create a context with timeout for graceful shutdown.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Shutdown HTTP server (drain connections).
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	// Stop event bus (stops heartbeat, closes subscriber channels).
	eventBus.Close()

	// Close prepared statements.
	queries.Close()

	// Close database.
	database.Close()

	log.Println("shutdown complete")
}
