package deploy

import (
	"fmt"
	"strings"
)

// CheckWebServer detects which web server is installed on the VPS.
// Returns: "nginx", "caddy", "apache", or "none".
func CheckWebServer(client *SSHClient) (string, error) {
	checks := []struct {
		name string
		cmd  string
	}{
		{"nginx", "which nginx && nginx -v 2>&1 || echo 'not found'"},
		{"caddy", "which caddy && caddy version || echo 'not found'"},
		{"apache", "which apache2 && apache2 -v 2>&1 | head -1 || echo 'not found'"},
	}

	for _, check := range checks {
		out, err := client.Run(check.cmd)
		if err == nil && !strings.Contains(out, "not found") && out != "" {
			return check.name, nil
		}
	}

	return "none", nil
}

// InstallNginx installs Nginx on the remote VPS.
func InstallNginx(client *SSHClient) (string, error) {
	var output strings.Builder

	// Update and install
	out, err := client.Run("apt-get update -qq && apt-get install -y -qq nginx")
	if err != nil {
		return out, fmt.Errorf("nginx install failed: %w\n%s", err, out)
	}
	output.WriteString(out)

	// Start and enable
	out, err = client.Run("systemctl enable nginx && systemctl start nginx")
	if err != nil {
		output.WriteString(fmt.Sprintf("\nStart failed: %v\n%s", err, out))
	} else {
		output.WriteString("\n✓ Nginx installed and running\n")
	}

	return output.String(), nil
}

// InstallCertbot installs certbot and its Nginx plugin.
func InstallCertbot(client *SSHClient) (string, error) {
	var output strings.Builder

	// Install certbot
	out, err := client.Run("apt-get update -qq && apt-get install -y -qq certbot python3-certbot-nginx")
	if err != nil {
		return out, fmt.Errorf("certbot install failed: %w\n%s", err, out)
	}
	output.WriteString(out)
	output.WriteString("\n✓ Certbot installed\n")

	return output.String(), nil
}

// ConfigureNginx writes an Nginx vhost config for a project.
func ConfigureNginx(client *SSHClient, domain, projectDir, projectName string, port int, isStatic bool) (string, error) {
	configName := projectName

	// Build the config
	var config strings.Builder
	config.WriteString(fmt.Sprintf(`server {
    listen 80;
    server_name %s;

    # Logs
    access_log /var/log/nginx/%s-access.log;
    error_log /var/log/nginx/%s-error.log;

`, domain, configName, configName))

	if isStatic {
		config.WriteString(fmt.Sprintf(`    root %s;
    index index.html index.htm;

    location / {
        try_files $uri $uri/ /index.html;
    }
`, projectDir))
	} else {
		config.WriteString(fmt.Sprintf(`    location / {
        proxy_pass http://127.0.0.1:%d;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_bypass $http_upgrade;
    }
`, port))
	}

	config.WriteString("}\n")

	// Write config to remote
	writeCmd := fmt.Sprintf("cat > /etc/nginx/sites-available/%s << 'NGINXEOF'\n%sNGINXEOF", configName, config.String())
	out, err := client.Run(writeCmd)
	if err != nil {
		return out, fmt.Errorf("write nginx config failed: %w\n%s", err, out)
	}

	// Enable site
	out, err = client.Run(fmt.Sprintf(
		"ln -sf /etc/nginx/sites-available/%s /etc/nginx/sites-enabled/%s && "+
			"nginx -t && systemctl reload nginx",
		configName, configName,
	))
	if err != nil {
		return out, fmt.Errorf("nginx reload failed: %w\n%s", err, out)
	}

	return fmt.Sprintf("✓ Nginx configured for %s → %s (port %d, static=%v)\n", domain, projectName, port, isStatic), nil
}

// ObtainSSL runs certbot to obtain an SSL certificate for a domain.
func ObtainSSL(client *SSHClient, domain, email string) (string, error) {
	cmd := fmt.Sprintf("certbot --nginx -d %s --non-interactive --agree-tos -m %s", domain, email)
	out, err := client.Run(cmd)
	if err != nil {
		return out, fmt.Errorf("certbot failed: %w\n%s", err, out)
	}

	return fmt.Sprintf("✓ SSL certificate obtained for %s\n%s", domain, out), nil
}

// AddDeployTools registers deployment tools on the agent registry.
func AddDeployTools() {
	// These are registered via agent.RegisterBuiltinTools extension
}
