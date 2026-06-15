package deploy

import (
	"fmt"
	"strings"
)

// SetupPM2 installs Node.js and PM2 on the remote VPS.
func SetupPM2(client *SSHClient) (string, error) {
	steps := []struct {
		desc string
		cmd  string
	}{
		{"Check Node.js", "which node && node --version || echo 'node not found'"},
		{"Check npm", "which npm && npm --version || echo 'npm not found'"},
		{"Check PM2", "which pm2 && pm2 --version || echo 'pm2 not found'"},
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

	// Install Node.js if missing
	if strings.Contains(output.String(), "node not found") {
		output.WriteString("\n→ Installing Node.js...\n")
		installCmd := "curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && apt-get install -y nodejs"
		out, err := client.Run(installCmd)
		if err != nil {
			return output.String(), fmt.Errorf("node install failed: %w\n%s", err, out)
		}
		output.WriteString(out)
	}

	// Install PM2 globally if missing
	if strings.Contains(output.String(), "pm2 not found") {
		output.WriteString("\n→ Installing PM2...\n")
		out, err := client.Run("npm install -g pm2")
		if err != nil {
			return output.String(), fmt.Errorf("pm2 install failed: %w\n%s", err, out)
		}
		output.WriteString(out)
	}

	return output.String(), nil
}

// StartPM2App starts (or restarts) an application with PM2.
// It writes a .env file to the project directory on the remote server
// so that applications using dotenv-style libraries can read it at runtime,
// in addition to passing them as inline --env flags to PM2.
func StartPM2App(client *SSHClient, name, dir, startCmd string, envVars map[string]string) (string, error) {
	// Build env args
	envArgs := ""
	for k, v := range envVars {
		envArgs += fmt.Sprintf(" --env %s='%s'", k, v)
	}

	// Write .env file on the remote server (dotenv-compatible)
	if len(envVars) > 0 {
		envContent := ""
		for k, v := range envVars {
			envContent += fmt.Sprintf("%s=%s\n", k, v)
		}
		writeCmd := fmt.Sprintf("cat > %s/.env << 'ENVEOF'\n%sENVEOF", dir, envContent)
		if _, err := client.Run(writeCmd); err != nil {
			return "", fmt.Errorf("write .env on remote: %w", err)
		}
	}

	// Check if app already exists
	checkCmd := fmt.Sprintf("pm2 list | grep '%s' || echo 'not_found'", name)
	checkOut, _ := client.Run(checkCmd)

	if strings.Contains(checkOut, "not_found") {
		// Start new
		cmd := fmt.Sprintf("cd %s && pm2 start %s --name %s%s", dir, startCmd, name, envArgs)
		out, err := client.Run(cmd)
		if err != nil {
			return out, fmt.Errorf("pm2 start failed: %w\n%s", err, out)
		}
		return out, nil
	}

	// Restart existing
	cmd := fmt.Sprintf("cd %s && pm2 restart %s%s", dir, name, envArgs)
	out, err := client.Run(cmd)
	if err != nil {
		return out, fmt.Errorf("pm2 restart failed: %w\n%s", err, out)
	}

	return out, nil
}

// SavePM2Config saves the PM2 process list for resurrection on reboot.
func SavePM2Config(client *SSHClient) (string, error) {
	out, err := client.Run("pm2 save && pm2 startup systemd -u root --hp /root")
	if err != nil {
		return out, fmt.Errorf("pm2 save failed: %w", err)
	}
	return out, nil
}

// PM2Status returns the status of a PM2 application.
func PM2Status(client *SSHClient, name string) (string, error) {
	cmd := fmt.Sprintf("pm2 show %s 2>&1 | head -20", name)
	return client.Run(cmd)
}
