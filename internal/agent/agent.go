package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agentd/internal/config"
	"agentd/internal/deploy"
	"agentd/internal/project"
	"agentd/internal/store"
)

// AgentRuntime provides the execution context for built-in tools.
type AgentRuntime struct {
	WorkDir         string
	Config          *config.Config
	DeploymentStore *store.DeploymentStore
	EnvStore        *config.EnvStore
	Session         Session
	DefaultServerID string // set from UI server selector
}

// Session bridges the agent to the WebSocket session for ask_user.
type Session interface {
	SendJSON(v any) error
	WaitForChoice(timeout time.Duration) (string, error)
}

// resolveServerID returns the explicit serverId if provided, otherwise the default.
func (rt *AgentRuntime) resolveServerID(args map[string]any) string {
	if sid, ok := args["serverId"].(string); ok && sid != "" {
		return sid
	}
	return rt.DefaultServerID
}

// --- Tool Handlers ---

func (rt *AgentRuntime) CheckSSHCredentials(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	servers := rt.Config.ListServers()

	if serverID != "" {
		srv, ok := rt.Config.GetServer(serverID)
		if !ok {
			return &ToolResult{
				Success: false,
				Output:  fmt.Sprintf("Server '%s' not found. Available servers: %d", serverID, len(servers)),
			}, nil
		}
		info, _ := json.Marshal(map[string]any{
			"id":       srv.ID,
			"name":     srv.Name,
			"host":     srv.Host,
			"port":     srv.Port,
			"username": srv.Username,
			"hasPassword": srv.Password != "",
		})
		return &ToolResult{Success: true, Output: string(info)}, nil
	}

	if len(servers) == 0 {
		return &ToolResult{
			Success: false,
			Output:  "No servers configured. The user needs to add a VPS server in the Servers page before deploying.",
		}, nil
	}

	names := make([]string, len(servers))
	for i, s := range servers {
		names[i] = fmt.Sprintf("%s (%s)", s.Name, s.ID)
	}
	return &ToolResult{
		Success: true,
		Output:  fmt.Sprintf("%d server(s) configured: %s", len(servers), strings.Join(names, ", ")),
	}, nil
}

func (rt *AgentRuntime) AnalyzeProject(ctx context.Context, args map[string]any) (*ToolResult, error) {
	entries, err := os.ReadDir(rt.WorkDir)
	if err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}

	var projects []project.Project
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.Name() == "node_modules" || e.Name() == "vendor" {
			continue
		}
		p, err := project.Analyze(rt.WorkDir, e.Name())
		if err != nil {
			continue
		}
		projects = append(projects, p)
	}

	if len(projects) == 0 {
		return &ToolResult{
			Success: true,
			Output:  "No sub-projects found in the working directory. The directory itself might be a single project. Let me check the root...",
		}, nil
	}

	result := make([]map[string]any, len(projects))
	for i, p := range projects {
		result[i] = map[string]any{
			"id":         p.ID,
			"name":       p.Name,
			"type":       p.Type,
			"frameworks": p.Frameworks,
			"hasDocker":  p.HasDocker,
			"envFiles":   p.EnvFiles,
			"buildCmd":   p.BuildCmd,
			"startCmd":   p.StartCmd,
		}
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return &ToolResult{Success: true, Output: string(data)}, nil
}

