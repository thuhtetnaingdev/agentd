package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// EnvEntry is a single environment variable.
type EnvEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"` // encrypted at rest
}

// EnvStore manages encrypted environment variables per project.
type EnvStore struct {
	dir        string
	encryptKey string
	mu         sync.RWMutex
}

// NewEnvStore creates an EnvStore rooted at workDir/.agentd/.
func NewEnvStore(workDir, encryptKey string) (*EnvStore, error) {
	dir := filepath.Join(workDir, ".agentd")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create env store dir: %w", err)
	}
	return &EnvStore{dir: dir, encryptKey: encryptKey}, nil
}

func (s *EnvStore) path() string {
	return filepath.Join(s.dir, "env.json")
}

func (s *EnvStore) deriveKey() []byte {
	if s.encryptKey == "" {
		return make([]byte, 32)
	}
	h := sha256.Sum256([]byte(s.encryptKey))
	return h[:]
}

// --- Load / Save ---

// List returns all stored env vars with values decrypted.
func (s *EnvStore) List() ([]EnvEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	for i := range entries {
		entries[i].Value = s.decrypt(entries[i].Value)
	}
	return entries, nil
}

// ListMasked returns all stored env vars with values replaced by a mask.
func (s *EnvStore) ListMasked() ([]EnvEntry, error) {
	entries, err := s.List()
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].Value != "" {
			entries[i].Value = "••••••••"
		}
	}
	return entries, nil
}

// Upsert adds or updates an env var. Value is encrypted before storage.
func (s *EnvStore) Upsert(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.loadLocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	found := false
	for i, e := range entries {
		if e.Key == key {
			entries[i].Value = s.encrypt(value)
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, EnvEntry{
			Key:   key,
			Value: s.encrypt(value),
		})
	}

	return s.saveLocked(entries)
}

// Delete removes an env var by key.
func (s *EnvStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.loadLocked()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for i, e := range entries {
		if e.Key == key {
			entries = append(entries[:i], entries[i+1:]...)
			return s.saveLocked(entries)
		}
	}
	return nil
}

// --- Internal ---

func (s *EnvStore) loadLocked() ([]EnvEntry, error) {
	data, err := os.ReadFile(s.path())
	if err != nil {
		return nil, err
	}
	var entries []EnvEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse env store: %w", err)
	}
	return entries, nil
}

func (s *EnvStore) saveLocked(entries []EnvEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(), data, 0600)
}

// --- Encryption (same AES-GCM as config.go) ---

func (s *EnvStore) encrypt(plain string) string {
	if plain == "" {
		return ""
	}
	key := s.deriveKey()
	block, _ := aes.NewCipher(key)
	aesGCM, _ := cipher.NewGCM(block)
	nonce := make([]byte, aesGCM.NonceSize())
	io.ReadFull(rand.Reader, nonce)
	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(ciphertext)
}

func (s *EnvStore) decrypt(encoded string) string {
	if encoded == "" {
		return ""
	}
	key := s.deriveKey()
	block, _ := aes.NewCipher(key)
	aesGCM, _ := cipher.NewGCM(block)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return ""
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plain, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return ""
	}
	return string(plain)
}
