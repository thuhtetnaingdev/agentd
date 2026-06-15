package deploy

import (
	"fmt"
	"strings"
)

// SetupDocker installs Docker and Docker Compose on the remote VPS.
func SetupDocker(client *SSHClient) (string, error) {
	steps := []struct {
		desc string
		cmd  string
	}{
		{"Check Docker", "which docker && docker --version || echo 'docker not found'"},
		{"Check Docker Compose", "which docker-compose && docker-compose --version || docker compose version 2>/dev/null || echo 'compose not found'"},
	}

	var output strings.Builder

	for _, step := range steps {
		out, err := client.Run(step.cmd)
		if err != nil {
			output.WriteString(fmt.Sprintf("⚠ %s: %v\n%s\n", step.desc, err, out))
		} else {
			output.WriteString(fmt.Sprintf("✓ %s: %s", step.desc, strings.TrimSpace(out)))
		}
	}

	// Install Docker if missing
	if strings.Contains(output.String(), "docker not found") {
		output.WriteString("\n→ Installing Docker...\n")
		installCmd := "curl -fsSL https://get.docker.com | sh"
		out, err := client.Run(installCmd)
		if err != nil {
			return output.String(), fmt.Errorf("docker install failed: %w\n%s", err, out)
		}
		output.WriteString(out)

		// Start and enable Docker
		client.Run("systemctl enable docker && systemctl start docker")
	}

	return output.String(), nil
}

// DockerComposeUp runs docker-compose up for a project.
func DockerComposeUp(client *SSHClient, projectDir, projectName string, envVars map[string]string) (string, error) {
	// Build env file content
	envContent := ""
	for k, v := range envVars {
		envContent += fmt.Sprintf("%s=%s\n", k, v)
	}

	// Write .env on remote
	if envContent != "" {
		writeCmd := fmt.Sprintf("cat > %s/.env << 'ENVEOF'\n%sENVEOF", projectDir, envContent)
		client.Run(writeCmd)
	}

	// Check if compose v2 or v1
	checkCmd := "docker compose version 2>/dev/null && echo 'v2' || echo 'v1'"
	composeVer, _ := client.Run(checkCmd)

	composeCmd := "docker-compose"
	if strings.Contains(composeVer, "v2") {
		composeCmd = "docker compose"
	}

	// Down first to clean up
	client.Run(fmt.Sprintf("cd %s && %s down 2>/dev/null", projectDir, composeCmd))

	// Build and up
	cmd := fmt.Sprintf("cd %s && %s up -d --build", projectDir, composeCmd)
	out, err := client.Run(cmd)
	if err != nil {
		return out, fmt.Errorf("docker-compose up failed: %w\n%s", err, out)
	}

	return out, nil
}