func (rt *AgentRuntime) RunShell(ctx context.Context, args map[string]any) (*ToolResult, error) {
	command, _ := args["command"].(string)
	if command == "" {
		return &ToolResult{Success: false, Error: "command is required"}, nil
	}

	// Block commands that should use dedicated tools instead
	blocked := []string{"ssh ", "rsync ", "scp ", "sftp ", "sshpass "}
	for _, b := range blocked {
		if strings.Contains(command, b) {
			return &ToolResult{
				Success: false,
				Output:  fmt.Sprintf("BLOCKED: run_shell cannot execute '%s' commands. Use the dedicated tool instead:", strings.TrimSpace(b)),
				Error:   fmt.Sprintf("Use run_ssh for remote commands, or deploy_rsync for file transfers. Do NOT use run_shell with raw ssh/rsync/scp/sftp/sshpass."),
			}, nil
		}
	}

	// Dangerous local commands — require user confirmation
	dangerous := []struct {
		pattern string
		label   string
	}{
		{"rm ", "rm (delete files)"},
		{"mv ", "mv (move/rename files)"},
		{"chmod ", "chmod (change permissions)"},
		{"chown ", "chown (change ownership)"},
		{"brew ", "brew (system package manager)"},
		{"apt ", "apt (system package manager)"},
		{"apt-get ", "apt-get (system package manager)"},
		{"yum ", "yum (system package manager)"},
		{"dnf ", "dnf (system package manager)"},
		{"pacman ", "pacman (system package manager)"},
		{"pip install", "pip install (Python packages)"},
		{"gem install", "gem install (Ruby packages)"},
		{"npm install -g", "npm global install"},
		{"pnpm add -g", "pnpm global install"},
		{"yarn global add", "yarn global install"},
		{"sudo ", "sudo (superuser)"},
		{"kill ", "kill (terminate process)"},
		{"shutdown", "shutdown/reboot"},
		{"reboot", "shutdown/reboot"},
		{"mkfs.", "mkfs (format disk)"},
		{"dd if=", "dd (disk operations)"},
		{"> /dev/", "write to device"},
	}
	wasDangerous := false
	for _, d := range dangerous {
		if strings.Contains(command, d.pattern) {
			// Auto-ask the user for confirmation before running
			rt.Session.SendJSON(map[string]any{
				"type": "choice_request",
				"payload": map[string]any{
					"prompt": fmt.Sprintf(
						"⚠️  The agent wants to run a dangerous command:\n\n```\n%s\n```\n\n**Risk:** %s\n\nAllow?",
						command, d.label,
					),
					"choices": []map[string]string{
						{"id": "yes", "title": "Yes, run it"},
						{"id": "no", "title": "No, block it"},
					},
				},
			})

			choice, err := rt.Session.WaitForChoice(30 * time.Second)
			if err != nil || choice != "yes" {
				reason := "blocked by user"
				if err != nil {
					reason = fmt.Sprintf("user didn't respond: %v", err)
				}
				return &ToolResult{
					Success: false,
					Output:  fmt.Sprintf("⚠️  DANGEROUS COMMAND BLOCKED\n\nCommand: `%s`\nRisk: %s\nReason: %s", command, d.label, reason),
					Error:   "dangerous_command_blocked",
				}, nil
			}

			// User approved — proceed to execute
			wasDangerous = true
			break
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = rt.WorkDir
	// Prevent interactive prompts: disconnect stdin
	cmd.Stdin = nil
	output, err := cmd.CombinedOutput()

	// Post-check for dangerous commands: verify local disk space wasn't affected
	if wasDangerous {
		dfOut, _ := exec.Command("df", "-h", rt.WorkDir).CombinedOutput()
		if len(dfOut) > 0 {
			output = append(output, []byte(fmt.Sprintf("\n\n📊 Local disk after command:\n%s", string(dfOut)))...)
		}
		// Also check project file integrity
		gitOut, _ := exec.Command("git", "-C", rt.WorkDir, "status", "--porcelain").CombinedOutput()
		if len(gitOut) > 0 {
			output = append(output, []byte(fmt.Sprintf("\n\n📁 Project file changes after command:\n%s", string(gitOut)))...)
		}
	}

	if err != nil {
		return &ToolResult{
			Success: false,
			Output:  string(output),
			Error:   err.Error(),
		}, nil
	}

	return &ToolResult{Success: true, Output: string(output)}, nil
}

func (rt *AgentRuntime) AskUser(ctx context.Context, args map[string]any) (*ToolResult, error) {
	question, _ := args["question"].(string)
	optionsStr, _ := args["options"].(string)

	var options []map[string]string
	if err := json.Unmarshal([]byte(optionsStr), &options); err != nil {
		return &ToolResult{Success: false, Error: fmt.Sprintf("invalid options JSON: %v", err)}, nil
	}

	// Send choice request to frontend
	choices := make([]map[string]string, len(options))
	for i, o := range options {
		choices[i] = map[string]string{
			"id":    o["id"],
			"title": o["title"],
		}
	}

	rt.Session.SendJSON(map[string]any{
		"type":    "choice_request",
		"payload": map[string]any{
			"prompt":  question,
			"choices": choices,
		},
	})

	// Wait for user response
	choice, err := rt.Session.WaitForChoice(2 * time.Minute)
	if err != nil {
		return &ToolResult{
			Success: false,
			Output:  fmt.Sprintf("FAILED: The user did NOT respond to the question: \"%s\"\n\nDO NOT retry ask_user. DO NOT assume any answer. Tell the user you didn't get a response and ask them to send a new message.", question),
			Error:   "user_did_not_respond",
		}, nil
	}

	return &ToolResult{
		Success: true,
		Output:  fmt.Sprintf("Question: %s\n\nUser selected: %s", question, choice),
	}, nil
}

func (rt *AgentRuntime) ListServers(ctx context.Context, args map[string]any) (*ToolResult, error) {
	servers := rt.Config.ListServers()

	// When a server is selected in the UI (per-project context), only show that one.
	if rt.DefaultServerID != "" {
		var filtered []config.ServerConfig
		for _, s := range servers {
			if s.ID == rt.DefaultServerID {
				filtered = append(filtered, s)
				break
			}
		}
		if len(filtered) > 0 {
			servers = filtered
		}
	}

	// Mask passwords in output — never leak them to chat history
	for i := range servers {
		if servers[i].Password != "" {
			servers[i].Password = "••••••••"
		}
	}
	data, _ := json.MarshalIndent(servers, "", "  ")
	return &ToolResult{Success: true, Output: string(data)}, nil
}

func (rt *AgentRuntime) ReadFile(ctx context.Context, args map[string]any) (*ToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return &ToolResult{Success: false, Error: "path is required"}, nil
	}

	fullPath := filepath.Join(rt.WorkDir, path)
	// Security: prevent path traversal
	if !strings.HasPrefix(filepath.Clean(fullPath), filepath.Clean(rt.WorkDir)) {
		return &ToolResult{Success: false, Error: "path traversal denied"}, nil
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}

	return &ToolResult{Success: true, Output: string(data)}, nil
}

func (rt *AgentRuntime) WriteFile(ctx context.Context, args map[string]any) (*ToolResult, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return &ToolResult{Success: false, Error: "path is required"}, nil
	}

	fullPath := filepath.Join(rt.WorkDir, path)
	if !strings.HasPrefix(filepath.Clean(fullPath), filepath.Clean(rt.WorkDir)) {
		return &ToolResult{Success: false, Error: "path traversal denied"}, nil
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}

	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}

	// Post-check: verify the file exists and is readable
	info, statErr := os.Stat(fullPath)
	if statErr != nil {
		return &ToolResult{Success: false, Output: fmt.Sprintf("Wrote file but cannot verify: %v", statErr), Error: statErr.Error()}, nil
	}

	return &ToolResult{Success: true, Output: fmt.Sprintf("Wrote %d bytes to %s (verified: %d bytes on disk)", len(content), path, info.Size())}, nil
}

