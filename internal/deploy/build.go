package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BuildResult holds the result of a local build.
type BuildResult struct {
	Success    bool
	Output     string
	OutputDir  string
	Command    string
}

// BuildProject runs the build command for a project locally.
func BuildProject(projectPath, buildCmd string, envVars map[string]string) (*BuildResult, error) {
	if buildCmd == "" {
		return &BuildResult{Success: true, Output: "No build command; skipping build.", OutputDir: projectPath}, nil
	}

	// Parse the build command
	parts := strings.Fields(buildCmd)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty build command")
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = projectPath
	cmd.Env = os.Environ()

	for k, v := range envVars {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	output, err := cmd.CombinedOutput()
	result := &BuildResult{
		Success: err == nil,
		Output:  string(output),
		Command: buildCmd,
	}

	if err != nil {
		return result, fmt.Errorf("build failed: %w\n%s", err, string(output))
	}

	// Determine output directory
	result.OutputDir = detectOutputDir(projectPath)

	return result, nil
}

// InstallDeps installs project dependencies locally.
// For Node.js projects, packageManager should be "npm", "pnpm", or "yarn" (detected from lock files).
func InstallDeps(projectPath, projectType, packageManager string) (*BuildResult, error) {
	var cmd *exec.Cmd

	switch projectType {
	case "node":
		pm := packageManager
		if pm == "" {
			pm = "npm"
		}
		switch pm {
		case "pnpm":
			cmd = exec.Command("pnpm", "install")
		case "yarn":
			cmd = exec.Command("yarn", "install")
		default:
			cmd = exec.Command("npm", "install")
		}
	case "go":
		cmd = exec.Command("go", "mod", "download")
	case "python":
		cmd = exec.Command("pip", "install", "-r", "requirements.txt")
	default:
		return &BuildResult{Success: true, Output: "No dependency manager detected."}, nil
	}

	cmd.Dir = projectPath
	output, err := cmd.CombinedOutput()

	if err != nil {
		return &BuildResult{
			Success: false,
			Output:  string(output),
			Command: cmd.String(),
		}, fmt.Errorf("install deps failed: %w\n%s", err, string(output))
	}

	return &BuildResult{
		Success: true,
		Output:  string(output),
		Command: cmd.String(),
	}, nil
}

func detectOutputDir(projectPath string) string {
	// Common build output directories
	candidates := []string{"dist", "build", "out", ".next", "public"}
	for _, c := range candidates {
		if info, err := os.Stat(filepath.Join(projectPath, c)); err == nil && info.IsDir() {
			return filepath.Join(projectPath, c)
		}
	}
	// For Go projects
	if info, err := os.Stat(filepath.Join(projectPath, "main.go")); err == nil && !info.IsDir() {
		return projectPath // Go binary is in the project root
	}
	return projectPath
}
