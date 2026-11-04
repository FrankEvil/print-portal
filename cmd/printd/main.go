package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/liuzhaojun/print-portal/internal/config"
)

func main() {
	cfg := config.Load()
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"version": "0.1.0",
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	log.Printf("printd listening on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, mux); err != nil {
		log.Fatal(err)
	}
}