// --- Deploy Tool Handlers ---

func (rt *AgentRuntime) SetupNode(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	version, _ := args["version"].(string)

	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	if version == "" {
		return &ToolResult{
			Success: false,
			Output:  "No Node.js version specified. Use ask_user to let the user choose a version (e.g., 18, 20, 22).",
			Error:   "version is required",
		}, nil
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	output, err := deploy.SetupNode(client, version)
	if err != nil {
		return &ToolResult{Success: false, Output: output, Error: err.Error()}, nil
	}

	return &ToolResult{Success: true, Output: output}, nil
}

func (rt *AgentRuntime) SetupPM2(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)

	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	output, err := deploy.SetupPM2(client)
	if err != nil {
		return &ToolResult{Success: false, Output: output, Error: err.Error()}, nil
	}

	return &ToolResult{Success: true, Output: output}, nil
}

func (rt *AgentRuntime) SetupDocker(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	output, err := deploy.SetupDocker(client)
	if err != nil {
		return &ToolResult{Success: false, Output: output, Error: err.Error()}, nil
	}

	return &ToolResult{Success: true, Output: output}, nil
}

func (rt *AgentRuntime) InstallDeps(ctx context.Context, args map[string]any) (*ToolResult, error) {
	projectName, _ := args["projectName"].(string)
	if projectName == "" {
		projectName = "."
	}

	projPath := rt.WorkDir
	projType := "unknown"
	pkgManager := ""

	if projectName != "." {
		projPath = filepath.Join(rt.WorkDir, projectName)
	}

	// Detect project type and package manager
	p, err := project.Analyze(rt.WorkDir, projectName)
	if err == nil {
		projType = p.Type
		pkgManager = p.PackageManager
	}
	if projectName == "." {
		// For root project, analyze directly
		rootName := filepath.Base(rt.WorkDir)
		rp, rerr := project.AnalyzeDir(rt.WorkDir, rootName)
		if rerr == nil {
			projType = rp.Type
			pkgManager = rp.PackageManager
		}
	}

	result, err := deploy.InstallDeps(projPath, projType, pkgManager)
	if err != nil {
		return &ToolResult{Success: false, Output: result.Output, Error: err.Error()}, nil
	}

	return &ToolResult{Success: true, Output: result.Output}, nil
}

