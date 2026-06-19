package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Session represents a saved chat session.
type Session struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SessionWithMessages includes the full message history.
type SessionWithMessages struct {
	Session
	Messages []Message `json:"messages"`
}

// Message is a single chat message.
type Message struct {
	Role        string    `json:"role"`                  // user, agent, tool, tool_call
	Content     string    `json:"content"`
	ToolName    string    `json:"toolName,omitempty"`    // for tool/tool_call messages
	ToolArgs    string    `json:"toolArgs,omitempty"`    // JSON arguments the LLM passed (for tool_call)
	ToolCallID  string    `json:"toolCallId,omitempty"`  // correlates tool_call ↔ tool result
	IsError     bool      `json:"isError,omitempty"`     // true if tool failed
	ErrorDetail string    `json:"errorDetail,omitempty"` // error message when IsError is true
	Timestamp   time.Time `json:"timestamp"`             // when this message was recorded
}

// ---- JSONL line types ----

type jsonlLine struct {
	Type string `json:"type"` // "meta" or "message"
	// meta fields
	ID        string    `json:"id,omitempty"`
	ProjectID string    `json:"projectId,omitempty"`
	Name      string    `json:"name,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
	// message fields
	Role        string    `json:"role,omitempty"`
	Content     string    `json:"content,omitempty"`
	ToolName    string    `json:"toolName,omitempty"`
	ToolArgs    string    `json:"toolArgs,omitempty"`
	ToolCallID  string    `json:"toolCallId,omitempty"`
	IsError     bool      `json:"isError,omitempty"`
	ErrorDetail string    `json:"errorDetail,omitempty"`
	Timestamp   time.Time `json:"timestamp,omitempty"`
}

// extJSONL is the file extension for JSONL session files.
const extJSONL = ".jsonl"

// SessionStore persists chat sessions to disk in JSONL format.
type SessionStore struct {
	baseDir string
}

// NewSessionStore creates a new store.
func NewSessionStore(baseDir string) *SessionStore {
	os.MkdirAll(baseDir, 0700)
	return &SessionStore{baseDir: baseDir}
}

// List returns all sessions for a project, sorted by most recent first.
// Also includes sessions from the "default" directory that have an empty projectId.
func (s *SessionStore) List(projectID string) ([]Session, error) {
	var sessions []Session

	// 1. Load from project-specific directory
	if projectID != "" {
		dir := filepath.Join(s.baseDir, sanitize(projectID))
		sessions = append(sessions, s.listDir(dir)...)
	}

	// 2. Also include orphaned sessions (empty projectId) from "default" dir
	defaultDir := filepath.Join(s.baseDir, "default")
	orphans := s.listDir(defaultDir)
	for _, sess := range orphans {
		if sess.ProjectID == "" {
			sessions = append(sessions, sess)
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	if sessions == nil {
		sessions = []Session{}
	}
	return sessions, nil
}

func (s *SessionStore) listDir(dir string) []Session {
	os.MkdirAll(dir, 0700)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var sessions []Session
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		var id string
		switch {
		case strings.HasSuffix(name, extJSONL):
			id = strings.TrimSuffix(name, extJSONL)
		case strings.HasSuffix(name, ".json"):
			// Backward compat: also pick up legacy `.json` files
			id = strings.TrimSuffix(name, ".json")
		default:
			continue
		}
		sess, err := s.loadMeta(dir, id)
		if err != nil {
			continue
		}
		sessions = append(sessions, sess)
	}
	return sessions
}

// Create starts a new session.
func (s *SessionStore) Create(projectID string) (*SessionWithMessages, error) {
	id := fmt.Sprintf("%d", time.Now().UnixMilli())

	swm := &SessionWithMessages{
		Session: Session{
			ID:        id,
			ProjectID: projectID,
			Name:      "New Chat",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		Messages: []Message{},
	}

	if err := s.save(swm); err != nil {
		return nil, err
	}
	return swm, nil
}

// Get returns a session with its full message history.
// Checks the project-specific directory first, then the default directory.
func (s *SessionStore) Get(projectID, sessionID string) (*SessionWithMessages, error) {
	if projectID != "" {
		dir := filepath.Join(s.baseDir, sanitize(projectID))
		swm, err := s.load(dir, sessionID)
		if err == nil {
			return swm, nil
		}
	}
	// Fallback: check default directory (orphaned sessions)
	defaultDir := filepath.Join(s.baseDir, "default")
	return s.load(defaultDir, sessionID)
}

// SaveMessages saves messages for a session. Auto-names the session from the first user message.
func (s *SessionStore) SaveMessages(projectID, sessionID string, messages []Message) error {
	dir := filepath.Join(s.baseDir, sanitize(projectID))
	os.MkdirAll(dir, 0700)

	existing, err := s.load(dir, sessionID)
	if err != nil {
		existing = &SessionWithMessages{
			Session: Session{
				ID:        sessionID,
				ProjectID: projectID,
				Name:      "New Chat",
				CreatedAt: time.Now(),
			},
		}
	}

	existing.Messages = messages
	existing.UpdatedAt = time.Now()

	// Auto-name from first user message
	if existing.Name == "New Chat" {
		for _, m := range messages {
			if m.Role == "user" {
				name := m.Content
				if len(name) > 50 {
					name = name[:47] + "..."
				}
				existing.Name = name
				break
			}
		}
	}

	return s.save(existing)
}

// Delete removes a session. Tries project dir first, then default.
func (s *SessionStore) Delete(projectID, sessionID string) error {
	// Try JSONL first, then legacy JSON
	path := filepath.Join(s.baseDir, sanitize(projectID), sessionID+extJSONL)
	err := os.Remove(path)
	if err == nil {
		return nil
	}
	// Try legacy .json path
	path = filepath.Join(s.baseDir, sanitize(projectID), sessionID+".json")
	err = os.Remove(path)
	if err == nil {
		return nil
	}
	// Try default directory
	defaultPath := filepath.Join(s.baseDir, "default", sessionID+extJSONL)
	err = os.Remove(defaultPath)
	if err == nil {
		return nil
	}
	defaultPath = filepath.Join(s.baseDir, "default", sessionID+".json")
	return os.Remove(defaultPath)
}

// --- JSONL I/O ---

// sessionPath returns the JSONL path for a session. It prefers the new .jsonl
// extension; if only a legacy .json exists, it returns that path instead so
// the caller can attempt migration.
func (s *SessionStore) sessionPath(dir, id string) (jsonlPath, legacyPath string) {
	jsonlPath = filepath.Join(dir, id+extJSONL)
	legacyPath = filepath.Join(dir, id+".json")
	return
}

// load reads a session from JSONL (preferred) or falls back to legacy JSON.
func (s *SessionStore) load(dir, id string) (*SessionWithMessages, error) {
	jsonlPath, legacyPath := s.sessionPath(dir, id)

	// Try JSONL first
	if _, err := os.Stat(jsonlPath); err == nil {
		return s.loadJSONL(jsonlPath)
	}

	// Fallback: legacy JSON
	if _, err := os.Stat(legacyPath); err == nil {
		return s.loadLegacyJSON(legacyPath)
	}

	return nil, fmt.Errorf("session %s not found in %s", id, dir)
}

// loadMeta reads only the metadata line from a JSONL file, or falls back to
// loading the full legacy JSON and returning just the Session portion.
func (s *SessionStore) loadMeta(dir, id string) (Session, error) {
	jsonlPath, legacyPath := s.sessionPath(dir, id)

	// Try JSONL — fast path: read just the first line
	if _, err := os.Stat(jsonlPath); err == nil {
		return s.loadMetaJSONL(jsonlPath)
	}

	// Fallback: legacy JSON
	if _, err := os.Stat(legacyPath); err == nil {
		swm, err := s.loadLegacyJSON(legacyPath)
		if err != nil {
			return Session{}, err
		}
		return swm.Session, nil
	}

	return Session{}, fmt.Errorf("session %s not found in %s", id, dir)
}

// loadJSONL reads a full JSONL session file.
func (s *SessionStore) loadJSONL(path string) (*SessionWithMessages, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	swm := &SessionWithMessages{
		Messages: []Message{},
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var jl jsonlLine
		if err := json.Unmarshal([]byte(line), &jl); err != nil {
			continue // skip corrupt lines
		}

		switch jl.Type {
		case "meta":
			swm.ID = jl.ID
			swm.ProjectID = jl.ProjectID
			swm.Name = jl.Name
			swm.CreatedAt = jl.CreatedAt
			swm.UpdatedAt = jl.UpdatedAt
		case "message":
			swm.Messages = append(swm.Messages, Message{
				Role:        jl.Role,
				Content:     jl.Content,
				ToolName:    jl.ToolName,
				ToolArgs:    jl.ToolArgs,
				ToolCallID:  jl.ToolCallID,
				IsError:     jl.IsError,
				ErrorDetail: jl.ErrorDetail,
				Timestamp:   jl.Timestamp,
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return swm, nil
}

// loadMetaJSONL reads only the first (meta) line from a JSONL file.
func (s *SessionStore) loadMetaJSONL(path string) (Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return Session{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return Session{}, fmt.Errorf("empty session file: %s", path)
	}

	line := strings.TrimSpace(scanner.Text())
	var jl jsonlLine
	if err := json.Unmarshal([]byte(line), &jl); err != nil {
		return Session{}, fmt.Errorf("parse meta line: %w", err)
	}

	if jl.Type != "meta" {
		return Session{}, fmt.Errorf("expected meta line, got type=%s", jl.Type)
	}

	return Session{
		ID:        jl.ID,
		ProjectID: jl.ProjectID,
		Name:      jl.Name,
		CreatedAt: jl.CreatedAt,
		UpdatedAt: jl.UpdatedAt,
	}, nil
}

// save writes a session as JSONL (meta line + one line per message).
func (s *SessionStore) save(swm *SessionWithMessages) error {
	dir := filepath.Join(s.baseDir, sanitize(swm.ProjectID))
	os.MkdirAll(dir, 0700)

	path := filepath.Join(dir, swm.ID+extJSONL)

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create session file: %w", err)
	}
	defer f.Close()

	// Write meta line
	meta := jsonlLine{
		Type:      "meta",
		ID:        swm.ID,
		ProjectID: swm.ProjectID,
		Name:      swm.Name,
		CreatedAt: swm.CreatedAt,
		UpdatedAt: swm.UpdatedAt,
	}
	line, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	if _, err := fmt.Fprintln(f, string(line)); err != nil {
		return err
	}

	// Write one line per message
	enc := json.NewEncoder(f)
	for _, msg := range swm.Messages {
		ml := jsonlLine{
			Type:        "message",
			Role:        msg.Role,
			Content:     msg.Content,
			ToolName:    msg.ToolName,
			ToolArgs:    msg.ToolArgs,
			ToolCallID:  msg.ToolCallID,
			IsError:     msg.IsError,
			ErrorDetail: msg.ErrorDetail,
			Timestamp:   msg.Timestamp,
		}
		if err := enc.Encode(ml); err != nil {
			return fmt.Errorf("encode message: %w", err)
		}
	}

	return nil
}

// --- Legacy JSON support (backward compatibility) ---

// loadLegacyJSON reads a session from the old .json format.
func (s *SessionStore) loadLegacyJSON(path string) (*SessionWithMessages, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var swm SessionWithMessages
	if err := json.Unmarshal(data, &swm); err != nil {
		return nil, err
	}
	return &swm, nil
}

// MigrateLegacy converts all .json session files under baseDir to .jsonl
// and removes the original .json file after a successful conversion.
func (s *SessionStore) MigrateLegacy() error {
	return filepath.Walk(s.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}

		// Skip if there's already a .jsonl for this session
		jsonlPath := strings.TrimSuffix(path, ".json") + extJSONL
		if _, err := os.Stat(jsonlPath); err == nil {
			// Already migrated — remove the legacy file
			os.Remove(path)
			return nil
		}

		// Load legacy JSON
		swm, err := s.loadLegacyJSON(path)
		if err != nil {
			return nil // skip corrupt files
		}

		// Save as JSONL
		if err := s.save(swm); err != nil {
			return nil // skip if save fails
		}

		// Remove legacy file on success
		os.Remove(path)
		return nil
	})
}

func sanitize(s string) string {
	// Replace path separators and other dangerous chars
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, "..", "_")
	if s == "" {
		s = "default"
	}
	return s
}
