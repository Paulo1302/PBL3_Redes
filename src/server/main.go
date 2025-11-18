package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"server/API" 
)

func main() {
    port := "8080"
    if envPort := os.Getenv("HTTP_PORT"); envPort != "" {
        port = envPort
    }

	log.Println("Starting Centralized Game Server...")

	// 1. Inicializa Store
	store := API.NewStore()

	// 2. Inicializa NATS
	go func() {
		API.SetupPS(store)
		log.Println("NATS Pub/Sub initialized.")
	}()

	// 3. Configura HTTP (opcional, se ainda usar para debug ou status)
	router := API.SetupRouter(store)
	
    go func() {
        addr := ":" + port
        log.Printf("HTTP Server listening on %s", addr)
        if err := http.ListenAndServe(addr, router); err != nil {
            log.Fatal("HTTP Server Error:", err)
        }
    }()

	// 4. Mantém rodando
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
}