const API_BASE = "/api";

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || res.statusText);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
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
};
