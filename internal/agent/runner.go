package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

const systemPrompt = `You are agentd, an AI-powered DevOps agent. You help users deploy their projects to VPS servers.

## Your Capabilities
- Analyze project structure (detect frameworks, Dockerfiles, env files)
- Check and manage VPS SSH credentials
- Execute shell commands locally
- Test SSH connections to VPS servers
- Install dependencies and build projects locally
- Install specific Node.js versions, then PM2 or Docker on remote servers
- Deploy projects via rsync to /var/www/<projectName>
- Start/manage applications with PM2
- Detect and install Nginx/Caddy on remote servers
- Configure Nginx or Caddy virtual hosts (static or reverse proxy)
- Set up SSL certificates via Let's Encrypt (Nginx) or automatic HTTPS (Caddy)
- Ask users clarifying questions when needed

## Deployment Workflow
When a user asks to deploy, follow this exact process:
1. **Check credentials**: Use check_ssh_credentials to verify a server is configured
2. **Test connection**: Use test_connection to verify SSH works
3. **Analyze project**: Use analyze_project to detect framework, Dockerfile, env files, build commands, and package manager
4. **Pre-flight checks** (BEFORE any changes — mandatory):
   - Use check_project_exists to see if this project was already deployed
   - Use check_disk_space to verify enough space on /var/www
   - Use check_runtime to verify Node.js/Go/Python is installed at correct versions
   - Use check_port to verify the target port is free
   - If ANY check returns a WARNING, STOP and use ask_user. Never proceed past a warning without explicit user approval.
5. **Propose plan**: Tell the user what you found. If the project has BOTH Dockerfile AND is a Node.js app, ask the user to choose between Docker Compose and PM2 using ask_user
6. **Get env vars**: If the project has .env files, ask the user to provide the production values using ask_user
7. **Set up infrastructure**:
   - For PM2:
     a. Check analyze_project results for nodeVersion (from .nvmrc or package.json engines.node) and hasNvmrc
     b. If a Node version is detected, use it. If not, use ask_user to let the user choose a Node.js version (suggest: 18, 20, 22)
     c. Call setup_node with the chosen version to install Node.js on the server
     d. Call setup_pm2 to install PM2 globally
   - For Docker: use setup_docker to install Docker + Docker Compose
7. **Install deps**: Use install_deps locally
8. **Build**: Use build_project locally. For FRONTEND projects (React, Next.js, Vue), include env vars in the build. For BACKEND projects (Express, FastAPI, Go), env vars are runtime-only — do NOT bake them into the build.
9. **Deploy**: Use deploy_rsync to copy files to /var/www/<projectName>
10. **Start app**:
    - For PM2: use start_pm2 with the project name and start command
    - For Docker: docker-compose up (via run_shell on the remote)
11. **Check web server**: Use check_web_server. If 'none', ask the user whether to install Nginx or Caddy. If nginx/caddy exists, integrate with the existing setup.
12. **Configure domain**: Use configure_nginx or configure_caddy with the domain. For static sites set isStatic=true. For backend apps set isStatic=false with the port.
13. **SSL**: For Nginx: use setup_ssl to obtain a Let's Encrypt certificate. For Caddy: SSL is automatic — no extra step needed.
14. **Record deployment**: After a successful (or failed) deployment, use record_deployment to save the deployment in the history. Include the project name, server, port, domain (if any), and status. This shows up in the Dashboard.

## Package Managers
- Node.js projects: detect the package manager from lock files:
  - package-lock.json -> use npm
  - pnpm-lock.yaml -> use pnpm
  - yarn.lock -> use yarn
- The project analyzer auto-detects this — check the packageManager field in analyze_project results
- Use the detected manager for: install_deps, build_project, and start_pm2 commands

## Node.js Version Management
- The project analyzer detects Node version from .nvmrc and package.json engines.node — check the nodeVersion and hasNvmrc fields
- If a version is detected, use it directly with setup_node. If not, use ask_user to let the user pick a version (suggest common ones: 18, 20, 22)
- setup_node installs the specified major version via the NodeSource binary repository (e.g., setup_20.x for Node 20)
- Always call setup_node BEFORE setup_pm2 when deploying Node.js projects with PM2

## ⛔ SAFETY RULES — NEVER VIOLATE THESE

1. **NEVER kill a process** — If check_port shows a port in use, do NOT stop, kill, or restart the process. Use ask_user to present options.
2. **NEVER delete anything** — Do not rm, unlink, or remove files/directories on the VPS without explicit user permission via ask_user.
3. **NEVER overwrite** — If check_project_exists shows an existing deployment, do NOT overwrite it. Use ask_user to let the user choose: backup+overwrite, keep existing, or abort.
4. **NEVER run destructive commands** — rm, mv, chmod, chown, kill, shutdown, reboot, mkfs, dd, and sudo are dangerous. run_shell will automatically ask the user for confirmation before executing them — you don't need to call ask_user for this. If the user denies, accept it and move on.
5. **Always confirm warnings** — If ANY pre-flight check returns a WARNING (not just an error), STOP and use ask_user before proceeding.
6. **NEVER retry ask_user** — If ask_user returns an error or timeout, do NOT call it again. Tell the user you didn't get a response and wait for a new message.
7. **Preserve existing infrastructure** — If nginx or caddy is already configured for other sites, integrate the new config without breaking existing ones. Use run_ssh to check existing configs first.

## Rules
- Always check SSH credentials before attempting any remote operation
- Always test the SSH connection before running remote commands
- **NEVER use run_shell to run 'ssh' commands** — run_shell is for LOCAL commands only. For ALL remote operations, use run_ssh with a serverId, or use the dedicated tools (setup_pm2, check_web_server, etc.)
- **Dangerous local commands are BLOCKED in run_shell** — you MUST use ask_user to get explicit permission. See SAFETY RULES above.
- When unsure, ask the user — never assume
- For frontend projects (React, Next.js, Vue): env vars ARE needed at build time
- For backend projects (Express, FastAPI, Go): env vars are for RUNTIME only, pass them to start_pm2 as envVars
- Build locally for VPS compatibility (same arch — check with test_connection)
- Be concise and actionable
- **Stop and provide your final answer as soon as you have enough information** — don't keep calling tools unnecessarily. After each tool result, ask yourself: "do I have enough to give the user a useful answer?" If yes, respond now
- If something fails, explain what went wrong and suggest next steps
- The user may not have a domain yet — if they don't, configure with the server IP as the server_name`

