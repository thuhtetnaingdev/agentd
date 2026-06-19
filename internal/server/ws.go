package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
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
	sessions        map[string]*Session
	mu              sync.RWMutex
	workDir         string
	cfg             *config.Config
	sessionStore    *store.SessionStore
	deploymentStore *store.DeploymentStore
	envStore        *config.EnvStore
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
	runner   *agent.AgentRunner
	cancelFn context.CancelFunc // cancels the running agent loop
	cancelMu sync.Mutex
}

func newHub() *Hub {
	return &Hub{
		sessions: make(map[string]*Session),
	}
}

func (h *Hub) SetConfig(workDir string, cfg *config.Config, sessionStore *store.SessionStore, deploymentStore *store.DeploymentStore, envStore *config.EnvStore) {
	h.workDir = workDir
	h.cfg = cfg
	h.sessionStore = sessionStore
	h.deploymentStore = deploymentStore
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
		Send:       make(chan []byte, 256),
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
			DeploymentStore: h.deploymentStore,
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
		log.Printf("[ws] dropping message type=%s (buffer full)", s.messageType(v))
	}
	return nil
} 

// messageType extracts the "type" field from a JSON-like map.
func (s *Session) messageType(v any) string {
	if m, ok := v.(map[string]any); ok {
		if t, ok := m["type"].(string); ok {
			return t
		}
	}
	return "unknown"
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
	case "cancel":
		s.handleCancel()
	default:
		log.Printf("unknown message type: %s", msg.Type)
	}
}

