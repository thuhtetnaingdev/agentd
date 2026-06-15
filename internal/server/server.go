package server

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"time"

	"agentd/internal/config"
	"agentd/internal/store"
)

//go:embed all:web-dist
var webDist embed.FS

// Options configures the server.
type Options struct {
	Port         int
	WorkDir      string
	Config       *config.Config
	SessionStore *store.SessionStore
	EnvStore     *config.EnvStore
}

// Server is the HTTP+WebSocket server.
type Server struct {
	opts         Options
	http         *http.Server
	mux          *http.ServeMux
	hub          *Hub
	sessionStore *store.SessionStore
	envStore     *config.EnvStore
}

// New creates a new Server.
func New(opts Options) (*Server, error) {
	s := &Server{
		opts:         opts,
		mux:          http.NewServeMux(),
		hub:          newHub(),
		sessionStore: opts.SessionStore,
		envStore:     opts.EnvStore,
	}
	s.hub.SetConfig(opts.WorkDir, opts.Config, opts.SessionStore, opts.EnvStore)

	s.routes()

	// Serve embedded React SPA
	distFS, err := fs.Sub(webDist, "web-dist")
	if err != nil {
		// In development, web-dist might not exist; that's ok
		distFS = nil
	}
	if distFS != nil {
		fileServer := http.FileServer(http.FS(distFS))
		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// SPA fallback: serve index.html for non-API routes
			if r.URL.Path != "/" {
				f, err := distFS.Open(r.URL.Path[1:])
				if err != nil {
					index, _ := distFS.Open("index.html")
					if index != nil {
						http.ServeContent(w, r, "index.html", time.Time{}, index.(io.ReadSeeker))
						return
					}
				} else {
					f.Close()
				}
			}
			fileServer.ServeHTTP(w, r)
		})
	}

	s.http = &http.Server{
		Addr:    fmt.Sprintf(":%d", opts.Port),
		Handler: corsMiddleware(s.mux),
	}

	return s, nil
}

// Start begins listening.
func (s *Server) Start() error {
	go s.hub.run()
	return s.http.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.http.Shutdown(ctx)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
