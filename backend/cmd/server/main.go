package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"aiopencad/backend/internal/config"
	"aiopencad/backend/internal/httpapi"
	"aiopencad/backend/internal/llm"
	"aiopencad/backend/internal/store"
)

func main() {
	cfg := config.Load()

	db, err := store.OpenSQLite(cfg.DBPath)
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	modelClient := llm.NewClient(cfg.LLM)
	server := httpapi.NewServer(db, modelClient, cfg)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	if cfg.FrontendDist != "" {
		abs, _ := filepath.Abs(cfg.FrontendDist)
		log.Printf("serving frontend from %s", abs)
	}

	log.Printf("AI OpenCAD backend listening on %s", cfg.Addr)
	if cfg.LLM.APIKey == "" {
		log.Printf("LLM apiKey is empty; using built-in demo CAD responses")
	} else {
		log.Printf("using OpenAI Responses API at %s with model %s, reasoning=%s, webSearch=%t", cfg.LLM.BaseURL, cfg.LLM.Model, cfg.LLM.ReasoningEffort, cfg.LLM.EnableWebSearch)
	}

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("server failed: %v", err)
		os.Exit(1)
	}
}
