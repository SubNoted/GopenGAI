package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
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
	engine := agent.NewEngine(client, toolReg, histRepo, agentReg, queries, cfg, eventBus)

	handler := &api.Handler{
		LLM:      client,
		DB:       queries,
		SQLDB:    database,
		Config:   cfg,
		Engine:   engine,
		EventBus: eventBus,
		History:  histRepo,
	}

	// Track active engine goroutines for graceful shutdown.
	wg := &sync.WaitGroup{}
	handler.Wg = wg

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, handler)

	// Wrap the mux with middleware: recovery → CORS → logging.
	wrapped := api.ApplyMiddleware(mux,
		api.RecoveryMiddleware,
		api.CORSMiddleware,
		api.LoggingMiddleware,
	)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

	srv := &http.Server{
		Addr:              addr,
		Handler:           wrapped,
		ReadHeaderTimeout: 10 * time.Second,  // protect against slow-loris
		ReadTimeout:       30 * time.Second,  // full request read deadline
		IdleTimeout:       120 * time.Second, // keep-alive connection timeout
		// WriteTimeout is intentionally 0 — SSE connections must stay open
		// indefinitely. Regular endpoints are protected by ReadTimeout.
	}

	// Channel to listen for OS signals for graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Channel for fatal server startup errors (e.g., EADDRINUSE).
	serverErr := make(chan error, 1)

	// Start server in a goroutine.
	go func() {
		log.Printf("GoPengAI listening on %s", addr)
		log.Printf("Endpoints:")
		log.Printf("  GET  /health                      health check")
		log.Printf("  POST /session                     create session")
		log.Printf("  GET  /session                     list sessions")
		log.Printf("  GET  /session/{id}                get session + messages")
		log.Printf("  PATCH /session/{id}               update session title")
		log.Printf("  DELETE /session/{id}              delete session")
		log.Printf("  GET  /session/{id}/messages       get active branch messages")
		log.Printf("  GET  /session/{id}/branches       list session branches")
		log.Printf("  POST /session/{id}/fork           fork session at message")
		log.Printf("  PUT  /session/{id}/branch         select active branch")
		log.Printf("  POST /session/{id}/abort          abort running generation")
		log.Printf("  POST /session/{id}/message        send message (history-aware)")
		log.Printf("  GET  /events                      SSE stream (all sessions)")
		log.Printf("  GET  /session/{id}/events         SSE stream (session-specific)")
		log.Printf("  PATCH /messages/{id}              edit message (new branch)")
		log.Printf("  GET  /agents                      list agents")
		log.Printf("  GET  /agents/{name}               get agent details")
		log.Printf("  GET  /memory?agent=NAME           list memory facts")
		log.Printf("  GET  /memory/{key}?agent=NAME     get specific memory fact")
		log.Printf("  GET  /v1/models                   list agents as models")
		log.Printf("  POST /v1/chat/completions         OpenAI-compatible pass-through")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Non-ErrServerClosed error (e.g., EADDRINUSE) — signal
			// the main goroutine to perform clean shutdown instead of
			// os.Exit which would bypass all cleanup.
			serverErr <- err
		}
	}()

	// Block until signal received or fatal server error.
	var shutdownReason string
	select {
	case sig := <-quit:
		shutdownReason = fmt.Sprintf("received signal %v", sig)
	case err := <-serverErr:
		shutdownReason = fmt.Sprintf("server error: %v", err)
	}
	log.Printf("%s, shutting down...", shutdownReason)

	// Create a context with timeout for graceful shutdown.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Disable keep-alives so idle connections don't block Shutdown.
	srv.SetKeepAlivesEnabled(false)

	// Shutdown HTTP server (drain in-flight requests).
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	// Wait for all active engine goroutines to finish before closing
	// the database (prevents "database is closed" errors in goroutines).
	wg.Wait()

	// Stop event bus (stops heartbeat, closes subscriber channels).
	eventBus.Close()

	// Close database.
	database.Close()

	log.Println("shutdown complete")
}
