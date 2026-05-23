package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"gopengai/internal/api"
	"gopengai/internal/config"
	"gopengai/internal/db"
	"gopengai/internal/llm"
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
	defer database.Close()

	// Run migrations
	if err := db.Migrate(database); err != nil {
		log.Fatalf("run migrations: %v", err)
	}
	log.Printf("database migrated: %s", cfg.DataDir+"/gopengai.db")

	client := llm.NewClient(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model)
	handler := &api.Handler{LLM: client}

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, handler)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("GoPengAI listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
