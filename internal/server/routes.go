package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"agentd/internal/config"
	"agentd/internal/deploy"
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

	// Deployments
	s.mux.HandleFunc("GET /api/deployments", s.handleListDeployments)
	s.mux.HandleFunc("GET /api/deployments/{id}", s.handleGetDeployment)
	s.mux.HandleFunc("GET /api/deployments/{id}/health", s.handleDeploymentHealth)
	s.mux.HandleFunc("DELETE /api/deployments/{id}", s.handleDeleteDeployment)

	// Agent chat (WebSocket)
	s.mux.HandleFunc("/api/ws", s.hub.handleWS)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if v, ok := s.cache.Get("settings"); ok {
		json.NewEncoder(w).Encode(v)
		return
	}
	v := s.opts.Config.Settings()
	s.cache.Set("settings", v)
	json.NewEncoder(w).Encode(v)
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
	s.cache.Delete("settings")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.opts.Config.Settings())
}

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if v, ok := s.cache.Get("servers"); ok {
		json.NewEncoder(w).Encode(v)
		return
	}
	v := s.opts.Config.ListServers()
	s.cache.Set("servers", v)
	json.NewEncoder(w).Encode(v)
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
	s.cache.Delete("servers")
	s.cache.Delete("stats")
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
	s.cache.Delete("servers")
	s.cache.Delete("stats")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(srv)
}

func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.opts.Config.DeleteServer(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cache.Delete("servers")
	s.cache.Delete("stats")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if v, ok := s.cache.Get("projects"); ok {
		json.NewEncoder(w).Encode(v)
		return
	}
	projects, err := project.Scan(s.opts.WorkDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cache.SetWithTTL("projects", projects, 30*time.Second)
	json.NewEncoder(w).Encode(projects)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if v, ok := s.cache.Get("stats"); ok {
		json.NewEncoder(w).Encode(v)
		return
	}

	servers := s.opts.Config.ListServers()
	projects, _ := project.Scan(s.opts.WorkDir)
	hasAPIKey := s.opts.Config.Settings().APIKey != ""

	deploymentCount := 0
	if s.deploymentStore != nil {
		if recs, err := s.deploymentStore.List(); err == nil {
			deploymentCount = len(recs)
		}
	}

	v := map[string]any{
		"serverCount":      len(servers),
		"projectCount":     len(projects),
		"deploymentCount":  deploymentCount,
		"hasAPIKey":        hasAPIKey,
		"workDir":          s.opts.WorkDir,
	}
	s.cache.Set("stats", v)
	json.NewEncoder(w).Encode(v)
}

// --- Session handlers ---

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("projectId")
	if s.sessionStore == nil {
		json.NewEncoder(w).Encode([]any{})
		return
	}
	cacheKey := "sessions:" + projectID
	if v, ok := s.cache.Get(cacheKey); ok {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
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
	s.cache.Set(cacheKey, sessions)
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
	s.cache.Delete("sessions:" + body.ProjectID)
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
	s.cache.Delete("sessions:" + projectID)
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
	if v, ok := s.cache.Get("env"); ok {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
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
	s.cache.Set("env", entries)
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
	s.cache.Delete("env")
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
	s.cache.Delete("env")
	w.WriteHeader(http.StatusNoContent)
}

// --- Deployment handlers ---

func (s *Server) handleListDeployments(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.deploymentStore == nil {
		json.NewEncoder(w).Encode([]any{})
		return
	}
	if v, ok := s.cache.Get("deployments"); ok {
		json.NewEncoder(w).Encode(v)
		return
	}
	recs, err := s.deploymentStore.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cache.Set("deployments", recs)
	json.NewEncoder(w).Encode(recs)
}

func (s *Server) handleGetDeployment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.deploymentStore == nil {
		http.Error(w, "deployment store not available", http.StatusInternalServerError)
		return
	}
	rec, err := s.deploymentStore.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rec)
}

func (s *Server) handleDeploymentHealth(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.deploymentStore == nil {
		http.Error(w, "deployment store not available", http.StatusInternalServerError)
		return
	}

	rec, err := s.deploymentStore.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Find the server to SSH into
	srv, ok := s.opts.Config.GetServer(rec.ServerID)
	if !ok {
		http.Error(w, fmt.Sprintf("server %s not found", rec.ServerID), http.StatusNotFound)
		return
	}

	port := rec.Port
	if port == 0 {
		port = 3000
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	healthStatus := "healthy"
	output, err := runHealthCheckAPI(client, port)
	if err != nil || !strings.Contains(output, "PORT_OK") {
		healthStatus = "unhealthy"
	}

	s.deploymentStore.UpdateHealth(rec.ID, healthStatus, time.Now())
	s.cache.Delete("deployments")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":           rec.ID,
		"projectName":  rec.ProjectName,
		"host":         rec.Host,
		"port":         port,
		"healthStatus": healthStatus,
		"lastChecked":  time.Now(),
		"output":       output,
	})
}

func (s *Server) handleDeleteDeployment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.deploymentStore == nil {
		http.Error(w, "deployment store not available", http.StatusInternalServerError)
		return
	}
	if err := s.deploymentStore.Delete(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cache.Delete("deployments")
	s.cache.Delete("stats")
	w.WriteHeader(http.StatusNoContent)
}

// runHealthCheckAPI performs an HTTP health check via SSH on the given port.
func runHealthCheckAPI(client *deploy.SSHClient, port int) (string, error) {
	// Try curl first
	cmd := fmt.Sprintf("curl -s -o /dev/null -w '%%{http_code}' --max-time 5 http://localhost:%d 2>&1 || echo 'CURL_FAILED'", port)
	output, err := client.Run(cmd)
	if err == nil && !strings.Contains(output, "CURL_FAILED") && output != "" {
		status := strings.TrimSpace(output)
		if status >= "200" && status < "500" {
			return fmt.Sprintf("HTTP %s on port %d — PORT_OK", status, port), nil
		}
		return fmt.Sprintf("HTTP %s on port %d", status, port), nil
	}

	// Fallback: check if port is listening
	checkCmd := fmt.Sprintf("ss -tlnp | grep -q ':%d ' && echo 'LISTENING' || echo 'NOT_LISTENING'", port)
	checkOut, _ := client.Run(checkCmd)
	if strings.Contains(checkOut, "LISTENING") {
		return fmt.Sprintf("Port %d is LISTENING — PORT_OK", port), nil
	}
	return fmt.Sprintf("Port %d is NOT_LISTENING", port), nil
}
