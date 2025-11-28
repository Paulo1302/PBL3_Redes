package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"server/API" 
)

func main() {
	// 1. Inicializa Store
	store := API.NewStore()

	// 2. Inicializa NATS
	go func() {
		API.SetupPS(store)
		log.Println("NATS Pub/Sub initialized.")
	}()

	// 3. Mantém rodando
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
}