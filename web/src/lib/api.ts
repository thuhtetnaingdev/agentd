const API_BASE = "/api";

// Simple in-memory cache with TTL for GET requests.
const cache = new Map<string, { data: any; expiry: number }>();
const inflight = new Map<string, Promise<any>>();

const TTL: Record<string, number> = {
  default: 10_000,       // 10s
  "/stats": 5_000,       // 5s — Dashboard refreshes often
  "/servers": 30_000,    // 30s — rarely changes
  "/settings": 30_000,   // 30s
  "/env": 30_000,        // 30s
  "/projects": 15_000,   // 15s — directory scan, moderately expensive
};

function getCacheKey(path: string): string {
  return path;
}

function isGetRequest(options?: RequestInit): boolean {
  return !options || !options.method || options.method === "GET";
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const key = getCacheKey(path);

  // Only cache GET requests
  if (isGetRequest(options)) {
    // Check in-memory cache
    const entry = cache.get(key);
    if (entry && Date.now() < entry.expiry) {
      return entry.data as T;
    }

    // Deduplicate in-flight requests
    const pending = inflight.get(key);
    if (pending) {
      return pending as Promise<T>;
    }
  }

  const promise = doFetch<T>(path, options);

  if (isGetRequest(options)) {
    inflight.set(key, promise);
    promise.finally(() => inflight.delete(key));
  } else {
    // Invalidate cache on mutations
    invalidateRelated(path);
  }

  return promise;
}

async function doFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || res.statusText);
  }
  if (res.status === 204) return undefined as T;
  const data = await res.json();

  // Store in cache for GET requests
  if (isGetRequest(options)) {
    const ttl = TTL[path] ?? TTL.default;
    cache.set(getCacheKey(path), { data, expiry: Date.now() + ttl });
  }

  return data;
}

// Invalidate cache entries affected by a mutation.
function invalidateRelated(path: string) {
  const root = path.split("/")[1]; // "servers", "projects", etc.
  // Exact match
  cache.forEach((_, key) => {
    if (key.startsWith("/" + root) || key.startsWith("/stats")) {
      cache.delete(key);
    }
  });
}

export const api = {
  // Health
  health: () => request<{ status: string }>("/health"),

  // Settings
  getSettings: () =>
    request<{ apiKey: string; apiBaseUrl: string; model: string }>("/settings"),
  updateSettings: (settings: {
    apiKey: string;
    apiBaseUrl?: string;
    model?: string;
  }) =>
    request<{ apiKey: string; apiBaseUrl: string; model: string }>(
      "/settings",
      {
        method: "PUT",
        body: JSON.stringify(settings),
      }
    ),

  // Servers
  listServers: () =>
    request<
      {
        id: string;
        name: string;
        host: string;
        port: number;
        username: string;
        password: string;
      }[]
    >("/servers"),
  createServer: (srv: {
    name: string;
    host: string;
    port: number;
    username: string;
    password: string;
  }) =>
    request<{
      id: string;
      name: string;
      host: string;
      port: number;
      username: string;
    }>("/servers", { method: "POST", body: JSON.stringify(srv) }),
  updateServer: (
    id: string,
    srv: {
      name: string;
      host: string;
      port: number;
      username: string;
      password?: string;
    }
  ) =>
    request<{
      id: string;
      name: string;
      host: string;
      port: number;
      username: string;
    }>(`/servers/${id}`, { method: "PUT", body: JSON.stringify(srv) }),
  deleteServer: (id: string) =>
    request<void>(`/servers/${id}`, { method: "DELETE" }),

  // Stats
  getStats: () =>
    request<{
      serverCount: number;
      projectCount: number;
      hasAPIKey: boolean;
      workDir: string;
    }>("/stats"),

  // Projects
  listProjects: () =>
    request<
      {
        id: string;
        name: string;
        type: string;
        frameworks: string[];
        hasDocker: boolean;
        envFiles: string[];
      }[]
    >("/projects"),
  getProject: (id: string) =>
    request<{
      id: string;
      name: string;
      type: string;
      frameworks: string[];
      hasDocker: boolean;
      envFiles: string[];
      buildCmd: string;
      startCmd: string;
    }>(`/projects/${id}`),

  // Sessions
  listSessions: (projectId: string) =>
    request<
      {
        id: string;
        projectId: string;
        name: string;
        createdAt: string;
        updatedAt: string;
      }[]
    >(`/sessions?projectId=${encodeURIComponent(projectId)}`),
  createSession: (projectId: string) =>
    request<{
      id: string;
      projectId: string;
      name: string;
      messages: { role: string; content: string }[];
    }>("/sessions", {
      method: "POST",
      body: JSON.stringify({ projectId }),
    }),
  getSession: (projectId: string, sessionId: string) =>
    request<{
      id: string;
      projectId: string;
      name: string;
      messages: {
        role: string;
        content: string;
        toolName?: string;
        toolArgs?: string;
        toolCallId?: string;
        isError?: boolean;
        errorDetail?: string;
        timestamp: string;
      }[];
    }>(`/sessions/${sessionId}?projectId=${encodeURIComponent(projectId)}`),
  deleteSession: (projectId: string, sessionId: string) =>
    request<void>(
      `/sessions/${sessionId}?projectId=${encodeURIComponent(projectId)}`,
      { method: "DELETE" }
    ),

  // Environment variables
  listEnv: () =>
    request<{ key: string; value: string }[]>("/env"),
  updateEnv: (key: string, value: string) =>
    request<{ key: string; value: string }[]>("/env", {
      method: "PUT",
      body: JSON.stringify({ key, value }),
    }),
  deleteEnv: (key: string) =>
    request<void>(`/env/${encodeURIComponent(key)}`, {
      method: "DELETE",
    }),

  // Deployments
  listDeployments: () =>
    request<
      {
        id: string;
        projectName: string;
        serverName: string;
        host: string;
        port: number;
        domain: string;
        status: string;
        healthStatus: string;
        deployedAt: string;
        lastChecked: string;
        error: string;
      }[]
    >("/deployments"),
  getDeployment: (id: string) =>
    request<{
      id: string;
      projectName: string;
      serverName: string;
      host: string;
      port: number;
      domain: string;
      status: string;
      healthStatus: string;
      deployedAt: string;
      lastChecked: string;
      error: string;
    }>(`/deployments/${id}`),
  checkDeploymentHealth: (id: string) =>
    request<{
      id: string;
      projectName: string;
      host: string;
      port: number;
      healthStatus: string;
      lastChecked: string;
      output: string;
    }>(`/deployments/${id}/health`),
  deleteDeployment: (id: string) =>
    request<void>(`/deployments/${id}`, { method: "DELETE" }),
};
