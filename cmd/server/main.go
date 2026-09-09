package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/carlallen/RB3EnhancedCompanion/internal/db"
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

	staticDir := "web/static"
	templateDir := "web/templates"

	storeDir := os.Getenv("STORE_DIR")
	if storeDir == "" {
		storeDir = "store"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = filepath.Join(storeDir, "rb3ecompanion.db")
	}

	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("db: failed to open %s: %v", dbPath, err)
	}
	defer database.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := rb3net.NewHub()

	if saved, err := db.LoadSongs(database); err != nil {
		log.Fatalf("db: failed to load songs: %v", err)
	} else if len(saved) > 0 {
		songList := make([]rb3net.Song, len(saved))
		for i, s := range saved {
			songList[i] = rb3net.Song(s)
		}
		hub.Mutate(func(s *rb3net.GameState) {
			s.SongList = songList
			s.SongListVersion++
		})
	}

	listener := &rb3net.Listener{Addr: udpAddr, Hub: hub}
	go func() {
		log.Printf("listening for RB3Enhanced on %s (udp)", udpAddr)
		if err := listener.Run(ctx); err != nil {
			log.Fatalf("udp listener error: %v", err)
		}
	}()

	metadataDirs := []string{filepath.Join(storeDir, "metadata")}
	songs := &rb3net.SongListWatcher{Hub: hub, DB: database, MetadataDirs: metadataDirs}
	go songs.Run(ctx)

	wled := &rb3net.WLEDWatcher{Hub: hub, DB: database}
	go wled.Run(ctx)

	dbCheck := &rb3net.RB3ECDBCheckWatcher{Hub: hub, DB: database, StoreDir: storeDir, MetadataDirs: metadataDirs}
	go dbCheck.Run(ctx)

	srv := &http.Server{
		Addr:    addr,
		Handler: server.NewRouter(hub, database, staticDir, storeDir, templateDir),
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
