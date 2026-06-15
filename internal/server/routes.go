package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"agentd/internal/config"
	"agentd/internal/project"
	"agentd/internal/store"
)

func (s *Server) routes() {
	// Health
	s.mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Settings
	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("PUT /api/settings", s.handlePutSettings)

	// Servers
	s.mux.HandleFunc("GET /api/servers", s.handleListServers)
	s.mux.HandleFunc("POST /api/servers", s.handleCreateServer)
	s.mux.HandleFunc("GET /api/servers/{id}", s.handleGetServer)
	s.mux.HandleFunc("PUT /api/servers/{id}", s.handleUpdateServer)
	s.mux.HandleFunc("DELETE /api/servers/{id}", s.handleDeleteServer)

	// Projects
	s.mux.HandleFunc("GET /api/projects", s.handleListProjects)
	s.mux.HandleFunc("GET /api/projects/{id}", s.handleGetProject)

	// Sessions
	s.mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	s.mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	s.mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	s.mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)

	// Stats
	s.mux.HandleFunc("GET /api/stats", s.handleStats)

	// Environment variables
	s.mux.HandleFunc("GET /api/env", s.handleListEnv)
	s.mux.HandleFunc("PUT /api/env", s.handlePutEnv)
	s.mux.HandleFunc("DELETE /api/env/{key}", s.handleDeleteEnv)

	// Agent chat (WebSocket)
	s.mux.HandleFunc("/api/ws", s.hub.handleWS)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.opts.Config.Settings())
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var settings struct {
		APIKey        string `json:"apiKey"`
		DeepSeekAPIKey string `json:"deepseekApiKey"` // backward compat
		APIBaseURL    string `json:"apiBaseUrl"`
		Model         string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Backward compat: if old deepseekApiKey is set, use it as the API key
	apiKey := settings.APIKey
	if apiKey == "" && settings.DeepSeekAPIKey != "" {
		apiKey = settings.DeepSeekAPIKey
	}
	baseURL := settings.APIBaseURL
	if baseURL == "" {
		baseURL = config.DefaultBaseURL
	}
	s.opts.Config.UpdateSettings(config.Settings{
		APIKey:     apiKey,
		APIBaseURL: baseURL,
		Model:      settings.Model,
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.opts.Config.Settings())
}

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.opts.Config.ListServers())
}

func (s *Server) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	var srv config.ServerConfig
	if err := json.NewDecoder(r.Body).Decode(&srv); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	created, err := s.opts.Config.AddServer(srv)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(created)
}

func (s *Server) handleGetServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, ok := s.opts.Config.GetServer(id)
	if !ok {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(srv)
}

func (s *Server) handleUpdateServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var srv config.ServerConfig
	if err := json.NewDecoder(r.Body).Decode(&srv); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	srv.ID = id
	if err := s.opts.Config.UpdateServer(srv); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(srv)
}

func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.opts.Config.DeleteServer(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := project.Scan(s.opts.WorkDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projects)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	servers := s.opts.Config.ListServers()
	projects, _ := project.Scan(s.opts.WorkDir)
	hasAPIKey := s.opts.Config.Settings().APIKey != ""

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"serverCount":  len(servers),
		"projectCount": len(projects),
		"hasAPIKey":    hasAPIKey,
		"workDir":      s.opts.WorkDir,
	})
}

// --- Session handlers ---

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("projectId")
	if s.sessionStore == nil {
		json.NewEncoder(w).Encode([]any{})
		return
	}
	sessions, err := s.sessionStore.List(projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []store.Session{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProjectID string `json:"projectId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.sessionStore == nil {
		http.Error(w, "session store not available", http.StatusInternalServerError)
		return
	}
	swm, err := s.sessionStore.Create(body.ProjectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(swm)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	projectID := r.URL.Query().Get("projectId")
	if s.sessionStore == nil {
		http.Error(w, "session store not available", http.StatusInternalServerError)
		return
	}
	swm, err := s.sessionStore.Get(projectID, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(swm)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	projectID := r.URL.Query().Get("projectId")
	if s.sessionStore == nil {
		http.Error(w, "session store not available", http.StatusInternalServerError)
		return
	}
	if err := s.sessionStore.Delete(projectID, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// If the ID matches the workdir basename, it's the root project
	rootName := filepath.Base(s.opts.WorkDir)
	var p project.Project
	var err error

	if id == rootName {
		p, err = project.AnalyzeDir(s.opts.WorkDir, rootName)
	} else {
		p, err = project.Analyze(s.opts.WorkDir, id)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

// --- Environment variable handlers ---

func (s *Server) handleListEnv(w http.ResponseWriter, r *http.Request) {
	if s.envStore == nil {
		json.NewEncoder(w).Encode([]config.EnvEntry{})
		return
	}
	entries, err := s.envStore.ListMasked()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []config.EnvEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

func (s *Server) handlePutEnv(w http.ResponseWriter, r *http.Request) {
	if s.envStore == nil {
		http.Error(w, "env store not available", http.StatusInternalServerError)
		return
	}
	var body struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.Key == "" {
		http.Error(w, "key is required", http.StatusBadRequest)
		return
	}
	if err := s.envStore.Upsert(body.Key, body.Value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Return updated list
	s.handleListEnv(w, r)
}

func (s *Server) handleDeleteEnv(w http.ResponseWriter, r *http.Request) {
	if s.envStore == nil {
		http.Error(w, "env store not available", http.StatusInternalServerError)
		return
	}
	key := r.PathValue("key")
	if err := s.envStore.Delete(key); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
