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

	"gopkg.in/yaml.v3"
)

// ConfigDir is the directory under which agentd stores configuration.
var ConfigDir = filepath.Join(os.Getenv("HOME"), ".agentd")

// Settings holds global application settings.
type Settings struct {
	APIKey     string `json:"apiKey" yaml:"api_key"`
	APIBaseURL string `json:"apiBaseUrl" yaml:"api_base_url"`
	Model      string `json:"model" yaml:"model"`
	EncryptKey string `json:"-" yaml:"encrypt_key"`
}

// Defaults for convenience
const (
	DefaultBaseURL = "https://api.openai.com/v1"
	DefaultModel   = "gpt-4o"
)

// ServerConfig holds VPS SSH credentials.
type ServerConfig struct {
	ID       string `json:"id" yaml:"id"`
	Name     string `json:"name" yaml:"name"`
	Host     string `json:"host" yaml:"host"`
	Port     int    `json:"port" yaml:"port"`
	Username string `json:"username" yaml:"username"`
	Password string `json:"password,omitempty" yaml:"password,omitempty"` // encrypted at rest
}

// Config is the root configuration.
type Config struct {
	path     string
	settings Settings       `yaml:"settings"`
	Servers  []ServerConfig `yaml:"servers"`
	mu       sync.RWMutex
}

// Load reads config from ~/.agentd/config.yaml.
func Load() (*Config, error) {
	if err := os.MkdirAll(ConfigDir, 0700); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	cfgPath := filepath.Join(ConfigDir, "config.yaml")
	cfg := &Config{path: cfgPath}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return defaults
			return cfg, cfg.Save()
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}

// Save writes the config to disk.
func (c *Config) Save() error {
	c.mu.RLock()
	data, err := yaml.Marshal(c)
	c.mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0600)
}

// Settings returns a copy of the current settings.
func (c *Config) Settings() Settings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.settings
}

// MarshalYAML implements custom YAML serialization for Config.
// Callers must hold c.mu (read or write lock).
func (c *Config) MarshalYAML() (interface{}, error) {
	return &configWire{
		Settings: c.settings,
		Servers:  c.Servers,
	}, nil
}

// UnmarshalYAML implements custom YAML deserialization for Config.
func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	var wire configWire
	if err := value.Decode(&wire); err != nil {
		return err
	}
	c.settings = wire.Settings
	c.Servers = wire.Servers
	return nil
}

type configWire struct {
	Settings Settings       `yaml:"settings"`
	Servers  []ServerConfig `yaml:"servers"`
}

// UpdateSettings replaces settings and saves.
func (c *Config) UpdateSettings(s Settings) {
	c.mu.Lock()
	if s.EncryptKey == "" {
		s.EncryptKey = c.settings.EncryptKey
	}
	// Auto-generate encryption key on first save
	if s.EncryptKey == "" {
		s.EncryptKey = generateEncryptKey()
	}
	c.settings = s
	c.mu.Unlock()
	c.Save()
}

// ListServers returns servers (passwords decrypted).
func (c *Config) ListServers() []ServerConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]ServerConfig, len(c.Servers))
	copy(result, c.Servers)
	for i := range result {
		result[i].Password = c.decrypt(result[i].Password)
	}
	return result
}

// GetServer returns a single server by ID.
func (c *Config) GetServer(id string) (ServerConfig, bool) {
	for _, s := range c.ListServers() {
		if s.ID == id {
			return s, true
		}
	}
	return ServerConfig{}, false
}

// AddServer creates a new server entry.
func (c *Config) AddServer(srv ServerConfig) (ServerConfig, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	srv.ID = fmt.Sprintf("srv_%d", len(c.Servers)+1)
	srv.Password = c.encrypt(srv.Password)
	c.Servers = append(c.Servers, srv)

	if err := c.saveLocked(); err != nil {
		return ServerConfig{}, err
	}

	srv.Password = c.decrypt(srv.Password)
	return srv, nil
}

// UpdateServer replaces an existing server.
func (c *Config) UpdateServer(srv ServerConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, s := range c.Servers {
		if s.ID == srv.ID {
			if srv.Password != "" {
				srv.Password = c.encrypt(srv.Password)
			} else {
				srv.Password = s.Password
			}
			c.Servers[i] = srv
			return c.saveLocked()
		}
	}
	return fmt.Errorf("server %s not found", srv.ID)
}

// DeleteServer removes a server.
func (c *Config) DeleteServer(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, s := range c.Servers {
		if s.ID == id {
			c.Servers = append(c.Servers[:i], c.Servers[i+1:]...)
			return c.saveLocked()
		}
	}
	return fmt.Errorf("server %s not found", id)
}

func (c *Config) saveLocked() error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0600)
}

// --- Encryption helpers ---

func (c *Config) key() []byte {
	if c.settings.EncryptKey == "" {
		return make([]byte, 32)
	}
	h := sha256.Sum256([]byte(c.settings.EncryptKey))
	return h[:]
}

func (c *Config) encrypt(plain string) string {
	if plain == "" {
		return ""
	}
	block, _ := aes.NewCipher(c.key())
	aesGCM, _ := cipher.NewGCM(block)
	nonce := make([]byte, aesGCM.NonceSize())
	io.ReadFull(rand.Reader, nonce)
	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(ciphertext)
}

func (c *Config) decrypt(encoded string) string {
	if encoded == "" {
		return ""
	}
	block, _ := aes.NewCipher(c.key())
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

func generateEncryptKey() string {
	b := make([]byte, 32)
	io.ReadFull(rand.Reader, b)
	return base64.StdEncoding.EncodeToString(b)
}

// Sanitized returns server list with passwords masked (for UI display).
func (c *Config) SanitizedServers() []ServerConfig {
	servers := c.ListServers()
	for i := range servers {
		if servers[i].Password != "" {
			servers[i].Password = "••••••••"
		}
	}
	return servers
}

// JSON helpers for API responses
func (c *Config) MarshalJSON() ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return json.Marshal(c)
}