// DefaultContextWindow is the default max context window for models.
const DefaultContextWindow = 200_000

// modelContextWindows maps model prefixes to their max context window sizes.
var modelContextWindows = map[string]int{
	"gpt-4o":         128_000,
	"gpt-4":          128_000,
	"gpt-3.5":        128_000,
	"claude":         200_000,
	"deepseek":       200_000,
	"deepseek-chat":  200_000,
	"deepseek-reasoner": 200_000,
	"gemini":         1_000_000,
	"llama":          128_000,
	"mistral":        128_000,
	"qwen":           128_000,
	"command-r":      128_000,
}

// SessionUsage tracks cumulative token usage for a session.
type SessionUsage struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
	CacheHitTokens   int `json:"cacheHitTokens"`
	CacheMissTokens  int `json:"cacheMissTokens"`
	ContextWindow    int `json:"contextWindow"`
}

// CacheHitRate returns the cache hit percentage (0-100).
func (u *SessionUsage) CacheHitRate() float64 {
	total := u.CacheHitTokens + u.CacheMissTokens
	if total == 0 {
		return 0
	}
	return float64(u.CacheHitTokens) / float64(total) * 100
}

// ContextUsagePercent returns the percentage of the context window used.
func (u *SessionUsage) ContextUsagePercent() float64 {
	if u.ContextWindow == 0 {
		return 0
	}
	return float64(u.PromptTokens) / float64(u.ContextWindow) * 100
}

// ContextWindowForModel returns the known context window for a model name,
// or the default if unknown.
func ContextWindowForModel(model string) int {
	// Check exact match first
	if w, ok := modelContextWindows[model]; ok {
		return w
	}
	// Check prefix match
	for prefix, w := range modelContextWindows {
		if strings.HasPrefix(model, prefix) {
			return w
		}
	}
	return DefaultContextWindow
}

