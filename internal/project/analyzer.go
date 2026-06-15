package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Project represents a detected project directory.
type Project struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Path           string            `json:"path"`
	Type           string            `json:"type"`       // node, python, go, rust, static, unknown
	Frameworks     []string          `json:"frameworks"` // nextjs, react, express, django, etc.
	HasDocker      bool              `json:"hasDocker"`
	EnvFiles       []string          `json:"envFiles"`
	Ports          []int             `json:"ports"`
	BuildCmd       string            `json:"buildCmd"`
	StartCmd       string            `json:"startCmd"`
	PM2Config      string            `json:"pm2Config,omitempty"`
	PackageManager string            `json:"packageManager,omitempty"` // npm, pnpm, yarn
}

// Framework patterns to detect.
var frameworkIndicators = map[string][]string{
	"nextjs":     {"next.config.js", "next.config.mjs", "next.config.ts"},
	"react":      {"src/App.tsx", "src/App.jsx", "public/index.html"},
	"vue":        {"vue.config.js", "src/App.vue"},
	"express":    {"app.js", "server.js"},
	"fastapi":    {"main.py"},
	"django":     {"manage.py"},
	"flask":      {"app.py"},
	"gin":        {"main.go"},
	"fiber":      {"main.go"},
	"laravel":    {"artisan"},
	"static":     {"index.html"},
}

// Scan returns the root project + any subdirectory projects under the given root.
// The root directory itself is always the first project (name = basename of root).
func Scan(root string) ([]Project, error) {
	projects := make([]Project, 0)

	// 1. Analyze the root directory itself
	rootName := filepath.Base(root)
	if rootName == "." || rootName == "/" {
		abs, err := filepath.Abs(root)
		if err == nil {
			rootName = filepath.Base(abs)
		}
	}
	if rootName == "" || rootName == "." || rootName == "/" {
		rootName = "project"
	}

	rootProj, err := AnalyzeDir(root, rootName)
	if err == nil {
		projects = append(projects, rootProj)
	}

	// 2. Scan subdirectories for additional projects
	entries, err := os.ReadDir(root)
	if err != nil {
		// If we can't read subdirs, at least return the root project
		return projects, nil
	}

	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.Name() == "node_modules" || e.Name() == "vendor" {
			continue
		}

		p, err := Analyze(root, e.Name())
		if err != nil {
			continue
		}
		// Only add subdirs that are actual projects (not already covered by root)
		if p.Type != "unknown" || p.HasDocker || len(p.Frameworks) > 0 {
			projects = append(projects, p)
		}
	}

	return projects, nil
}

// AnalyzeDir analyzes a directory given its absolute path and display name.
func AnalyzeDir(path, name string) (Project, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Project{}, err
	}
	if !info.IsDir() {
		return Project{}, fmt.Errorf("%s is not a directory", name)
	}

	p := Project{
		ID:         name,
		Name:       name,
		Path:       path,
		Type:       "unknown",
		Frameworks: []string{},
		EnvFiles:   []string{},
		Ports:      []int{},
	}

	detectProjectType(path, &p)
	return p, nil
}

// Analyze deeply inspects a project directory (name is a subdirectory of root).
func Analyze(root, name string) (Project, error) {
	dir := filepath.Join(root, name)
	return AnalyzeDir(dir, name)
}

func detectProjectType(dir string, p *Project) {
	// Detect package.json
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
		p.Type = "node"
	}

	// Detect Python
	if _, err := os.Stat(filepath.Join(dir, "requirements.txt")); err == nil {
		if p.Type == "unknown" {
			p.Type = "python"
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "pyproject.toml")); err == nil {
		if p.Type == "unknown" {
			p.Type = "python"
		}
	}

	// Detect Go
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		p.Type = "go"
	}

	// Detect Docker
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil {
		p.HasDocker = true
	}
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err == nil {
		p.HasDocker = true
	}
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yaml")); err == nil {
		p.HasDocker = true
	}

	// Detect frameworks
	for fw, indicators := range frameworkIndicators {
		for _, ind := range indicators {
			if _, err := os.Stat(filepath.Join(dir, ind)); err == nil {
				p.Frameworks = append(p.Frameworks, fw)
				break
			}
		}
	}

	// Detect env files
	envPatterns := []string{".env", ".env.local", ".env.production", ".env.development"}
	for _, pat := range envPatterns {
		if _, err := os.Stat(filepath.Join(dir, pat)); err == nil {
			p.EnvFiles = append(p.EnvFiles, pat)
		}
	}

	// Determine build and start commands
	p.BuildCmd = inferBuildCmd(*p)
	p.StartCmd = inferStartCmd(*p)

	// Detect package manager from lock files
	if _, err := os.Stat(filepath.Join(dir, "pnpm-lock.yaml")); err == nil {
		p.PackageManager = "pnpm"
	} else if _, err := os.Stat(filepath.Join(dir, "yarn.lock")); err == nil {
		p.PackageManager = "yarn"
	} else if _, err := os.Stat(filepath.Join(dir, "package-lock.json")); err == nil {
		p.PackageManager = "npm"
	} else if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
		p.PackageManager = "npm" // default for Node.js projects
	}

	// Detect PM2 config
	for _, f := range []string{"ecosystem.config.js", "ecosystem.config.cjs", "pm2.config.js"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			p.PM2Config = f
			break
		}
	}
}

func inferBuildCmd(p Project) string {
	pm := p.PackageManager
	if pm == "" {
		pm = "npm"
	}
	for _, fw := range p.Frameworks {
		switch fw {
		case "nextjs":
			return pm + " run build"
		case "react":
			return pm + " run build"
		case "vue":
			return pm + " run build"
		}
	}
	if p.Type == "node" {
		return pm + " run build"
	}
	if p.Type == "go" {
		return "go build -o app ./..."
	}
	return ""
}

func inferStartCmd(p Project) string {
	pm := p.PackageManager
	if pm == "" {
		pm = "npm"
	}
	for _, fw := range p.Frameworks {
		switch fw {
		case "nextjs":
			return pm + " start"
		case "react":
			return "npx serve -s build -l 3000"
		}
	}
	if p.Type == "node" {
		return pm + " start"
	}
	if p.Type == "go" {
		return "./app"
	}
	if p.Type == "python" {
		return "python main.py"
	}
	return ""
}
