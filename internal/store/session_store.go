package store

import (
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
	Role      string `json:"role"`               // user, agent, tool
	Content   string `json:"content"`
	ToolName  string `json:"toolName,omitempty"`  // for tool messages
	IsError   bool   `json:"isError,omitempty"`   // true if tool failed
}

// SessionStore persists chat sessions to disk.
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
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
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
	path := filepath.Join(s.baseDir, sanitize(projectID), sessionID+".json")
	err := os.Remove(path)
	if err == nil {
		return nil
	}
	// Try default directory
	defaultPath := filepath.Join(s.baseDir, "default", sessionID+".json")
	return os.Remove(defaultPath)
}

// --- internal helpers ---

func (s *SessionStore) load(dir, id string) (*SessionWithMessages, error) {
	path := filepath.Join(dir, id+".json")
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

func (s *SessionStore) loadMeta(dir, id string) (Session, error) {
	swm, err := s.load(dir, id)
	if err != nil {
		return Session{}, err
	}
	return swm.Session, nil
}

func (s *SessionStore) save(swm *SessionWithMessages) error {
	dir := filepath.Join(s.baseDir, sanitize(swm.ProjectID))
	os.MkdirAll(dir, 0700)

	path := filepath.Join(dir, swm.ID+".json")
	data, err := json.MarshalIndent(swm, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
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