func (rt *AgentRuntime) BuildProject(ctx context.Context, args map[string]any) (*ToolResult, error) {
	projectName, _ := args["projectName"].(string)
	buildCmd, _ := args["buildCmd"].(string)

	if projectName == "" {
		projectName = "."
	}
	if buildCmd == "" {
		// Try to detect
		p, err := project.Analyze(rt.WorkDir, projectName)
		if err == nil {
			buildCmd = p.BuildCmd
		}
	}

	projPath := rt.WorkDir
	if projectName != "." {
		projPath = filepath.Join(rt.WorkDir, projectName)
	}

	// Parse env vars if provided
	envVars := map[string]string{}
	if envStr, ok := args["envVars"].(string); ok && envStr != "" {
		for _, line := range strings.Split(envStr, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				envVars[parts[0]] = parts[1]
			}
		}
	}

	result, err := deploy.BuildProject(projPath, buildCmd, envVars)
	if err != nil {
		return &ToolResult{Success: false, Output: result.Output, Error: err.Error()}, nil
	}

	return &ToolResult{Success: true, Output: result.Output}, nil
}

func (rt *AgentRuntime) DeployRsync(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	projectName, _ := args["projectName"].(string)
	buildDir, _ := args["buildDir"].(string)

	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	if projectName == "" {
		projectName = filepath.Base(rt.WorkDir)
	}

	// If projectName matches the workdir basename, the source IS the workdir itself
	rootName := filepath.Base(rt.WorkDir)
	srcPath := rt.WorkDir
	if projectName != rootName {
		srcPath = filepath.Join(rt.WorkDir, projectName)
	}
	if buildDir != "" {
		srcPath = filepath.Join(srcPath, buildDir)
	}

	remotePath := "/var/www"
	if rp, ok := args["remotePath"].(string); ok && rp != "" {
		remotePath = rp
	}

	fullRemotePath := filepath.Join(remotePath, projectName)

	// Use Go-native SSH file transfer (no sshpass needed)
	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	output, err := deploy.DeployViaSSH(client, srcPath, fullRemotePath, nil)
	if err != nil {
		return &ToolResult{Success: false, Output: output, Error: err.Error()}, nil
	}

	// Post-check: verify disk space after deployment
	if warn, _ := deploy.CheckDiskSpace(client); warn != "" {
		output += fmt.Sprintf("\n\n%s", warn)
	}

	return &ToolResult{Success: true, Output: output}, nil
}

func (rt *AgentRuntime) StartPM2(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	projectName, _ := args["projectName"].(string)
	startCmd, _ := args["startCmd"].(string)

	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	dir := filepath.Join("/var/www", projectName)
	envVars := map[string]string{}
	if envStr, ok := args["envVars"].(string); ok && envStr != "" {
		for _, line := range strings.Split(envStr, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				envVars[parts[0]] = parts[1]
			}
		}
	}

	output, err := deploy.StartPM2App(client, projectName, dir, startCmd, envVars)
	if err != nil {
		return &ToolResult{Success: false, Output: output, Error: err.Error()}, nil
	}

	// Save PM2 config for resurrection on reboot
	deploy.SavePM2Config(client)

	// Post-check: wait a moment then verify the app is online
	time.Sleep(3 * time.Second)
	if warn, _ := deploy.CheckPM2Status(client, projectName); warn != "" {
		output += fmt.Sprintf("\n\n⚠️  Post-check: %s", warn)
	}

	return &ToolResult{Success: true, Output: output}, nil
}

func (rt *AgentRuntime) CheckWebServer(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	result, err := deploy.CheckWebServer(client)
	if err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}

	return &ToolResult{Success: true, Output: result}, nil
}

func (rt *AgentRuntime) InstallNginx(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	output, err := deploy.InstallNginx(client)
	if err != nil {
		return &ToolResult{Success: false, Output: output, Error: err.Error()}, nil
	}

	// Also install certbot
	certOut, _ := deploy.InstallCertbot(client)
	output += "\n" + certOut

	// Post-check: verify nginx is actually running
	if warn, _ := deploy.CheckWebServerRunning(client, "nginx"); warn != "" {
		output += fmt.Sprintf("\n\n%s", warn)
	}

	return &ToolResult{Success: true, Output: output}, nil
}

