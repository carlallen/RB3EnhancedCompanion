package server

import (
	"html/template"
	"net/http"
	"path/filepath"

	"github.com/carlallen/RB3EnhancedCompanion/internal/rb3net"
)

func NewRouter(hub *rb3net.Hub, staticDir, templateDir string) http.Handler {
	indexTmpl := template.Must(template.ParseFiles(filepath.Join(templateDir, "index.html")))
	configTmpl := template.Must(template.ParseFiles(filepath.Join(templateDir, "config.html")))

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.Handle("/", handleIndex(indexTmpl))
	mux.Handle("/config", handlePage(configTmpl))
	mux.HandleFunc("/ws", handleWS(hub))
	mux.HandleFunc("/jump", handleJump(hub))

	fs := http.FileServer(http.Dir(staticDir))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	return mux
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
