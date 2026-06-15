package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"agentd/internal/agent"
	"agentd/internal/config"
	"agentd/internal/store"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Message types exchanged over WebSocket.
type WSMessage struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

// Hub manages WebSocket connections per session.
type Hub struct {
	sessions     map[string]*Session
	mu           sync.RWMutex
	workDir      string
	cfg          *config.Config
	sessionStore *store.SessionStore
	envStore     *config.EnvStore
}

// Session is a single chat session (one project).
type Session struct {
	ID        string
	ProjectID string
	Conn      *websocket.Conn
	Send      chan []byte
	hub       *Hub

	// Persistence
	sessionID         string // the persisted session ID (from session store)
	SelectedServerID  string // server selected in UI dropdown

	// ask_user choice mechanism
	choiceCh   chan string
	choiceMu   sync.Mutex
	choiceDone chan struct{}

	// Agent runner
	runner *agent.AgentRunner
}

func newHub() *Hub {
	return &Hub{
		sessions: make(map[string]*Session),
	}
}

func (h *Hub) SetConfig(workDir string, cfg *config.Config, sessionStore *store.SessionStore, envStore *config.EnvStore) {
	h.workDir = workDir
	h.cfg = cfg
	h.sessionStore = sessionStore
	h.envStore = envStore
}

func (h *Hub) run() {}

func (h *Hub) register(s *Session) {
	h.mu.Lock()
	h.sessions[s.ID] = s
	h.mu.Unlock()
}

func (h *Hub) unregister(s *Session) {
	h.mu.Lock()
	delete(h.sessions, s.ID)
	h.mu.Unlock()
}

func (h *Hub) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}

	wsSessionID := r.URL.Query().Get("sessionId")
	if wsSessionID == "" {
		wsSessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
	}
	projectID := r.URL.Query().Get("projectId")
	persistSessionID := r.URL.Query().Get("sid") // persisted session ID from store

	s := &Session{
		ID:         wsSessionID,
		ProjectID:  projectID,
		Conn:       conn,
		Send:       make(chan []byte, 64),
		hub:        h,
		sessionID:  persistSessionID,
		choiceCh:   make(chan string, 1),
		choiceDone: make(chan struct{}, 1),
	}

	// Create agent runtime and runner
	settings := h.cfg.Settings()
	if settings.APIKey != "" {
		runtime := &agent.AgentRuntime{
			WorkDir:         h.workDir,
			Config:          h.cfg,
			EnvStore:        h.envStore,
			Session:         s,
			DefaultServerID: s.SelectedServerID,
		}
		s.runner = agent.NewAgentRunner(settings.APIKey, settings.APIBaseURL, settings.Model, runtime)
		s.runner.Init()
	} else {
		log.Printf("[ws] no API key configured — agent disabled for session %s", wsSessionID)
	}

	h.register(s)

	go s.writePump()
	go s.readPump()
}

func (s *Session) readPump() {
	defer func() {
		s.hub.unregister(s)
		s.Conn.Close()
	}()

	for {
		_, msg, err := s.Conn.ReadMessage()
		if err != nil {
			break
		}

		var wsMsg WSMessage
		if err := json.Unmarshal(msg, &wsMsg); err != nil {
			continue
		}

		s.handleMessage(wsMsg)
	}
}

func (s *Session) writePump() {
	defer s.Conn.Close()
	for msg := range s.Send {
		if err := s.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
		}
	}
}

// SendJSON sends a structured message to this session.
func (s *Session) SendJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	select {
	case s.Send <- data:
	default:
	}
	return nil
}

// WaitForChoice blocks until the user responds to an ask_user prompt.
func (s *Session) WaitForChoice(timeout time.Duration) (string, error) {
	s.choiceMu.Lock()
	s.choiceDone = make(chan struct{}, 1)
	s.choiceMu.Unlock()

	select {
	case choice := <-s.choiceCh:
		return choice, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout waiting for user choice")
	}
}

// handleMessage dispatches incoming messages.
func (s *Session) handleMessage(msg WSMessage) {
	switch msg.Type {
	case "chat":
		s.handleChat(msg)
	case "choice_response":
		s.handleChoiceResponse(msg)
	default:
		log.Printf("unknown message type: %s", msg.Type)
	}
}

