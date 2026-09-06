package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/carlallen/RB3EnhancedCompanion/internal/rb3net"
	"github.com/carlallen/RB3EnhancedCompanion/internal/server"
)

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	udpAddr := os.Getenv("UDP_ADDR")
	if udpAddr == "" {
		udpAddr = ":21070"
	}

	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = "web/static"
	}

	templateDir := os.Getenv("TEMPLATE_DIR")
	if templateDir == "" {
		templateDir = "web/templates"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := rb3net.NewHub()
	listener := &rb3net.Listener{Addr: udpAddr, Hub: hub}
	go func() {
		log.Printf("listening for RB3Enhanced on %s (udp)", udpAddr)
		if err := listener.Run(ctx); err != nil {
			log.Fatalf("udp listener error: %v", err)
		}
	}()

	srv := &http.Server{
		Addr:    addr,
		Handler: server.NewRouter(staticDir, templateDir),
	}

	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	log.Println("shutting down")
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}
}
