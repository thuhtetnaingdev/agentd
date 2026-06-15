package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// ToolResult is the result of executing a tool.
type ToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// ToolExecutor executes a tool by name with the given arguments.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args map[string]any) (*ToolResult, error)
	Definitions() []ToolDef
}

// Registry holds all registered tools.
type Registry struct {
	tools map[string]Tool
}

// Tool represents a single executable tool.
type Tool struct {
	Def     ToolDef
	Handler func(ctx context.Context, args map[string]any) (*ToolResult, error)
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

func (r *Registry) Register(tool Tool) {
	r.tools[tool.Def.Function.Name] = tool
}

func (r *Registry) Definitions() []ToolDef {
	defs := make([]ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Def)
	}
	return defs
}

func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) (*ToolResult, error) {
	t, ok := r.tools[name]
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("unknown tool: %s", name)}, nil
	}
	return t.Handler(ctx, args)
}

// RegisterBuiltinTools registers all built-in tools on the given registry.
func RegisterBuiltinTools(r *Registry, runtime *AgentRuntime) {
	// check_ssh_credentials
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "check_ssh_credentials",
				Description: "Check whether SSH credentials are configured for the given server ID. Returns the server details if found.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"serverId": {Type: "string", Description: "The server ID to check (e.g., 'srv_1'). Leave empty to check if any server exists."},
					},
				},
			},
		},
		Handler: runtime.CheckSSHCredentials,
	})

	// analyze_project
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "analyze_project",
				Description: "Analyze the current project to detect its type (Node.js, Go, Python, etc.), frameworks (Next.js, React, Express, etc.), Dockerfile presence, environment files, build commands, and start commands.",
				Parameters: JSONSchema{
					Type:       "object",
					Properties: map[string]JSONProp{},
				},
			},
		},
		Handler: runtime.AnalyzeProject,
	})

	// run_shell
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "run_shell",
				Description: "Execute a shell command LOCALLY only. Use for: ls, cat, node --version, npm --version. SSH/rsync/scp/sftp/sshpass are BLOCKED. Dangerous commands (rm, mv, sudo, brew, apt, pip install, chmod, chown, kill, etc.) are also BLOCKED — you MUST use ask_user first to get the user's explicit permission.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"command": {Type: "string", Description: "The shell command to execute."},
					},
					Required: []string{"command"},
				},
			},
		},
		Handler: runtime.RunShell,
	})

	// ask_user
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "ask_user",
				Description: "Pause and ask the user a question with multiple-choice options. The agent will wait for the user's response before continuing.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"question": {Type: "string", Description: "The question to ask the user."},
						"options":  {Type: "string", Description: "JSON array of option objects with 'id' and 'title' fields, e.g. [{\"id\":\"pm2\",\"title\":\"PM2\"},{\"id\":\"docker\",\"title\":\"Docker Compose\"}]"},
					},
					Required: []string{"question", "options"},
				},
			},
		},
		Handler: runtime.AskUser,
	})

	// list_servers
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "list_servers",
				Description: "List all configured VPS servers.",
				Parameters: JSONSchema{
					Type:       "object",
					Properties: map[string]JSONProp{},
				},
			},
		},
		Handler: runtime.ListServers,
	})

	// read_file
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "read_file",
				Description: "Read the contents of a file relative to the project directory.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"path": {Type: "string", Description: "Relative path to the file to read."},
					},
					Required: []string{"path"},
				},
			},
		},
		Handler: runtime.ReadFile,
	})

	// write_file
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "write_file",
				Description: "Write content to a file relative to the project directory.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"path":    {Type: "string", Description: "Relative path to the file to write."},
						"content": {Type: "string", Description: "Content to write to the file."},
					},
					Required: []string{"path", "content"},
				},
			},
		},
		Handler: runtime.WriteFile,
	})

	// --- Deploy Tools ---

	// run_ssh
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "run_ssh",
				Description: "Execute a command on a remote VPS via the built-in SSH client. Use this for ALL remote operations — never use run_shell with raw 'ssh' commands. This tool uses the configured credentials and won't prompt for passwords.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"serverId": {Type: "string", Description: "The server ID to run the command on."},
						"command":  {Type: "string", Description: "The shell command to execute on the remote server."},
					},
					Required: []string{"serverId", "command"},
				},
			},
		},
		Handler: runtime.RunSSH,
	})

	// check_project_exists
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "check_project_exists",
				Description: "Check if a project already exists at /var/www/<projectName> on the VPS. If it does, this returns a WARNING — DO NOT overwrite or delete without asking the user first. Shows existing files, size, and any running PM2 process.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"serverId":    {Type: "string", Description: "The server ID."},
						"projectName": {Type: "string", Description: "The project name to check."},
					},
					Required: []string{"serverId", "projectName"},
				},
			},
		},
		Handler: runtime.CheckProjectExists,
	})

	// check_disk_space
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "check_disk_space",
				Description: "Check available disk space on the VPS. Shows df -h output for /var/www and root, plus a summary of existing project sizes. Run this before deploying to ensure there's enough space.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"serverId": {Type: "string", Description: "The server ID to check."},
					},
					Required: []string{"serverId"},
				},
			},
		},
		Handler: runtime.CheckDiskSpace,
	})

	// check_port
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "check_port",
				Description: "Check if a specific port is in use on the VPS. Use this before starting an application to detect port conflicts.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"serverId": {Type: "string", Description: "The server ID."},
						"port":     {Type: "number", Description: "The port number to check (default: 3000)."},
					},
					Required: []string{"serverId", "port"},
				},
			},
		},
		Handler: runtime.CheckPort,
	})

	// check_runtime
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "check_runtime",
				Description: "Check which runtimes are installed on the VPS (Node.js, Go, Python) and their versions. Run this before deploying to ensure the required runtime is available.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"serverId": {Type: "string", Description: "The server ID."},
						"runtime":  {Type: "string", Description: "The runtime to check: 'node', 'go', 'python', or empty for all."},
					},
					Required: []string{"serverId"},
				},
			},
		},
		Handler: runtime.CheckRuntime,
	})

	// get_env_vars
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "get_env_vars",
				Description: "Get the list of environment variables configured for this project. Values are partially masked for security — the LLM can reference these keys when calling build_project or start_pm2. The actual values are injected server-side.",
				Parameters: JSONSchema{
					Type:       "object",
					Properties: map[string]JSONProp{},
				},
			},
		},
		Handler: runtime.GetEnvVars,
	})

	// test_connection
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "test_connection",
				Description: "Test SSH connection to a VPS server. Verifies connectivity and reports the remote architecture.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"serverId": {Type: "string", Description: "The server ID to test connection to."},
					},
					Required: []string{"serverId"},
				},
			},
		},
		Handler: runtime.TestConnection,
	})

	// setup_node
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "setup_node",
				Description: "Install a specific Node.js version on the remote VPS via the NodeSource binary distributions. Call this BEFORE setup_pm2. The version can come from .nvmrc, package.json engines.node, or the user's choice (e.g., '18', '20', '22').",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"serverId": {Type: "string", Description: "The server ID."},
						"version":  {Type: "string", Description: "The Node.js major version to install (e.g., '18', '20', '22'). Uses NodeSource setup_{version}.x repo."},
					},
					Required: []string{"serverId", "version"},
				},
			},
		},
		Handler: runtime.SetupNode,
	})

	// setup_pm2
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "setup_pm2",
				Description: "Install PM2 globally on the remote VPS. Requires Node.js to be already installed (call setup_node first). Uses 'npm install -g pm2'.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"serverId": {Type: "string", Description: "The server ID to set up PM2 on."},
					},
					Required: []string{"serverId"},
				},
			},
		},
		Handler: runtime.SetupPM2,
	})

	// setup_docker
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "setup_docker",
				Description: "Install Docker and Docker Compose on the remote VPS. Run this before deploying with Docker Compose.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"serverId": {Type: "string", Description: "The server ID to set up Docker on."},
					},
					Required: []string{"serverId"},
				},
			},
		},
		Handler: runtime.SetupDocker,
	})

	// install_deps
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "install_deps",
				Description: "Install project dependencies locally (npm install, go mod download, pip install, etc.). Run this before building.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"projectName": {Type: "string", Description: "The project directory name. Use '.' for the root."},
					},
				},
			},
		},
		Handler: runtime.InstallDeps,
	})

	// build_project
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "build_project",
				Description: "Build the project locally. For frontend projects (React, Next.js), pass env vars for the build. For backend projects, env vars are for runtime only and don't need to be included in the build.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"projectName": {Type: "string", Description: "The project directory name."},
						"buildCmd":    {Type: "string", Description: "The build command. Auto-detected if not provided."},
						"envVars":     {Type: "string", Description: "Environment variables in KEY=VALUE format, one per line. Only include for frontend projects that need them at build time."},
					},
					Required: []string{"projectName"},
				},
			},
		},
		Handler: runtime.BuildProject,
	})

	// deploy_rsync
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "deploy_rsync",
				Description: "Rsync the project files (or build output) to the remote VPS at /var/www/<projectName>. Skips node_modules, .git, .env files.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"serverId":    {Type: "string", Description: "The server ID to deploy to."},
						"projectName": {Type: "string", Description: "The project name (used as subdirectory under /var/www)."},
						"buildDir":    {Type: "string", Description: "Subdirectory containing build output (e.g., 'dist', 'build', '.next'). If empty, deploys the whole project."},
						"remotePath":  {Type: "string", Description: "Remote base path. Defaults to /var/www."},
					},
					Required: []string{"serverId", "projectName"},
				},
			},
		},
		Handler: runtime.DeployRsync,
	})

	// start_pm2
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "start_pm2",
				Description: "Start (or restart) the deployed project using PM2 on the remote VPS. Writes a .env file and saves the PM2 process list for auto-start on reboot.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"serverId":    {Type: "string", Description: "The server ID."},
						"projectName": {Type: "string", Description: "The PM2 app name (usually the project name)."},
						"startCmd":    {Type: "string", Description: "The command to start the app (e.g., 'npm start', 'node server.js')."},
						"envVars":     {Type: "string", Description: "Runtime environment variables in KEY=VALUE format, one per line."},
					},
					Required: []string{"serverId", "projectName", "startCmd"},
				},
			},
		},
		Handler: runtime.StartPM2,
	})

	// check_web_server
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "check_web_server",
				Description: "Check which web server (nginx, caddy, apache) is installed on the remote VPS. Returns the name or 'none'.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"serverId": {Type: "string", Description: "The server ID to check."},
					},
					Required: []string{"serverId"},
				},
			},
		},
		Handler: runtime.CheckWebServer,
	})

	// install_nginx
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "install_nginx",
				Description: "Install Nginx and Certbot on the remote VPS. Use this when no web server is detected and the user wants Nginx.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"serverId": {Type: "string", Description: "The server ID."},
					},
					Required: []string{"serverId"},
				},
			},
		},
		Handler: runtime.InstallNginx,
	})

	// configure_nginx
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "configure_nginx",
				Description: "Create an Nginx virtual host configuration for the project and reload Nginx. Configures either static file serving or reverse proxy based on isStatic flag.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"serverId":    {Type: "string", Description: "The server ID."},
						"domain":      {Type: "string", Description: "The domain name for the vhost."},
						"projectName": {Type: "string", Description: "The project name (used for config file naming and static root)."},
						"port":        {Type: "number", Description: "The local port the app listens on (for reverse proxy mode)."},
						"isStatic":    {Type: "boolean", Description: "If true, serves static files from /var/www/<projectName>. If false, reverse proxies to the given port."},
					},
					Required: []string{"serverId", "domain", "projectName", "isStatic"},
				},
			},
		},
		Handler: runtime.ConfigureNginx,
	})

	// setup_ssl
	r.Register(Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        "setup_ssl",
				Description: "Obtain an SSL certificate from Let's Encrypt using Certbot with the Nginx plugin.",
				Parameters: JSONSchema{
					Type: "object",
					Properties: map[string]JSONProp{
						"serverId": {Type: "string", Description: "The server ID."},
						"domain":   {Type: "string", Description: "The domain to obtain SSL for."},
						"email":    {Type: "string", Description: "Email for Let's Encrypt notifications. Defaults to admin@<domain>."},
					},
					Required: []string{"serverId", "domain"},
				},
			},
		},
		Handler: runtime.SetupSSL,
	})
}

// parseToolArgs parses JSON string arguments into a map.
func parseToolArgs(args string) (map[string]any, error) {
	var result map[string]any
	if err := json.Unmarshal([]byte(args), &result); err != nil {
		return nil, fmt.Errorf("parse tool args: %w", err)
	}
	return result, nil
}
