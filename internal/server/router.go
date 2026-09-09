package server

import (
	"database/sql"
	"html/template"
	"net/http"
	"os"
	"path/filepath"

	"github.com/carlallen/RB3EnhancedCompanion/internal/rb3net"
)

func NewRouter(hub *rb3net.Hub, database *sql.DB, staticDir, storeDir, templateDir string) http.Handler {
	indexTmpl := template.Must(template.ParseFiles(filepath.Join(templateDir, "index.html")))
	configTmpl := template.Must(template.ParseFiles(filepath.Join(templateDir, "config.html")))

	storeArtDir := filepath.Join(storeDir, "art")

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.Handle("/", handleIndex(indexTmpl))
	mux.Handle("/config", handleConfig(configTmpl, database))
	mux.Handle("/config/wled-devices", handleAddWLEDDevice(database))
	mux.Handle("/config/wled-devices/delete", handleDeleteWLEDDevice(database))
	mux.Handle("/config/wled-devices/enabled", handleWLEDDeviceEnabled(database))
	mux.Handle("/config/wled-devices/channels", handleWLEDDeviceChannels(configTmpl, database))
	mux.HandleFunc("/ws", handleWS(hub, storeArtDir))
	mux.HandleFunc("/jump", handleJump(hub))

	mux.Handle("/static/art/", http.StripPrefix("/static/art/", handleArt(storeArtDir)))

	fs := http.FileServer(http.Dir(staticDir))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	return mux
}

// handleArt serves album art downloaded into storeArtDir. The dashboard only
// requests a song's art when the song list data it was pushed says the song
// has some (see songDTO.HasArt) - it uses its own default image directly
// otherwise - so there's no fallback guessing to do here.
func handleArt(storeArtDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		path := filepath.Join(storeArtDir, name)

		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}

		// A shortname's art URL never changes even when the file backing it
		// does (e.g. rb3net.RB3ECDBCheckWatcher downloading art after the
		// fact), so browsers must always revalidate rather than trust a
		// previously cached copy.
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, path)
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
