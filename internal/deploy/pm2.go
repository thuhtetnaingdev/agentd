package deploy

import (
	"fmt"
	"strings"
)

// SetupNode installs the specified Node.js major version on the remote VPS
// using the NodeSource binary distributions repository.
func SetupNode(client *SSHClient, version string) (string, error) {
	var output strings.Builder

	// Check if the requested Node version is already installed
	checkCmd := fmt.Sprintf("node --version 2>/dev/null | grep -q 'v%s' && echo 'installed' || echo 'not installed'", version)
	checkOut, _ := client.Run(checkCmd)

	if strings.Contains(checkOut, "installed") {
		nodeVer, _ := client.Run("node --version 2>/dev/null")
		output.WriteString(fmt.Sprintf("✓ Node.js %s already installed", strings.TrimSpace(nodeVer)))
		return output.String(), nil
	}

	// Install via NodeSource
	output.WriteString(fmt.Sprintf("→ Installing Node.js %s via NodeSource...\n", version))
	installCmd := fmt.Sprintf(
		"curl -fsSL https://deb.nodesource.com/setup_%s.x | bash - && apt-get install -y nodejs",
		version,
	)
	installOut, err := client.Run(installCmd)
	if err != nil {
		return output.String(), fmt.Errorf("node %s install failed: %w\n%s", version, err, installOut)
	}
	output.WriteString(installOut)

	// Verify
	nodeVer, _ := client.Run("node --version 2>/dev/null")
	output.WriteString(fmt.Sprintf("✓ Node.js %s installed", strings.TrimSpace(nodeVer)))

	return output.String(), nil
}

// SetupPM2 installs PM2 globally on the remote VPS.
func SetupPM2(client *SSHClient) (string, error) {
	var output strings.Builder

	// Check Node.js
	nodeOut, err := client.Run("node --version 2>/dev/null || echo 'node not found'")
	if err != nil {
		output.WriteString(fmt.Sprintf("⚠ node check: %v\n", err))
	}
	if strings.Contains(nodeOut, "node not found") {
		return output.String(), fmt.Errorf("Node.js is not installed — run setup_node first")
	}
	output.WriteString(fmt.Sprintf("✓ Node.js: %s\n", strings.TrimSpace(nodeOut)))

	// Check PM2
	pm2Out, _ := client.Run("pm2 --version 2>/dev/null || echo 'pm2 not found'")
	if strings.Contains(pm2Out, "pm2 not found") {
		output.WriteString("→ Installing PM2 globally...\n")
		out, err := client.Run("npm install -g pm2")
		if err != nil {
			return output.String(), fmt.Errorf("pm2 install failed: %w\n%s", err, out)
		}
		output.WriteString(out)
		output.WriteString("✓ PM2 installed\n")
	} else {
		output.WriteString(fmt.Sprintf("✓ PM2: %s\n", strings.TrimSpace(pm2Out)))
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
	checkCmd := fmt.Sprintf("pm2 list 2>/dev/null | grep '%s' || echo 'not_found'", name)
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

// SavePM2Config saves the PM2 process list and ensures PM2 runs as a systemd
// service so it survives reboots and is supervised by the init system.
func SavePM2Config(client *SSHClient) (string, error) {
	var output strings.Builder

	// 1. Save the current PM2 process list
	out, err := client.Run("pm2 save")
	if err != nil {
		return out, fmt.Errorf("pm2 save failed: %w", err)
	}
	output.WriteString(out)

	// 2. Detect the user PM2 is running as
	userOut, _ := client.Run("whoami")
	user := strings.TrimSpace(userOut)
	if user == "" {
		user = "root"
	}

	// 3. Detect PM2 binary path
	pm2PathOut, _ := client.Run("which pm2 2>/dev/null || echo '/usr/bin/pm2'")
	pm2Path := strings.TrimSpace(pm2PathOut)

	// 4. Write systemd service file
	serviceContent := fmt.Sprintf(`[Unit]
Description=PM2 process manager
After=network.target

[Service]
Type=forking
User=%s
LimitNOFILE=65536
ExecStart=%s resurrect
ExecReload=%s reload all
ExecStop=%s kill
Restart=always
RestartSec=5
Environment=PATH=/usr/local/bin:/usr/bin:/bin
Environment=PM2_HOME=%s

[Install]
WantedBy=multi-user.target
`, user, pm2Path, pm2Path, pm2Path, fmt.Sprintf("/home/%s/.pm2", user))

	// Escape for shell heredoc
	writeCmd := fmt.Sprintf("cat > /etc/systemd/system/pm2.service << 'UNITEOF'\n%sUNITEOF", serviceContent)
	out, err = client.Run(writeCmd)
	if err != nil {
		output.WriteString(out)
		return output.String(), fmt.Errorf("write systemd unit failed: %w", err)
	}
	output.WriteString("✓ systemd unit written\n")

	// 5. Reload, enable, and start
	out, err = client.Run("systemctl daemon-reload && systemctl enable pm2 && systemctl restart pm2")
	if err != nil {
		output.WriteString(out)
		return output.String(), fmt.Errorf("systemctl enable failed: %w", err)
	}
	output.WriteString(out)
	output.WriteString("✓ PM2 systemd service enabled and running\n")

	return output.String(), nil
}

// PM2Status returns the status of a PM2 application.
func PM2Status(client *SSHClient, name string) (string, error) {
	cmd := fmt.Sprintf("pm2 show %s 2>&1 | head -20", name)
	return client.Run(cmd)
}
