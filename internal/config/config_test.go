package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSave(t *testing.T) {
	// Use temp dir
	tmp := t.TempDir()
	origDir := ConfigDir
	ConfigDir = tmp
	defer func() { ConfigDir = origDir }()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg == nil {
		t.Fatal("nil config")
	}

	// Verify config file was created
	cfgPath := filepath.Join(tmp, "config.yaml")
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		t.Fatal("config.yaml not created")
	}

	// Update settings
	cfg.UpdateSettings(Settings{APIKey: "sk-test-key"})
	if cfg.Settings().APIKey != "sk-test-key" {
		t.Errorf("expected sk-test-key, got %s", cfg.Settings().APIKey)
	}

	// Add server
	srv, err := cfg.AddServer(ServerConfig{
		Name:     "test-vps",
		Host:     "192.168.1.1",
		Port:     22,
		Username: "root",
		Password: "secret123",
	})
	if err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	if srv.ID == "" {
		t.Fatal("empty server ID")
	}

	// List servers
	servers := cfg.ListServers()
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	if servers[0].Password != "secret123" {
		t.Errorf("password not decrypted: %s", servers[0].Password)
	}

	// Get server
	srv2, ok := cfg.GetServer(srv.ID)
	if !ok {
		t.Fatal("GetServer returned not found")
	}
	if srv2.Host != "192.168.1.1" {
		t.Errorf("wrong host: %s", srv2.Host)
	}

	// Update server
	srv2.Host = "10.0.0.1"
	if err := cfg.UpdateServer(srv2); err != nil {
		t.Fatalf("UpdateServer: %v", err)
	}
	srv3, _ := cfg.GetServer(srv.ID)
	if srv3.Host != "10.0.0.1" {
		t.Errorf("update failed: %s", srv3.Host)
	}

	// Delete server
	if err := cfg.DeleteServer(srv.ID); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}
	if len(cfg.ListServers()) != 0 {
		t.Fatal("server not deleted")
	}

	// Reload from disk
	cfg2, err := Load()
	if err != nil {
		t.Fatalf("re-Load: %v", err)
	}
	if cfg2.Settings().APIKey != "sk-test-key" {
		t.Errorf("settings not persisted: %s", cfg2.Settings().APIKey)
	}
}

func TestEncryptDecrypt(t *testing.T) {
	tmp := t.TempDir()
	origDir := ConfigDir
	ConfigDir = tmp
	defer func() { ConfigDir = origDir }()

	cfg, _ := Load()

	plain := "my-super-secret-password"
	enc := cfg.encrypt(plain)
	if enc == "" || enc == plain {
		t.Fatal("encryption failed")
	}

	dec := cfg.decrypt(enc)
	if dec != plain {
		t.Errorf("decrypt mismatch: got %s", dec)
	}

	// Empty string should stay empty
	if cfg.encrypt("") != "" {
		t.Error("empty should stay empty")
	}
	if cfg.decrypt("") != "" {
		t.Error("empty decrypt should stay empty")
	}
}