// AgentRunner manages the agent conversation loop.
type AgentRunner struct {
	client   *Client
	registry *Registry
	runtime  *AgentRuntime
	logger   *AgentLogger
	messages []ChatMessage
	usage    SessionUsage
}

// NewAgentRunner creates a new agent runner with the given API key, base URL, and model.
func NewAgentRunner(apiKey, baseURL, model string, runtime *AgentRuntime) *AgentRunner {
	client := NewClient(apiKey, baseURL, model)
	return &AgentRunner{
		client:   client,
		registry: NewRegistry(),
		runtime:  runtime,
		logger:   &AgentLogger{Session: runtime.Session},
		messages: []ChatMessage{
			{Role: "system", Content: systemPrompt},
		},
	}
}

// Init registers all built-in tools.
func (r *AgentRunner) Init() {
	RegisterBuiltinTools(r.registry, r.runtime)
}

// SetDefaultServerID updates the runtime's default server (called when user changes server dropdown).
func (r *AgentRunner) SetDefaultServerID(id string) {
	r.runtime.DefaultServerID = id
}

// Run executes the agent loop for a user message.
func (r *AgentRunner) Run(ctx context.Context, userMessage string) error {
	r.messages = append(r.messages, ChatMessage{
		Role:    "user",
		Content: userMessage,
	})

	maxIterations := 25
	for i := 0; i < maxIterations; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// If we're near the limit, ask the LLM to wrap up
		if i >= maxIterations-3 {
			r.messages = append(r.messages, ChatMessage{
				Role:    "system",
				Content: fmt.Sprintf("You have %d steps remaining. If you have enough information, provide your final answer now summarizing what was accomplished. Only call tools if absolutely necessary.", maxIterations-i-1),
			})
		}

		tools := r.registry.Definitions()
		resp, err := r.client.Chat(r.messages, tools)
		if err != nil {
			r.logger.LogError(fmt.Errorf("LLM call failed: %w", err))
			return err
		}

		// Accumulate usage
		if resp.Usage != nil {
			r.usage.PromptTokens += resp.Usage.PromptTokens
			r.usage.CompletionTokens += resp.Usage.CompletionTokens
			r.usage.TotalTokens += resp.Usage.TotalTokens
			r.usage.CacheHitTokens += resp.Usage.PromptCacheHitTokens
			r.usage.CacheMissTokens += resp.Usage.PromptCacheMissTokens
		}
		if r.usage.ContextWindow == 0 {
			r.usage.ContextWindow = ContextWindowForModel(r.client.Model)
		}
		r.logger.LogUsage(r.usage)

		if len(resp.Choices) == 0 {
			r.logger.LogError(fmt.Errorf("no choices in response"))
			return fmt.Errorf("no response from LLM")
		}

		msg := resp.Choices[0].Message
		finishReason := resp.Choices[0].FinishReason

		// If the LLM wants to call tools
		if len(msg.ToolCalls) > 0 {
			// Send agent content first so live view matches what history will show
			if strings.TrimSpace(msg.Content) != "" {
				r.logger.LogAgentMessage(msg.Content)
			}

			// Add assistant message with tool calls to history
			r.messages = append(r.messages, msg)

			for _, tc := range msg.ToolCalls {
				args, err := parseToolArgs(tc.Function.Arguments)
				if err != nil {
					r.logger.LogError(fmt.Errorf("bad tool args for %s: %w", tc.Function.Name, err))
					r.messages = append(r.messages, ChatMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf(`{"error": "bad arguments: %v"}`, err),
					})
					continue
				}

				r.logger.LogToolCall(tc.Function.Name, args, tc.ID)

				result, err := r.registry.Execute(ctx, tc.Function.Name, args)
				if err != nil {
					log.Printf("[agent] tool %s execution error: %v", tc.Function.Name, err)
					result = &ToolResult{Success: false, Error: err.Error()}
				}

				r.logger.LogToolResult(tc.Function.Name, result, tc.ID)

				// Serialize result for LLM
				resultJSON, _ := json.Marshal(result)
				r.messages = append(r.messages, ChatMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
					Content:    string(resultJSON),
				})
			}

			// Continue loop — LLM will process tool results
			continue
		}

		// Final response from LLM
		if msg.Content != "" {
			r.logger.LogAgentMessage(msg.Content)
		}
		r.messages = append(r.messages, msg)
		_ = finishReason
		return nil
	}

	r.logger.LogAgentMessage("⚠️ Reached the maximum number of steps (25). The deployment may be partially complete. Type 'continue' to resume, or review the tool outputs above to see what was accomplished.")
	return nil
}

