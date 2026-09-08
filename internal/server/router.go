package server

import (
	"database/sql"
	"html/template"
	"net/http"
	"os"
	"path/filepath"

	"github.com/carlallen/RB3EnhancedCompanion/internal/rb3net"
)

const defaultAlbumArt = "blank_album_art_keep.png"

func NewRouter(hub *rb3net.Hub, database *sql.DB, staticDir, storeDir, templateDir string) http.Handler {
	indexTmpl := template.Must(template.ParseFiles(filepath.Join(templateDir, "index.html")))
	configTmpl := template.Must(template.ParseFiles(filepath.Join(templateDir, "config.html")))

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.Handle("/", handleIndex(indexTmpl))
	mux.Handle("/config", handleConfig(configTmpl, database))
	mux.Handle("/config/wled-devices", handleAddWLEDDevice(database))
	mux.Handle("/config/wled-devices/delete", handleDeleteWLEDDevice(database))
	mux.Handle("/config/wled-devices/enabled", handleWLEDDeviceEnabled(database))
	mux.Handle("/config/wled-devices/channels", handleWLEDDeviceChannels(configTmpl, database))
	mux.HandleFunc("/ws", handleWS(hub))
	mux.HandleFunc("/jump", handleJump(hub))

	staticImagesDir := filepath.Join(staticDir, "images")
	storeArtDir := filepath.Join(storeDir, "art")
	mux.Handle("/static/art/", http.StripPrefix("/static/art/", handleArt(storeArtDir, staticImagesDir)))

	fs := http.FileServer(http.Dir(staticDir))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	return mux
}

// handleArt serves album art, preferring a custom file in storeArtDir, then
// falling back to a default image.
func handleArt(storeArtDir, staticImagesDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)

		candidates := []string{
			filepath.Join(storeArtDir, name),
			filepath.Join(staticImagesDir, defaultAlbumArt),
		}

		for _, path := range candidates {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				http.ServeFile(w, r, path)
				return
			}
		}

		http.NotFound(w, r)
	}
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func handleIndex(tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if err := tmpl.Execute(w, nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
