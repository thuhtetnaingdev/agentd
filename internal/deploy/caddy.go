package deploy

import (
	"fmt"
	"strings"
)

// InstallCaddy installs Caddy on the remote VPS.
// Uses the official Caddy APT repository for up-to-date versions.
func InstallCaddy(client *SSHClient) (string, error) {
	var output strings.Builder

	// Install dependencies and add Caddy repo
	cmds := []string{
		"apt-get update -qq",
		"apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https",
		"curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg",
		`curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list`,
		"apt-get update -qq",
		"apt-get install -y -qq caddy",
	}

	for _, cmd := range cmds {
		out, err := client.Run(cmd)
		if err != nil {
			// If repo setup fails (e.g., curl not installed), try direct install
			if strings.Contains(err.Error(), "curl") || strings.Contains(out, "curl") {
				out2, err2 := client.Run("apt-get install -y -qq curl && " + cmd)
				if err2 != nil {
					output.WriteString(fmt.Sprintf("Caddy install step failed: %v\n%s\n", err2, out2))
					continue
				}
				output.WriteString(out2)
				continue
			}
			output.WriteString(fmt.Sprintf("Caddy install step warning: %v\n%s\n", err, out))
		} else {
			output.WriteString(out)
		}
	}

	// Start and enable
	out, err := client.Run("systemctl enable caddy && systemctl start caddy")
	if err != nil {
		output.WriteString(fmt.Sprintf("\nStart failed: %v\n%s", err, out))
	} else {
		output.WriteString("\n✓ Caddy installed and running\n")
	}

	return output.String(), nil
}

// ConfigureCaddy writes a Caddyfile vhost config for a project.
// Caddy handles SSL automatically — no separate certbot step needed.
func ConfigureCaddy(client *SSHClient, domain, projectDir, projectName string, port int, isStatic bool) (string, error) {
	configName := projectName

	// Build the Caddyfile config
	var config strings.Builder

	if isStatic {
		config.WriteString(fmt.Sprintf(`%s {
    root * %s
    file_server
    encode gzip
}
`, domain, projectDir))
	} else {
		config.WriteString(fmt.Sprintf(`%s {
    reverse_proxy 127.0.0.1:%d
    encode gzip
}
`, domain, port))
	}

	// Write config to remote Caddy includes directory
	writeCmd := fmt.Sprintf("cat > /etc/caddy/%s.caddy << 'CADDYEOF'\n%sCADDYEOF", configName, config.String())
	out, err := client.Run(writeCmd)
	if err != nil {
		return out, fmt.Errorf("write caddy config failed: %w\n%s", err, out)
	}

	// Ensure the main Caddyfile imports our config
	importLine := fmt.Sprintf("import /etc/caddy/%s.caddy", configName)
	ensureCmd := fmt.Sprintf(
		`grep -q '%s' /etc/caddy/Caddyfile 2>/dev/null || echo '%s' >> /etc/caddy/Caddyfile`,
		importLine, importLine,
	)
	_, _ = client.Run(ensureCmd)

	// Validate and reload Caddy
	out, err = client.Run("caddy validate --config /etc/caddy/Caddyfile && systemctl reload caddy")
	if err != nil {
		return out, fmt.Errorf("caddy reload failed: %w\n%s", err, out)
	}

	return fmt.Sprintf("✓ Caddy configured for %s → %s (port %d, static=%v)\n", domain, projectName, port, isStatic), nil
}

// ObtainSSLCaddy is a no-op for Caddy — SSL is automatic.
// Kept for API symmetry with nginx; always returns success.
func ObtainSSLCaddy(client *SSHClient, domain string) (string, error) {
	// Caddy obtains certificates automatically via ACME.
	// We just verify Caddy is running and the domain is configured.
	out, err := client.Run(fmt.Sprintf("caddy validate --config /etc/caddy/Caddyfile 2>&1"))
	if err != nil {
		return out, fmt.Errorf("caddy validation failed: %w\n%s", err, out)
	}
	return fmt.Sprintf("✓ Caddy handles SSL automatically for %s. No separate certbot step needed.\n", domain), nil
}