// RunStreaming executes the agent loop with streaming responses.
// Every LLM call is streamed so the user sees content and reasoning
// appear in real-time. Tool-calling iterations also stream text and
// reasoning before the tool calls are executed.
func (r *AgentRunner) RunStreaming(ctx context.Context, userMessage string) error {
	r.messages = append(r.messages, ChatMessage{
		Role:    "user",
		Content: userMessage,
	})

	maxIterations := 25
	for i := 0; i < maxIterations; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// If we're near the limit, ask the LLM to wrap up
		if i >= maxIterations-3 {
			r.messages = append(r.messages, ChatMessage{
				Role:    "system",
				Content: fmt.Sprintf("You have %d steps remaining. If you have enough information, provide your final answer now summarizing what was accomplished. Only call tools if absolutely necessary.", maxIterations-i-1),
			})
		}

		tools := r.registry.Definitions()

		// Stream every iteration -- accumulate content, reasoning, and tool calls
		// from the streaming response.
		var accumulatedContent strings.Builder
		var accumulatedReasoning strings.Builder
		var accumulatedToolCalls []ToolCall
		var lastChunkFinishReason string
		var lastUsage *UsageData

		err := r.client.ChatStream(r.messages, tools, func(chunk StreamChunk) error {
			for _, choice := range chunk.Choices {
				// Stream reasoning in real-time
				if choice.Delta.ReasoningContent != "" {
					accumulatedReasoning.WriteString(choice.Delta.ReasoningContent)
					r.logger.LogReasoningDelta(choice.Delta.ReasoningContent)
				}
				// Stream content in real-time
				if choice.Delta.Content != "" {
					accumulatedContent.WriteString(choice.Delta.Content)
					r.logger.LogContentChunk(choice.Delta.Content)
				}
				// Accumulate tool call deltas (streaming arrives as partial chunks)
				for _, tcd := range choice.Delta.ToolCalls {
					// Ensure we have enough slots
					for len(accumulatedToolCalls) <= tcd.Index {
						accumulatedToolCalls = append(accumulatedToolCalls, ToolCall{})
					}
					tc := &accumulatedToolCalls[tcd.Index]
					if tcd.ID != "" {
						tc.ID = tcd.ID
					}
					if tcd.Type != "" {
						tc.Type = tcd.Type
					}
					if tcd.Function.Name != "" {
						tc.Function.Name = tcd.Function.Name
					}
					if tcd.Function.Arguments != "" {
						tc.Function.Arguments += tcd.Function.Arguments
					}
				}
				if choice.FinishReason != "" {
					lastChunkFinishReason = choice.FinishReason
				}
			}
			if chunk.Usage != nil {
				lastUsage = chunk.Usage
			}
			return nil
		})
		if err != nil {
			r.logger.LogError(fmt.Errorf("LLM call failed: %w", err))
			return err
		}

		// Log the request and response summaries
		log.Printf("[llm] ── request (iter) ─────────────────────────")
		log.Printf("[llm] model=%s  msgs=%d  tools=%d  last_role=%s", r.client.Model, len(r.messages), len(tools), r.messages[len(r.messages)-1].Role)
		log.Printf("[llm] ── response ──────────────────────────────")
		log.Printf("[llm] finish_reason: %s", lastChunkFinishReason)
		contentStr := accumulatedContent.String()
		reasoningStr := accumulatedReasoning.String()
		if contentStr != "" {
			log.Printf("[llm] content: %s", contentStr)
		}
		if reasoningStr != "" {
			log.Printf("[llm] reasoning: %s", reasoningStr)
		}
		for _, tc := range accumulatedToolCalls {
			if tc.Function.Name != "" {
				log.Printf("[llm] tool_call: %s(%s)", tc.Function.Name, tc.Function.Arguments)
			}
		}

		// Accumulate usage from the final stream chunk
		if lastUsage != nil {
			r.usage.PromptTokens += lastUsage.PromptTokens
			r.usage.CompletionTokens += lastUsage.CompletionTokens
			r.usage.TotalTokens += lastUsage.TotalTokens
			r.usage.CacheHitTokens += lastUsage.PromptCacheHitTokens
			r.usage.CacheMissTokens += lastUsage.PromptCacheMissTokens
		}
		if r.usage.ContextWindow == 0 {
			r.usage.ContextWindow = ContextWindowForModel(r.client.Model)
		}
		r.logger.LogUsage(r.usage)
		if lastUsage != nil {
			hitRate := 0.0
			if lastUsage.PromptTokens > 0 {
				hitRate = float64(lastUsage.PromptCacheHitTokens) / float64(lastUsage.PromptTokens) * 100
			}
			log.Printf("[llm] usage: ↑%d ↓%d total=%d cache_hit=%d cache_miss=%d hit_rate=%.1f%%",
				lastUsage.PromptTokens, lastUsage.CompletionTokens, lastUsage.TotalTokens,
				lastUsage.PromptCacheHitTokens, lastUsage.PromptCacheMissTokens, hitRate)
		}
		log.Printf("[llm] ───────────────────────────────────────────")

		// Remove empty/partial tool calls (those with no name)
		var toolCalls []ToolCall
		for _, tc := range accumulatedToolCalls {
			if tc.Function.Name != "" {
				toolCalls = append(toolCalls, tc)
			}
		}

		// Build the assistant message
		msg := ChatMessage{
			Role:      "assistant",
			Content:   contentStr,
			ToolCalls: toolCalls,
		}

		// If the LLM wants to call tools
		if len(toolCalls) > 0 {
			// Send the full content as an agent_message (finalizes the temp streaming bubble)
			if contentStr != "" {
				r.logger.LogAgentMessage(contentStr)
			}

			// Add assistant message with tool calls to history
			r.messages = append(r.messages, msg)

			for _, tc := range toolCalls {
				args, err := parseToolArgs(tc.Function.Arguments)
				if err != nil {
					r.logger.LogError(fmt.Errorf("bad tool args for %s: %w", tc.Function.Name, err))
					r.messages = append(r.messages, ChatMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf(`{"error": "bad arguments: %v"}`, err),
					})
					continue
				}

				r.logger.LogToolCall(tc.Function.Name, args, tc.ID)

				result, err := r.registry.Execute(ctx, tc.Function.Name, args)
				if err != nil {
					log.Printf("[agent] tool %s execution error: %v", tc.Function.Name, err)
					result = &ToolResult{Success: false, Error: err.Error()}
				}

				r.logger.LogToolResult(tc.Function.Name, result, tc.ID)

				resultJSON, _ := json.Marshal(result)
				r.messages = append(r.messages, ChatMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
					Content:    string(resultJSON),
				})
			}

			// Continue loop -- LLM will process tool results in the next iteration
			continue
		}

		// Final response from LLM -- content was already streamed
		if contentStr != "" {
			r.logger.LogAgentMessage(contentStr)
		}
		r.messages = append(r.messages, msg)
		_ = lastChunkFinishReason
		return nil
	}

	r.logger.LogAgentMessage("⚠️ Reached the maximum number of steps (25). The deployment may be partially complete. Type 'continue' to resume, or review the tool outputs above to see what was accomplished.")
	return nil
}

// GetMessages returns the full conversation history.
func (r *AgentRunner) GetMessages() []ChatMessage {
	return r.messages
}

// SetMessages restores conversation history (for loading previous sessions).
// Preserves the system prompt at position 0.
func (r *AgentRunner) SetMessages(msgs []ChatMessage) {
	r.messages = []ChatMessage{
		{Role: "system", Content: systemPrompt},
	}
	r.messages = append(r.messages, msgs...)
}

// ClearHistory resets the conversation but keeps the system prompt.
func (r *AgentRunner) ClearHistory() {
	r.messages = []ChatMessage{
		{Role: "system", Content: systemPrompt},
	}
}

// WaitForChoice is implemented by wsSession — blocks until user responds.
func WaitForChoice(sess Session, timeout time.Duration) (string, error) {
	return sess.WaitForChoice(timeout)
}