func (rt *AgentRuntime) ConfigureNginx(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	domain, _ := args["domain"].(string)
	projectName, _ := args["projectName"].(string)
	port, _ := args["port"].(float64) // JSON numbers are float64
	isStatic, _ := args["isStatic"].(bool)

	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	projectDir := filepath.Join("/var/www", projectName)

	output, err := deploy.ConfigureNginx(client, domain, projectDir, projectName, int(port), isStatic)
	if err != nil {
		return &ToolResult{Success: false, Output: output, Error: err.Error()}, nil
	}

	// Post-check: validate web server config (nginx/caddy/apache)
	if warn, _ := deploy.ValidateWebServer(client); warn != "" {
		output += fmt.Sprintf("\n\n⚠️  Post-check: %s", warn)
	}

	// Post-check: verify the site actually responds to HTTP
	if domain != "" {
		if warn, _ := deploy.CheckHTTPResponse(client, domain, isStatic, projectName); warn != "" {
			output += fmt.Sprintf("\n\n%s", warn)
		}
	}

	return &ToolResult{Success: true, Output: output}, nil
}

func (rt *AgentRuntime) SetupSSL(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	domain, _ := args["domain"].(string)
	email, _ := args["email"].(string)

	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	if email == "" {
		email = "admin@" + domain
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	output, err := deploy.ObtainSSL(client, domain, email)
	if err != nil {
		return &ToolResult{Success: false, Output: output, Error: err.Error()}, nil
	}

	// Post-check: verify the SSL cert is installed and valid
	if warn, _ := deploy.CheckSSLCert(client, domain); warn != "" {
		output += fmt.Sprintf("\n\n⚠️  Post-check: %s", warn)
	}

	return &ToolResult{Success: true, Output: output}, nil
}

func (rt *AgentRuntime) InstallCaddy(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	output, err := deploy.InstallCaddy(client)
	if err != nil {
		return &ToolResult{Success: false, Output: output, Error: err.Error()}, nil
	}

	// Post-check: verify caddy is actually running
	if warn, _ := deploy.CheckWebServerRunning(client, "caddy"); warn != "" {
		output += fmt.Sprintf("\n\n%s", warn)
	}

	return &ToolResult{Success: true, Output: output}, nil
}

func (rt *AgentRuntime) ConfigureCaddy(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	domain, _ := args["domain"].(string)
	projectName, _ := args["projectName"].(string)
	port, _ := args["port"].(float64) // JSON numbers are float64
	isStatic, _ := args["isStatic"].(bool)

	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	projectDir := filepath.Join("/var/www", projectName)

	output, err := deploy.ConfigureCaddy(client, domain, projectDir, projectName, int(port), isStatic)
	if err != nil {
		return &ToolResult{Success: false, Output: output, Error: err.Error()}, nil
	}

	// Post-check: validate web server config (nginx/caddy/apache)
	if warn, _ := deploy.ValidateWebServer(client); warn != "" {
		output += fmt.Sprintf("\n\n⚠️  Post-check: %s", warn)
	}

	// Post-check: verify the site actually responds to HTTP
	if domain != "" {
		if warn, _ := deploy.CheckHTTPResponse(client, domain, isStatic, projectName); warn != "" {
			output += fmt.Sprintf("\n\n%s", warn)
		}
	}

	return &ToolResult{Success: true, Output: output}, nil
}

func (rt *AgentRuntime) RunSSH(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	command, _ := args["command"].(string)

	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	if command == "" {
		return &ToolResult{Success: false, Error: "command is required"}, nil
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	output, err := client.Run(command)
	if err != nil {
		// Post-check: even on failure, verify disk space wasn't affected
		if warn, _ := deploy.CheckDiskSpace(client); warn != "" {
			output += fmt.Sprintf("\n\n%s", warn)
		}
		return &ToolResult{
			Success: false,
			Output:  output,
			Error:   err.Error(),
		}, nil
	}

	// Post-check: verify disk space after remote command
	if warn, _ := deploy.CheckDiskSpace(client); warn != "" {
		output += fmt.Sprintf("\n\n%s", warn)
	}

	// Post-check: verify critical services are still running
	if warn, _ := deploy.CheckRemoteServices(client); warn != "" {
		output += fmt.Sprintf("\n\n%s", warn)
	}

	return &ToolResult{Success: true, Output: output}, nil
}

// --- Pre-flight Check Handlers ---

func (rt *AgentRuntime) CheckProjectExists(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	projectName, _ := args["projectName"].(string)

	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	if projectName == "" {
		projectName = filepath.Base(rt.WorkDir)
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	remotePath := filepath.Join("/var/www", projectName)

	// Check if directory exists and what's in it
	cmd := fmt.Sprintf(
		"if [ -d %s ]; then echo 'EXISTS'; ls -la %s | head -20; echo '---SIZE---'; du -sh %s; echo '---PM2---'; pm2 list 2>/dev/null | grep %s || echo 'no pm2 process'; else echo 'NOT_FOUND'; fi",
		remotePath, remotePath, remotePath, projectName,
	)
	output, err := client.Run(cmd)
	if err != nil {
		return &ToolResult{Success: false, Output: output, Error: err.Error()}, nil
	}

	if strings.Contains(output, "NOT_FOUND") {
		return &ToolResult{
			Success: true,
			Output:  fmt.Sprintf("Path %s does not exist. Safe to deploy.", remotePath),
		}, nil
	}

	return &ToolResult{
		Success: false,
		Output:  fmt.Sprintf("⚠️  WARNING: %s ALREADY EXISTS on the server:\n\n%s\n\nDO NOT delete or overwrite without asking the user first. Use ask_user to confirm: keep existing, backup then overwrite, or abort.", remotePath, output),
	}, nil
}

func (rt *AgentRuntime) CheckDiskSpace(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	// Check available disk space in /var/www and overall
	output, err := client.Run("df -h /var/www / 2>/dev/null && echo '---' && du -sh /var/www/* 2>/dev/null | sort -rh | head -10")
	if err != nil {
		return &ToolResult{Success: false, Output: output, Error: err.Error()}, nil
	}

	return &ToolResult{Success: true, Output: output}, nil
}

func (rt *AgentRuntime) CheckPort(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	port, _ := args["port"].(float64)
	if port == 0 {
		port = 3000
	}

	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	cmd := fmt.Sprintf("ss -tlnp | grep ':%d ' || echo 'PORT_FREE'", int(port))
	output, err := client.Run(cmd)
	if err != nil && !strings.Contains(output, "PORT_FREE") {
		return &ToolResult{Success: false, Output: output, Error: err.Error()}, nil
	}

	if strings.Contains(output, "PORT_FREE") {
		return &ToolResult{Success: true, Output: fmt.Sprintf("Port %d is free.", int(port))}, nil
	}

	return &ToolResult{
		Success: false,
		Output:  fmt.Sprintf("⚠️  WARNING: Port %d is IN USE:\n%s\n\nDO NOT kill the existing process. Use ask_user to let the user decide: stop the existing process, use a different port, or abort deployment.", int(port), output),
	}, nil
}

func (rt *AgentRuntime) CheckRuntime(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	runtime, _ := args["runtime"].(string) // node, go, python

	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	checks := []string{}
	switch runtime {
	case "node":
		checks = []string{
			"node --version 2>&1 || echo 'node: NOT INSTALLED'",
			"npm --version 2>&1 || echo 'npm: NOT INSTALLED'",
			"pm2 --version 2>&1 || echo 'pm2: NOT INSTALLED'",
		}
	case "go":
		checks = []string{"go version 2>&1 || echo 'go: NOT INSTALLED'"}
	case "python":
		checks = []string{
			"python3 --version 2>&1 || echo 'python3: NOT INSTALLED'",
			"pip3 --version 2>&1 || echo 'pip3: NOT INSTALLED'",
		}
	default:
		checks = []string{
			"node --version 2>&1 || echo 'node: NOT INSTALLED'",
			"go version 2>&1 || echo 'go: NOT INSTALLED'",
			"python3 --version 2>&1 || echo 'python3: NOT INSTALLED'",
		}
	}

	cmd := strings.Join(checks, " && echo '---' && ")
	output, err := client.Run(cmd)
	if err != nil {
		return &ToolResult{Success: false, Output: output, Error: err.Error()}, nil
	}

	return &ToolResult{Success: true, Output: output}, nil
}

func (rt *AgentRuntime) GetEnvVars(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if rt.EnvStore == nil {
		return &ToolResult{Success: true, Output: "No environment variables configured."}, nil
	}

	entries, err := rt.EnvStore.List()
	if err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}

	if len(entries) == 0 {
		return &ToolResult{Success: true, Output: "No environment variables configured. The user can add them in Settings."}, nil
	}

	// Return var names and masked values so the LLM knows what's available
	result := make([]map[string]string, len(entries))
	for i, e := range entries {
		display := e.Value
		if len(display) > 8 {
			display = display[:4] + "..." + display[len(display)-4:]
		}
		result[i] = map[string]string{
			"key":   e.Key,
			"value": display,
		}
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return &ToolResult{Success: true, Output: string(data)}, nil
}

func (rt *AgentRuntime) TestConnection(ctx context.Context, args map[string]any) (*ToolResult, error) {
	serverID := rt.resolveServerID(args)
	srv, ok := rt.Config.GetServer(serverID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", serverID)}, nil
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	if err := client.TestConnection(); err != nil {
		return &ToolResult{Success: false, Error: fmt.Sprintf("connection failed: %v", err)}, nil
	}

	arch, _ := client.DetectArch()
	return &ToolResult{Success: true, Output: fmt.Sprintf("Connection successful. Architecture: %s", arch)}, nil
}


func (rt *AgentRuntime) RecordDeployment(ctx context.Context, args map[string]any) (*ToolResult, error) {
	projectName, _ := args["projectName"].(string)
	status, _ := args["status"].(string) // "success" or "failed"
	port := 0
	if p, ok := args["port"].(float64); ok {
		port = int(p)
	}
	domain, _ := args["domain"].(string)
	errMsg, _ := args["error"].(string)

	if projectName == "" {
		return &ToolResult{Success: false, Error: "projectName is required"}, nil
	}
	if status == "" {
		status = "success"
	}

	// Resolve server info
	serverID := rt.resolveServerID(args)
	serverName := serverID
	host := ""
	if srv, ok := rt.Config.GetServer(serverID); ok {
		serverName = srv.Name
		host = srv.Host
	}

	if rt.DeploymentStore == nil {
		return &ToolResult{Success: false, Error: "deployment store not available"}, nil
	}

	// If projectName is ".", use the workdir basename
	if projectName == "." {
		projectName = filepath.Base(rt.WorkDir)
	}

	rec := store.DeploymentRecord{
		ProjectName:  projectName,
		ServerID:     serverID,
		ServerName:   serverName,
		Host:         host,
		Port:         port,
		Domain:       domain,
		Status:       status,
		HealthStatus: "unknown",
		DeployedAt:   time.Now(),
		Error:        errMsg,
	}

	created, err := rt.DeploymentStore.Create(rec)
	if err != nil {
		return &ToolResult{Success: false, Error: fmt.Sprintf("save deployment: %v", err)}, nil
	}

	// If status is success, immediately run a health check
	if status == "success" && host != "" && port > 0 {
		if srv, ok := rt.Config.GetServer(serverID); ok {
			client := deploy.NewSSHClient(host, srv.Port, srv.Username, srv.Password)

			healthStatus := "healthy"
			checkOutput, err := runHealthCheck(client, port)
			client.Close()
			if err != nil || !strings.Contains(checkOutput, "PORT_OK") {
				healthStatus = "unhealthy"
			}
			rt.DeploymentStore.UpdateHealth(created.ID, healthStatus, time.Now())
			created.HealthStatus = healthStatus
			created.LastChecked = time.Now()
		}
	}

	data, _ := json.MarshalIndent(created, "", "  ")
	return &ToolResult{Success: true, Output: fmt.Sprintf("✓ Deployment recorded:\n%s", string(data))}, nil
}

func (rt *AgentRuntime) CheckDeploymentHealth(ctx context.Context, args map[string]any) (*ToolResult, error) {
	deploymentID, _ := args["deploymentId"].(string)
	projectName, _ := args["projectName"].(string)
	port := 0
	if p, ok := args["port"].(float64); ok {
		port = int(p)
	}

	if rt.DeploymentStore == nil {
		return &ToolResult{Success: false, Error: "deployment store not available"}, nil
	}

	var rec *store.DeploymentRecord
	var err error

	if deploymentID != "" {
		rec, err = rt.DeploymentStore.Get(deploymentID)
	} else {
		// Find the most recent deployment for this project
		all, listErr := rt.DeploymentStore.List()
		if listErr != nil {
			return &ToolResult{Success: false, Error: fmt.Sprintf("list deployments: %v", listErr)}, nil
		}
		for i := range all {
			if all[i].ProjectName == projectName || (projectName == "" && i == 0) {
				rec = &all[i]
				break
			}
		}
	}

	if err != nil || rec == nil {
		return &ToolResult{Success: false, Output: "No matching deployment found."}, nil
	}

	// Resolve server from the deployment record
	srv, ok := rt.Config.GetServer(rec.ServerID)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("server %s not found", rec.ServerID)}, nil
	}

	if port == 0 {
		port = rec.Port
	}
	if port == 0 {
		port = 3000 // default
	}

	client := deploy.NewSSHClient(srv.Host, srv.Port, srv.Username, srv.Password)
	defer client.Close()

	healthStatus := "healthy"
	output, err := runHealthCheck(client, port)
	if err != nil || !strings.Contains(output, "PORT_OK") {
		healthStatus = "unhealthy"
	}

	rt.DeploymentStore.UpdateHealth(rec.ID, healthStatus, time.Now())

	return &ToolResult{Success: true, Output: fmt.Sprintf("Deployment %s (%s:%d): health = %s\n%s", rec.ProjectName, rec.Host, port, healthStatus, output)}, nil
}

// runHealthCheck performs an HTTP health check via SSH on the given port.
func runHealthCheck(client *deploy.SSHClient, port int) (string, error) {
	// Try curl first, then fall back to checking if the port is listening
	cmd := fmt.Sprintf("curl -s -o /dev/null -w '%%{http_code}' --max-time 5 http://localhost:%d 2>&1 || echo 'CURL_FAILED'", port)
	output, err := client.Run(cmd)
	if err == nil && !strings.Contains(output, "CURL_FAILED") && output != "" {
		status := strings.TrimSpace(output)
		if status >= "200" && status < "500" {
			return fmt.Sprintf("HTTP %s on port %d — PORT_OK", status, port), nil
		}
		return fmt.Sprintf("HTTP %s on port %d", status, port), nil
	}

	// Fallback: check if port is listening (ss/telnet)
	checkCmd := fmt.Sprintf("ss -tlnp | grep -q ':%d ' && echo 'LISTENING' || echo 'NOT_LISTENING'", port)
	checkOut, _ := client.Run(checkCmd)
	if strings.Contains(checkOut, "LISTENING") {
		return fmt.Sprintf("Port %d is LISTENING — PORT_OK", port), nil
	}
	return fmt.Sprintf("Port %d is NOT_LISTENING", port), nil
}

// AgentLogger adapts log for the agent session.
type AgentLogger struct {
	Session Session
}

func (l *AgentLogger) LogToolCall(name string, args map[string]any, toolCallID string) {
	l.Session.SendJSON(map[string]any{
		"type": "tool_call",
		"payload": map[string]any{
			"toolName":    name,
			"description": fmt.Sprintf("Running %s...", name),
			"arguments":   args,
			"toolCallId":  toolCallID,
		},
	})
}

func (l *AgentLogger) LogToolResult(name string, result *ToolResult, toolCallID string) {
	l.Session.SendJSON(map[string]any{
		"type": "tool_result",
		"payload": map[string]any{
			"toolName":   name,
			"toolCallId": toolCallID,
			"success":    result.Success,
			"output":     result.Output,
			"error":      result.Error,
		},
	})
}

func (l *AgentLogger) LogAgentMessage(content string) {
	l.LogAgentMessageWithReasoning(content, "")
}

// LogAgentMessageWithReasoning sends an agent message with optional reasoning content.
func (l *AgentLogger) LogAgentMessageWithReasoning(content, reasoning string) {
	if strings.TrimSpace(content) == "" {
		return // skip empty bubbles (including whitespace-only like "\n")
	}
	payload := map[string]any{
		"content": content,
	}
	if reasoning != "" {
		payload["reasoning"] = reasoning
	}
	l.Session.SendJSON(map[string]any{
		"type": "agent_message",
		"payload": payload,
	})
}

// LogReasoningDelta streams a reasoning token to the frontend.
func (l *AgentLogger) LogReasoningDelta(chunk string) {
	if chunk == "" {
		return
	}
	l.Session.SendJSON(map[string]any{
		"type": "reasoning_update",
		"payload": map[string]any{
			"content": chunk,
		},
	})
}

// LogContentChunk streams a content token to the frontend.
func (l *AgentLogger) LogContentChunk(chunk string) {
	if chunk == "" {
		return
	}
	l.Session.SendJSON(map[string]any{
		"type": "content_chunk",
		"payload": map[string]any{
			"content": chunk,
		},
	})
}

func (l *AgentLogger) LogUsage(usage SessionUsage) {
	hitRate := 0.0
	total := usage.CacheHitTokens + usage.CacheMissTokens
	if total > 0 {
		hitRate = float64(usage.CacheHitTokens) / float64(total) * 100
	}
	ctxPercent := 0.0
	if usage.ContextWindow > 0 {
		ctxPercent = float64(usage.PromptTokens) / float64(usage.ContextWindow) * 100
	}
	l.Session.SendJSON(map[string]any{
		"type": "usage_update",
		"payload": map[string]any{
			"promptTokens":         usage.PromptTokens,
			"completionTokens":     usage.CompletionTokens,
			"totalTokens":          usage.TotalTokens,
			"cacheHitTokens":       usage.CacheHitTokens,
			"cacheMissTokens":      usage.CacheMissTokens,
			"cacheHitRate":         hitRate,
			"contextWindow":        usage.ContextWindow,
			"contextUsagePercent":  ctxPercent,
		},
	})
}

func (l *AgentLogger) LogError(err error) {
	log.Printf("[agent] error: %v", err)
	l.Session.SendJSON(map[string]any{
		"type": "agent_error",
		"payload": map[string]any{
			"message": err.Error(),
		},
	})
}
