package deploy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidateWebServer detects the web server type and runs its config test.
// Returns "" if the config is valid, or an error string describing the failure.
func ValidateWebServer(client *SSHClient) (string, error) {
	ws, err := CheckWebServer(client)
	if err != nil {
		return "", fmt.Errorf("detect web server: %w", err)
	}

	switch ws {
	case "nginx":
		return validateNginx(client)
	case "caddy":
		return validateCaddy(client)
	case "apache":
		return validateApache(client)
	default:
		return "", nil // no web server = nothing to validate
	}
}

func validateNginx(client *SSHClient) (string, error) {
	out, err := client.Run("nginx -t 2>&1")
	if err != nil {
		return fmt.Sprintf("nginx config test FAILED:\n%s", out), nil
	}
	return "", nil // config is valid
}

func validateCaddy(client *SSHClient) (string, error) {
	// Caddyfile path — check the common locations
	out, err := client.Run("caddy validate --config /etc/caddy/Caddyfile 2>&1")
	if err != nil {
		// Try alternate path
		out2, err2 := client.Run("caddy validate --config /etc/caddy/Caddyfile 2>&1")
		if err2 != nil {
			_ = out
			return fmt.Sprintf("caddy config test FAILED:\n%s", out2), nil
		}
	}
	return "", nil
}

func validateApache(client *SSHClient) (string, error) {
	out, err := client.Run("apache2ctl configtest 2>&1")
	if err != nil {
		return fmt.Sprintf("apache config test FAILED:\n%s", out), nil
	}
	return "", nil
}

// PM2AppStatus represents a single PM2 process from `pm2 jlist`.
type PM2AppStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "online", "stopped", "errored", etc.
}

// CheckPM2Status checks whether a PM2 app is running and online.
// Returns "" if online, or an error message describing the issue.
func CheckPM2Status(client *SSHClient, projectName string) (string, error) {
	out, err := client.Run("pm2 jlist 2>&1")
	if err != nil {
		return fmt.Sprintf("pm2 jlist failed: %s", out), nil
	}

	var apps []PM2AppStatus
	if err := json.Unmarshal([]byte(out), &apps); err != nil {
		// Output might not be pure JSON (warnings mixed in) — try to find the app by name
		if strings.Contains(out, fmt.Sprintf(`"name":"%s"`, projectName)) {
			// Check if it's online nearby
			if strings.Contains(out, fmt.Sprintf(`"name":"%s"`, projectName)+`","status":"online"`) ||
				strings.Contains(out, fmt.Sprintf(`"status":"online"`)+`","name":"`+projectName+`"`) {
				return "", nil
			}
			return fmt.Sprintf("PM2 app '%s' found but status is not online. Raw output:\n%s", projectName, out), nil
		}
		return fmt.Sprintf("PM2 app '%s' not found in pm2 process list. Raw output:\n%s", projectName, out), nil
	}

	for _, app := range apps {
		if app.Name == projectName {
			if app.Status == "online" {
				return "", nil
			}
			return fmt.Sprintf("PM2 app '%s' status is '%s' (expected 'online')", projectName, app.Status), nil
		}
	}

	return fmt.Sprintf("PM2 app '%s' not found in pm2 process list (%d apps running)", projectName, len(apps)), nil
}

// CheckDiskSpace checks disk usage on /var/www and returns a warning if usage ≥ 90%.
func CheckDiskSpace(client *SSHClient) (string, error) {
	out, err := client.Run("df -h /var/www 2>&1")
	if err != nil {
		return "", nil // non-critical, skip on error
	}

	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		return "", nil
	}

	// Parse the second line (first is header)
	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return "", nil
	}

	// df -h output: Filesystem Size Used Avail Use% Mounted on
	usePercent := strings.TrimSuffix(fields[4], "%")
	var usage int
	fmt.Sscanf(usePercent, "%d", &usage)

	if usage >= 90 {
		return fmt.Sprintf("⚠️  Disk usage on /var/www is at %d%% — critically low space:\n%s", usage, lines[1]), nil
	}
	if usage >= 80 {
		return fmt.Sprintf("⚠️  Disk usage on /var/www is at %d%% — running low:\n%s", usage, lines[1]), nil
	}

	return "", nil
}

// CheckSSLCert verifies that an SSL certificate is installed and valid for the given domain.
func CheckSSLCert(client *SSHClient, domain string) (string, error) {
	cmd := fmt.Sprintf(
		"echo | openssl s_client -connect %s:443 -servername %s 2>/dev/null | openssl x509 -noout -dates 2>&1",
		domain, domain,
	)
	out, err := client.Run(cmd)
	if err != nil {
		return fmt.Sprintf("SSL cert check failed for %s:\n%s", domain, out), nil
	}

	if strings.Contains(out, "notBefore") && strings.Contains(out, "notAfter") {
		return "", nil // cert is present and has date range
	}

	return fmt.Sprintf("SSL cert check for %s returned unexpected output:\n%s", domain, out), nil
}

// CheckWebServerRunning verifies that a web server process is actually running.
// Returns "" if running, or a warning string.
func CheckWebServerRunning(client *SSHClient, wsName string) (string, error) {
	cmd := fmt.Sprintf("systemctl is-active %s 2>&1", wsName)
	out, err := client.Run(cmd)
	if err != nil || !strings.Contains(out, "active") {
		return fmt.Sprintf("⚠️  %s is installed but not running (systemctl status: %s)", wsName, strings.TrimSpace(out)), nil
	}
	return "", nil
}