func (s *Session) handleChat(msg WSMessage) {
	var payload struct {
		Message   string `json:"message"`
		ProjectID string `json:"projectId"`
		ServerID  string `json:"serverId"`
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[ws] bad chat payload: %v", err)
		return
	}

	// Store selected server on the session for the runtime to use
	if payload.ServerID != "" {
		s.SelectedServerID = payload.ServerID
	}

	// Use session ID from payload if provided (more reliable than WS sid)
	if payload.SessionID != "" {
		s.sessionID = payload.SessionID
	}

	// Load previous messages into the runner so the LLM has full conversation context
	if s.sessionID != "" && s.hub.sessionStore != nil && s.runner != nil {
		pid := s.ProjectID
		if pid == "" {
			pid = filepath.Base(s.hub.workDir)
		}
		swm, err := s.hub.sessionStore.Get(pid, s.sessionID)
		if err == nil && len(swm.Messages) > 0 {
			var history []agent.ChatMessage
			for _, m := range swm.Messages {
				// Only include user and agent messages for LLM context.
				// Tool messages require tool_call_id which isn't stored;
				// their results are already reflected in the agent's responses.
				if m.Role != "user" && m.Role != "agent" {
					continue
				}
				role := m.Role
				if role == "agent" {
					role = "assistant" // LLM expects "assistant"
				}
				history = append(history, agent.ChatMessage{
					Role:    role,
					Content: m.Content,
				})
			}
			s.runner.SetMessages(history)
		}
	}

	if s.runner == nil {
		// Try to create runner if API key was added since connection
		settings := s.hub.cfg.Settings()
		if settings.APIKey != "" {
			runtime := &agent.AgentRuntime{
				WorkDir:         s.hub.workDir,
				Config:          s.hub.cfg,
				EnvStore:        s.hub.envStore,
				Session:         s,
				DefaultServerID: s.SelectedServerID,
			}
			s.runner = agent.NewAgentRunner(settings.APIKey, settings.APIBaseURL, settings.Model, runtime)
			s.runner.Init()
		} else {
			s.SendJSON(map[string]any{
				"type": "agent_error",
				"payload": map[string]any{
					"message": "No API key configured. Please add your API key in Settings first.",
				},
			})
			return
		}
	}

	// Ensure session exists in store
	if s.sessionID == "" && s.hub.sessionStore != nil {
		pid := s.ProjectID
		if pid == "" {
			pid = filepath.Base(s.hub.workDir)
		}
		swm, err := s.hub.sessionStore.Create(pid)
		if err == nil {
			s.sessionID = swm.ID
		}
	}

	// Run agent in background
	go func() {
		ctx := context.Background()
		if err := s.runner.Run(ctx, payload.Message); err != nil {
			log.Printf("[agent] run error: %v", err)
		}

		// Save messages to session store
		if s.hub.sessionStore != nil && s.sessionID != "" {
			msgs := s.runner.GetMessages()
			var storeMsgs []store.Message
			for _, m := range msgs {
				if m.Role == "system" {
					continue
				}
				role := m.Role
				if role == "assistant" {
					role = "agent"
				}
				sm := store.Message{
					Role:    role,
					Content: m.Content,
				}
				// Capture tool metadata
				if m.Name != "" {
					sm.ToolName = m.Name
					// Parse tool result JSON for success/error
					var tr struct {
						Success bool   `json:"success"`
						Output  string `json:"output"`
					}
					if err := json.Unmarshal([]byte(m.Content), &tr); err == nil {
						sm.Content = tr.Output
						// Only mark as error if explicitly false AND there's an error field
						if !tr.Success {
							var trErr struct {
								Success bool   `json:"success"`
								Error   string `json:"error"`
							}
							if err2 := json.Unmarshal([]byte(m.Content), &trErr); err2 == nil && trErr.Error != "" {
								sm.IsError = true
							}
						}
					}
				}
				// Skip empty agent messages (LLM returns empty content with tool_calls)
				if role == "agent" && sm.Content == "" {
					continue
				}
				storeMsgs = append(storeMsgs, sm)
			}
			s.hub.sessionStore.SaveMessages(s.ProjectID, s.sessionID, storeMsgs)
		}
	}()
}

func (s *Session) handleChoiceResponse(msg WSMessage) {
	var payload struct {
		ChoiceID  string `json:"choiceId"`
		ProjectID string `json:"projectId"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[ws] bad choice_response payload: %v", err)
		return
	}

	s.choiceMu.Lock()
	defer s.choiceMu.Unlock()

	select {
	case s.choiceCh <- payload.ChoiceID:
	default:
	}
}
