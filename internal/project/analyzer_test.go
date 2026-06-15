package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeNodeProject(t *testing.T) {
	root := t.TempDir()
	projDir := filepath.Join(root, "test-node-app")
	os.MkdirAll(projDir, 0755)

	os.WriteFile(filepath.Join(projDir, "package.json"), []byte(`{"name":"test-app","scripts":{"build":"next build","start":"next start"}}`), 0644)
	os.WriteFile(filepath.Join(projDir, "next.config.js"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(projDir, ".env"), []byte("FOO=bar"), 0644)
	os.WriteFile(filepath.Join(projDir, "Dockerfile"), []byte("FROM node"), 0644)

	p, err := Analyze(root, "test-node-app")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if p.Type != "node" {
		t.Errorf("expected type node, got %s", p.Type)
	}
	if !p.HasDocker {
		t.Error("expected HasDocker=true")
	}
	if len(p.EnvFiles) != 1 || p.EnvFiles[0] != ".env" {
		t.Errorf("expected [.env], got %v", p.EnvFiles)
	}

	hasNextJS := false
	for _, fw := range p.Frameworks {
		if fw == "nextjs" {
			hasNextJS = true
		}
	}
	if !hasNextJS {
		t.Errorf("expected nextjs framework, got %v", p.Frameworks)
	}
}

func TestAnalyzeGoProject(t *testing.T) {
	root := t.TempDir()
	projDir := filepath.Join(root, "test-go-app")
	os.MkdirAll(projDir, 0755)

	os.WriteFile(filepath.Join(projDir, "go.mod"), []byte("module test"), 0644)
	os.WriteFile(filepath.Join(projDir, "main.go"), []byte("package main"), 0644)

	p, err := Analyze(root, "test-go-app")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if p.Type != "go" {
		t.Errorf("expected type go, got %s", p.Type)
	}
	if p.BuildCmd != "go build -o app ./..." {
		t.Errorf("expected go build cmd, got %s", p.BuildCmd)
	}
}

func TestScan(t *testing.T) {
	root := t.TempDir()

	os.MkdirAll(filepath.Join(root, "frontend"), 0755)
	os.MkdirAll(filepath.Join(root, "backend"), 0755)
	os.WriteFile(filepath.Join(root, "frontend", "package.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(root, "backend", "go.mod"), []byte("module backend"), 0644)

	projects, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	// Scan now includes root directory as first project
	if len(projects) < 2 {
		t.Fatalf("expected at least 2 projects (root + subdirs), got %d", len(projects))
	}

	foundBackend := false
	foundFrontend := false
	for _, p := range projects {
		if p.Name == "backend" {
			foundBackend = true
		}
		if p.Name == "frontend" {
			foundFrontend = true
		}
	}
	if !foundBackend || !foundFrontend {
		t.Errorf("missing sub-projects. got: ")
		for _, p := range projects {
			t.Logf("  - %s (type=%s)", p.Name, p.Type)
		}
	}
}

func TestAnalyzeUnknown(t *testing.T) {
	root := t.TempDir()
	projDir := filepath.Join(root, "unknown-project")
	os.MkdirAll(projDir, 0755)
	os.WriteFile(filepath.Join(projDir, "README.md"), []byte("# hello"), 0644)

	p, err := Analyze(root, "unknown-project")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if p.Type != "unknown" {
		t.Errorf("expected unknown, got %s", p.Type)
	}
}
