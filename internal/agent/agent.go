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
)

// AgentRuntime provides the execution context for built-in tools.
type AgentRuntime struct {
	WorkDir         string
	Config          *config.Config
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

	// Dangerous local commands — require user confirmation via ask_user
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
	for _, d := range dangerous {
		if strings.Contains(command, d.pattern) {
			return &ToolResult{
				Success: false,
				Output:  fmt.Sprintf("⚠️  DANGEROUS COMMAND DETECTED: %s\n\nCommand: `%s`\n\nThis command could modify the system or delete data. You MUST use ask_user to get explicit permission from the user before running this. Explain what the command does and why it's needed.", d.label, command),
				Error:   "dangerous_command_blocked",
			}, nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = rt.WorkDir
	// Prevent interactive prompts: disconnect stdin
	cmd.Stdin = nil
	output, err := cmd.CombinedOutput()

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

	return &ToolResult{Success: true, Output: fmt.Sprintf("Wrote %d bytes to %s", len(content), path)}, nil
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
		return &ToolResult{
			Success: false,
			Output:  output,
			Error:   err.Error(),
		}, nil
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

// AgentLogger adapts log for the agent session.
type AgentLogger struct {
	Session Session
}

func (l *AgentLogger) LogToolCall(name string, args map[string]any) {
	l.Session.SendJSON(map[string]any{
		"type": "tool_call",
		"payload": map[string]any{
			"toolName":    name,
			"description": fmt.Sprintf("Running %s...", name),
			"arguments":   args,
		},
	})
}

func (l *AgentLogger) LogToolResult(name string, result *ToolResult) {
	l.Session.SendJSON(map[string]any{
		"type": "tool_result",
		"payload": map[string]any{
			"toolName": name,
			"success":  result.Success,
			"output":   result.Output,
			"error":    result.Error,
		},
	})
}

func (l *AgentLogger) LogAgentMessage(content string) {
	if content == "" {
		return // skip empty bubbles
	}
	l.Session.SendJSON(map[string]any{
		"type": "agent_message",
		"payload": map[string]any{
			"content": content,
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
