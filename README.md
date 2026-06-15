# agentd

**AI-powered DevOps agent** — a self-hosted web dashboard that deploys your projects to VPS servers using any OpenAI-compatible LLM.

Point it at your project, add a VPS server, and type "deploy" — the agent handles framework detection, dependency installation, building, rsync deployment, PM2 process management, Nginx configuration, and SSL certificates.

## Features

- **Any LLM provider** — works with OpenAI, Anthropic, DeepSeek, Groq, OpenRouter, or any OpenAI-compatible API. Configure base URL and model name in Settings.
- **[models.dev](https://models.dev) integration** — browse and select providers and models directly from the Settings page using live models.dev data.
- **Encrypted secrets** — API keys, server passwords, and environment variables are encrypted at rest with AES-256-GCM.
- **Production env vars** — manually configure environment variables in the UI; the agent injects them at deploy time (both as PM2 `--env` flags and as a `.env` file on the server).
- **Pre-flight safety checks** — verifies SSH, disk space, port availability, runtime versions, and existing deployments before making changes.
- **Multiple frameworks** — detects Node.js, Go, Python, Docker, and their package managers (npm, pnpm, yarn).
- **WebSocket streaming** — real-time tool call and response streaming in the chat UI.
- **Session persistence** — chat history saved per-project in `.agentd/sessions/`.

## Quick start

```bash
# Clone and build
git clone https://github.com/your-org/agentd.git
cd agentd
make all          # builds frontend + Go binary

# Or just build the Go binary (if web-dist/ is already present)
make build

# Run
./agentd --dir /path/to/your/project --port 3001
```

Open `http://localhost:3001`, then:

1. **Settings** → add your API key, pick a provider from models.dev (or enter a custom base URL), and select a model
2. **Servers** → add your VPS (host, port, username, password or key)
3. **Project** → select your project
4. **Chat** → type `prepare to deploy`

## How it works

```
┌──────────────┐     WebSocket      ┌──────────────┐    OpenAI API    ┌───────────┐
│  React SPA   │ ◄────────────────► │  Go backend   │ ◄─────────────► │  LLM      │
│  (web/)      │   JSON messages    │  (agent loop) │  chat/completions│  (any)    │
└──────────────┘                    └──────┬────────┘                 └───────────┘
                                          │
                                          │ SSH (native Go)
                                          ▼
                                   ┌──────────────┐
                                   │  Your VPS    │
                                   │  • PM2       │
                                   │  • Nginx     │
                                   │  • SSL       │
                                   │  • /var/www  │
                                   └──────────────┘
```

1. The LLM receives a system prompt with 20+ built-in tools (check SSH, run shell, analyze project, deploy rsync, setup PM2, configure Nginx, etc.)
2. It decides which tools to call, the Go backend executes them, and results feed back to the LLM
3. The LLM continues until the deployment is complete or it needs user input (`ask_user`)

## Configuration

### API provider (Settings page)

| Field | Description |
|-------|-------------|
| **API Key** | Your provider's API key (encrypted at rest) |
| **Provider** | Browse models.dev or pick from featured providers |
| **API Base URL** | OpenAI-compatible endpoint (`/chat/completions` appended automatically) |
| **Model** | Model name (free text with provider model suggestions) |

### Servers

Add one or more VPS servers with SSH credentials. Passwords are encrypted at rest.

### Environment variables (Settings page)

Add key-value pairs for production. Values are encrypted in `.agentd/env.json`. The agent's `get_env_vars` tool reads them at deploy time and injects them into `build_project` or `start_pm2`.

## Deployment workflow

The agent follows this process:

1. **Check credentials** — verify a server is configured
2. **Test connection** — verify SSH works
3. **Analyze project** — detect framework, build commands, env files
4. **Pre-flight checks** — disk space, port availability, runtime versions, existing deployments
5. **Propose plan** — summarize findings, ask clarifying questions
6. **Get env vars** — collect production values (or use pre-configured ones)
7. **Set up infrastructure** — install Node.js/PM2 or Docker
8. **Install deps** — locally
9. **Build** — locally (with env vars for frontend projects)
10. **Deploy** — rsync to `/var/www/<project>`
11. **Start** — PM2 (or Docker Compose), with `.env` file + inline env vars
12. **Configure domain** — Nginx virtual host (static or reverse proxy)
13. **SSL** — Let's Encrypt via Certbot

## Development

```bash
# Install frontend dependencies
make install-web

# Run Go backend in dev mode
make run

# Build frontend separately
make build-web

# Run tests
make test

# Clean
make clean
```

### Project structure

```
cmd/agentd/          CLI entry point (Cobra)
internal/
  agent/             LLM client, runner loop, tool registry, system prompt
  config/            Config, server passwords, env store (all encrypted)
  deploy/            SSH, rsync, PM2, Docker, Nginx, SSL
  project/           Framework detection (Node, Go, Python, Docker)
  server/            HTTP + WebSocket server, REST API, embedded SPA
  store/             Session persistence
web/                 React + Vite + Tailwind SPA
  src/
    components/      Chat, Layout
    hooks/           useWebSocket
    lib/             API client
    pages/           Dashboard, Settings, Servers, Projects
```

### Storage layout

```
~/.agentd/
  config.yaml        Global settings + servers (passwords encrypted)

<project>/.agentd/
  env.json           Environment variables (encrypted)
  sessions/          Chat history (JSON per session)
```

## Requirements

- **Go** 1.25+
- **Node.js** 20+ (for frontend build)
- **An OpenAI-compatible API key** (OpenAI, Anthropic, DeepSeek, Groq, OpenRouter, etc.)
- **A VPS** with SSH access (for deployment)

## License

MIT
