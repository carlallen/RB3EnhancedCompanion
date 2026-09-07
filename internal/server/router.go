package server

import (
	"html/template"
	"net/http"
	"os"
	"path/filepath"

	"github.com/carlallen/RB3EnhancedCompanion/internal/rb3net"
)

const defaultAlbumArt = "blank_album_art_keep.png"

func NewRouter(hub *rb3net.Hub, staticDir, storeDir, templateDir string) http.Handler {
	indexTmpl := template.Must(template.ParseFiles(filepath.Join(templateDir, "index.html")))
	configTmpl := template.Must(template.ParseFiles(filepath.Join(templateDir, "config.html")))

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.Handle("/", handleIndex(indexTmpl))
	mux.Handle("/config", handlePage(configTmpl))
	mux.HandleFunc("/ws", handleWS(hub))
	mux.HandleFunc("/jump", handleJump(hub))

	staticArtDir := filepath.Join(staticDir, "art")
	storeArtDir := filepath.Join(storeDir, "art")
	mux.Handle("/static/art/", http.StripPrefix("/static/art/", handleArt(storeArtDir, staticArtDir)))

	fs := http.FileServer(http.Dir(staticDir))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	return mux
}

// handleArt serves album art, preferring a custom file in storeArtDir, then
// falling back to the bundled artwork in staticArtDir, then a default image.
func handleArt(storeArtDir, staticArtDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)

		candidates := []string{
			filepath.Join(storeArtDir, name),
			filepath.Join(staticArtDir, name),
			filepath.Join(staticArtDir, defaultAlbumArt),
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

func handlePage(tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := tmpl.Execute(w, nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