// handleCancel stops the currently running agent loop.
func (s *Session) handleCancel() {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	if s.cancelFn != nil {
		s.cancelFn()
		s.cancelFn = nil
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

	// Load previous messages into the runner so the LLM has full conversation context.
	// Only reload from store if the runner has fewer messages (page refresh / new connection).
	// If the runner already has accumulated messages from this session, skip the reload
	// to preserve the live byte-identical message history (critical for prompt cache).
	if s.sessionID != "" && s.hub.sessionStore != nil && s.runner != nil {
		pid := s.ProjectID
		if pid == "" {
			pid = filepath.Base(s.hub.workDir)
		}
		// Check if runner already has messages beyond the system prompt
		runnerMsgs := s.runner.GetMessages()
		needsReload := len(runnerMsgs) <= 1 // only system prompt = needs reload
		if needsReload {
			swm, err := s.hub.sessionStore.Get(pid, s.sessionID)
			if err == nil && len(swm.Messages) > 0 {
				history := BuildAgentMessages(swm.Messages)
				s.runner.SetMessages(history)
			}
		}
	}

	if s.runner == nil {
		// Try to create runner if API key was added since connection
		settings := s.hub.cfg.Settings()
		if settings.APIKey != "" {
			runtime := &agent.AgentRuntime{
				WorkDir:         s.hub.workDir,
				Config:          s.hub.cfg,
				DeploymentStore: s.hub.deploymentStore,
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

	// Sync selected server from UI to the runtime (may have changed since runner was created).
	if s.runner != nil {
		s.runner.SetDefaultServerID(s.SelectedServerID)
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

	// Cancel any previous run
	s.cancelMu.Lock()
	if s.cancelFn != nil {
		s.cancelFn()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFn = cancel
	s.cancelMu.Unlock()

	// Run agent in background
	go func() {
		defer func() {
			s.cancelMu.Lock()
			s.cancelFn = nil
			s.cancelMu.Unlock()
		}()
		if err := s.runner.RunStreaming(ctx, payload.Message); err != nil {
			if err == context.Canceled {
				s.SendJSON(map[string]any{
					"type":    "agent_cancelled",
					"payload": map[string]any{},
				})
				return
			}
			log.Printf("[agent] run error: %v", err)
		}

		// Save messages to session store
		if s.hub.sessionStore != nil && s.sessionID != "" {
			storeMsgs := BuildStoreMessages(s.runner.GetMessages())
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

// BuildAgentMessages converts stored store.Message back to agent.ChatMessage format.
// This is the inverse of BuildStoreMessages — it reconstructs the full message
// history including tool calls and tool results so the LLM gets byte-identical
// context, which is critical for DeepSeek's prefix-based prompt cache.
//
// The store interleaves tool_call entries with their tool results:
//   user → agent → tool_call(tc1) → tool(r1) → tool_call(tc2) → tool(r2) → ...
// This function groups consecutive tool_call+tool pairs back into a single
// assistant message with multiple ToolCalls followed by tool result messages.
func BuildAgentMessages(msgs []store.Message) []agent.ChatMessage {
	var out []agent.ChatMessage

	// Build a map of tool results by ToolCallID for quick lookup
	toolResults := map[string]string{}
	toolNames := map[string]string{}
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID != "" {
			toolResults[m.ToolCallID] = m.Content
			toolNames[m.ToolCallID] = m.ToolName
		}
	}

	for i := 0; i < len(msgs); i++ {
		m := msgs[i]

		switch m.Role {
		case "user":
			out = append(out, agent.ChatMessage{Role: "user", Content: m.Content})

		case "agent":
			content := m.Content
			i++

			// Check if the NEXT messages are tool_call entries from the
			// same assistant turn. If so, merge them into one assistant
			// message with ToolCalls.
			var toolCallIDs []string
			for i < len(msgs) && msgs[i].Role == "tool_call" {
				tc := msgs[i]
				toolCallIDs = append(toolCallIDs, tc.ToolCallID)
				i++
				// Consume the matching tool result (stored immediately after the tool_call)
				if i < len(msgs) && msgs[i].Role == "tool" {
					i++
				}
			}

			if len(toolCallIDs) > 0 {
				// Build tool calls and tool results
				toolCalls := make([]agent.ToolCall, 0, len(toolCallIDs))
				for _, tcID := range toolCallIDs {
					// Re-find the tool_call message from earlier
					for _, orig := range msgs {
						if orig.ToolCallID == tcID && orig.Role == "tool_call" {
							toolCalls = append(toolCalls, agent.ToolCall{
								ID:   tcID,
								Type: "function",
								Function: agent.FunctionCall{
									Name:      orig.ToolName,
									Arguments: orig.ToolArgs,
								},
							})
							break
						}
					}
				}
				out = append(out, agent.ChatMessage{
					Role:      "assistant",
					Content:   content,
					ToolCalls: toolCalls,
				})
				// Append tool results in the same order
				for _, tcID := range toolCallIDs {
					resultContent := toolResults[tcID]
					name := toolNames[tcID]
					if resultContent == "" {
						resultContent = `{"success":false,"output":"","error":"no result stored"}`
					}
					out = append(out, agent.ChatMessage{
						Role:       "tool",
						ToolCallID: tcID,
						Name:       name,
						Content:    resultContent,
					})
				}
			} else {
				// Plain agent message without tool calls
				out = append(out, agent.ChatMessage{Role: "assistant", Content: content})
			}
			// Loop already advanced past tool_call+tool pairs; continue without i++
			continue

		case "tool":
			// Orphan tool result — shouldn't happen in a well-formed store,
			// but include it anyway (SanitizeToolPairing will handle it)
			out = append(out, agent.ChatMessage{
				Role:       "tool",
				ToolCallID: m.ToolCallID,
				Name:       m.ToolName,
				Content:    m.Content,
			})

		case "tool_call":
			// Orphan tool_call — will be handled by SanitizeToolPairing
			// which backfills a placeholder result
			out = append(out, agent.ChatMessage{
				Role:      "assistant",
				ToolCalls: []agent.ToolCall{{
					ID:   m.ToolCallID,
					Type: "function",
					Function: agent.FunctionCall{
						Name:      m.ToolName,
						Arguments: m.ToolArgs,
					},
				}},
			})
		}
	}

	return out
}

// BuildStoreMessages converts agent ChatMessages to store.Message format,
// interleaving tool_call entries with their corresponding tool results and
// properly populating IsError / ErrorDetail / Content for failures.
func BuildStoreMessages(msgs []agent.ChatMessage) []store.Message {
	// Build a lookup of tool results by ToolCallID so we can
	// interleave them with their tool_call messages.
	toolResults := map[string]store.Message{}
	for _, m := range msgs {
		if m.Name != "" && m.ToolCallID != "" {
			sm := store.Message{
				Role:       "tool",
				ToolName:   m.Name,
				ToolCallID: m.ToolCallID,
				Timestamp:  time.Now(),
			}
			var tr struct {
				Success bool   `json:"success"`
				Output  string `json:"output"`
				Error   string `json:"error"`
			}
			if err := json.Unmarshal([]byte(m.Content), &tr); err == nil {
				if tr.Success {
					sm.Content = tr.Output
				} else {
					sm.IsError = true
					sm.Content = tr.Output
					if sm.Content == "" {
						sm.Content = tr.Error
					}
					sm.ErrorDetail = tr.Error
				}
			} else {
				sm.Content = m.Content
			}
			toolResults[m.ToolCallID] = sm
		}
	}

	var storeMsgs []store.Message
	for _, m := range msgs {
		if m.Role == "system" {
			continue
		}

		// Assistant message with tool calls: store agent content first,
		// then interleave each tool_call with its result so they are
		// adjacent in the stored list.
		if len(m.ToolCalls) > 0 {
			if strings.TrimSpace(m.Content) != "" {
				storeMsgs = append(storeMsgs, store.Message{
					Role:      "agent",
					Content:   m.Content,
					Timestamp: time.Now(),
				})
			}
			for _, tc := range m.ToolCalls {
				// Store the tool_call
				storeMsgs = append(storeMsgs, store.Message{
					Role:       "tool_call",
					ToolName:   tc.Function.Name,
					ToolArgs:   tc.Function.Arguments,
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("%s(%s)", tc.Function.Name, abbreviateArgs(tc.Function.Arguments)),
					Timestamp:  time.Now(),
				})
				// Store the matching tool result immediately after
				if tr, ok := toolResults[tc.ID]; ok {
					storeMsgs = append(storeMsgs, tr)
				}
			}
			continue
		}

		// Tool result — already stored above alongside its tool_call
		if m.Name != "" {
			// Fallback for tool results without a ToolCallID (old format)
			if m.ToolCallID == "" {
				sm := store.Message{
					Role:      "tool",
					ToolName:  m.Name,
					Content:   m.Content,
					Timestamp: time.Now(),
				}
				var tr struct {
					Success bool   `json:"success"`
					Output  string `json:"output"`
					Error   string `json:"error"`
				}
				if err := json.Unmarshal([]byte(m.Content), &tr); err == nil {
					if tr.Success {
						sm.Content = tr.Output
					} else {
						sm.IsError = true
						sm.Content = tr.Output
						if sm.Content == "" {
							sm.Content = tr.Error
						}
						sm.ErrorDetail = tr.Error
					}
				}
				storeMsgs = append(storeMsgs, sm)
			}
			continue
		}

		// Regular user/agent message
		role := m.Role
		if role == "assistant" {
			role = "agent"
		}
		if role == "agent" && strings.TrimSpace(m.Content) == "" {
			continue
		}
		storeMsgs = append(storeMsgs, store.Message{
			Role:      role,
			Content:   m.Content,
			Timestamp: time.Now(),
		})
	}
	return storeMsgs
}

// abbreviateArgs returns a compact display string for tool call arguments.
// e.g. `{"command":"ls -la"}` → `ls -la`
func abbreviateArgs(argsJSON string) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	// Show first meaningful value
	for _, v := range args {
		s := fmt.Sprint(v)
		if len(s) > 60 {
			s = s[:57] + "..."
		}
		return s
	}
	return ""
}
