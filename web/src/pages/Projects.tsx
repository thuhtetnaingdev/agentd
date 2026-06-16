import { useState, useEffect, useRef, useCallback } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { api } from "../lib/api";
import { useWebSocket, WSMessage } from "../hooks/useWebSocket";
import {
  FolderOpen,
  Send,
  Loader2,
  Bot,
  User,
  Check,
  X,
  AlertTriangle,
  ArrowLeft,
  MessageSquare,
  Plus,
  Trash2,
  ChevronLeft,
  Server,
  Square,
  Wrench,
} from "lucide-react";
import Markdown from "../components/Markdown";

// --- Types ---

interface ChatMessage {
  id: string;
  role: "user" | "agent" | "tool" | "tool_call" | "choice";
  content: string;
  toolName?: string;
  toolCallId?: string;
  toolStatus?: "running" | "success" | "error";
  toolError?: string;
  choices?: { id: string; title: string }[];
  timestamp: Date;
}

interface ProjectData {
  id: string;
  name: string;
  type: string;
  frameworks: string[];
  hasDocker: boolean;
}

interface SessionData {
  id: string;
  projectId: string;
  name: string;
  createdAt: string;
  updatedAt: string;
}

// --- Main Component ---

export default function Projects() {
  const { id } = useParams();
  const navigate = useNavigate();

  const [project, setProject] = useState<ProjectData | null>(null);
  const [sessions, setSessions] = useState<SessionData[]>([]);
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([
    {
      id: "welcome",
      role: "agent",
      content:
        "Hello! I'm your DevOps agent. I can help you deploy this project.\n\nTry saying:\n• \"prepare to deploy\"\n• \"analyze this project\"\n• \"check my servers\"",
      timestamp: new Date(),
    },
  ]);
  const [input, setInput] = useState("");
  const [agentThinking, setAgentThinking] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [servers, setServers] = useState<{ id: string; name: string; host: string }[]>([]);
  const [selectedServer, setSelectedServer] = useState<string>("");
  const messagesEndRef = useRef<HTMLDivElement>(null);

  // Auto-pick project
  useEffect(() => {
    if (id) {
      api.getProject(id).then(setProject).catch(() => setProject(null));
    } else {
      api.listProjects().then((projects) => {
        if (projects.length > 0) {
          navigate(`/projects/${projects[0].id}`, { replace: true });
        }
      }).catch(() => {});
    }
  }, [id]);

  // Load servers
  useEffect(() => {
    api.listServers().then((srvs) => {
      setServers(srvs);
      if (srvs.length > 0 && !selectedServer) {
        setSelectedServer(srvs[0].id);
      }
    }).catch(() => {});
  }, []);

  // Load sessions for this project
  const loadSessions = useCallback(() => {
    if (!id) return;
    api.listSessions(id).then(setSessions).catch(() => {});
  }, [id]);

  useEffect(() => {
    loadSessions();
  }, [loadSessions]);

  // Handle WebSocket messages
  const handleMessage = useCallback((msg: WSMessage) => {
    const addMessage = (m: ChatMessage) => {
      setMessages((prev) => [...prev, m]);
    };

    switch (msg.type) {
      case "agent_message":
        setAgentThinking(false);
        if (msg.payload?.content) {
          addMessage({
            id: crypto.randomUUID(),
            role: "agent",
            content: msg.payload.content,
            timestamp: new Date(),
          });
        }
        break;
      case "tool_call":
        addMessage({
          id: crypto.randomUUID(),
          role: "tool",
          content: msg.payload?.description || "",
          toolName: msg.payload?.toolName,
          toolCallId: msg.payload?.toolCallId,
          toolStatus: "running",
          timestamp: new Date(),
        });
        break;
      case "tool_result":
        setMessages((prev) =>
          prev.map((m) =>
            m.role === "tool" &&
            m.toolStatus === "running" &&
            (msg.payload?.toolCallId
              ? m.toolCallId === msg.payload.toolCallId
              : m.toolName === msg.payload?.toolName)
              ? {
                  ...m,
                  toolStatus: msg.payload?.success ? "success" : "error",
                  content: msg.payload?.output || m.content,
                  toolError: msg.payload?.error || undefined,
                }
              : m
          )
        );
        break;
      case "choice_request":
        addMessage({
          id: crypto.randomUUID(),
          role: "choice",
          content: msg.payload?.prompt || "",
          choices: msg.payload?.choices || [],
          timestamp: new Date(),
        });
        break;
      case "agent_error":
        setAgentThinking(false);
        addMessage({
          id: crypto.randomUUID(),
          role: "agent",
          content: `❌ ${msg.payload?.message || "Something went wrong"}`,
          timestamp: new Date(),
        });
        break;
      case "agent_cancelled":
        setAgentThinking(false);
        addMessage({
          id: crypto.randomUUID(),
          role: "agent",
          content: "⏹️ Agent stopped.",
          timestamp: new Date(),
        });
        break;
    }
  }, []);

  const { send } = useWebSocket(id || null, activeSessionId, handleMessage);

  // Auto-scroll
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  // Send message
  const handleSend = () => {
    if (!input.trim() || agentThinking) return;

    const userMsg: ChatMessage = {
      id: crypto.randomUUID(),
      role: "user",
      content: input.trim(),
      timestamp: new Date(),
    };
    setMessages((prev) => [...prev, userMsg]);
    setInput("");
    setAgentThinking(true);

    send({
      type: "chat",
      payload: { message: userMsg.content, projectId: id, serverId: selectedServer, sessionId: activeSessionId },
    });
  };

  const handleCancel = () => {
    send({ type: "cancel", payload: {} });
    setAgentThinking(false);
  };

  const handleChoice = (choiceId: string) => {
    send({
      type: "choice_response",
      payload: { choiceId, projectId: id },
    });
    setMessages((prev) =>
      prev.map((m) =>
        m.role === "choice"
          ? { ...m, choices: undefined } // keep question text, just remove buttons
          : m
      )
    );
  };

  // New session
  const handleNewSession = async () => {
    if (!id) return;
    try {
      const swm = await api.createSession(id);
      setActiveSessionId(swm.id);
      setMessages([
        {
          id: "welcome",
          role: "agent",
          content:
            "Hello! I'm your DevOps agent. I can help you deploy this project.\n\nTry saying:\n• \"prepare to deploy\"\n• \"analyze this project\"\n• \"check my servers\"",
          timestamp: new Date(),
        },
      ]);
      loadSessions();
    } catch (e) {
      console.error(e);
    }
  };

  // Load session history
  const handleSelectSession = async (sessionId: string) => {
    if (!id) return;
    setActiveSessionId(sessionId);
    try {
      const swm = await api.getSession(id, sessionId);
      const loaded: ChatMessage[] = swm.messages
        .filter((m: any) => {
          // Skip empty agent messages (LLM filler between tool calls)
          if ((m.role === "agent" || m.role === "assistant") && !m.content) return false;
          return true;
        })
        .map((m: any, i: number) => {
          let role = m.role === "assistant" ? "agent" : (m.role as ChatMessage["role"]);
          let content = m.content;
          let toolName = m.toolName;
          let toolStatus: "success" | "error" | undefined = undefined;

          // Handle tool_call messages (stored arguments)
          if (m.role === "tool_call") {
            role = "tool_call";
            content = m.content; // already formatted as "toolName(args)" by server
          }

          // Handle tool messages
          if (m.role === "tool") {
            // ask_user messages should show as Q&A, not as tool bubbles
            if (m.toolName === "ask_user") {
              if (content && content.includes("User selected:")) {
                const parts = content.split("User selected:");
                const question = parts[0].replace("Question: ", "").trim();
                return {
                  id: `hist_${i}`,
                  role: "choice",
                  content: question,
                  timestamp: new Date(),
                };
              }
              if (content && content.includes("Question:")) {
                // Timeout or error — show the question with a note
                const question = content.replace("Question: ", "").split("\n")[0].trim();
                return {
                  id: `hist_${i}`,
                  role: "choice",
                  content: question + "\n\n⚠️ No response (timed out)",
                  timestamp: new Date(),
                };
              }
              // Empty/error — skip entirely
              return null;
            }
            if (toolName) {
              toolStatus = m.isError ? "error" : "success";
            } else if (content && content.startsWith("{")) {
              try {
                const parsed = JSON.parse(content);
                if (parsed.output !== undefined) {
                  content = parsed.output;
                  toolStatus = parsed.success !== false ? "success" : "error";
                }
              } catch {}
            }
          }

          return {
            id: `hist_${i}`,
            role,
            content,
            toolName,
            toolCallId: m.toolCallId,
            toolStatus,
            toolError: m.errorDetail || undefined,
            timestamp: new Date(),
          };
        })
        .filter(Boolean) as ChatMessage[];

      // Merge adjacent tool_call + tool pairs into single "tool" messages
      // (This makes history display identical to the live WebSocket flow where
      //  tool_call creates a running tool, then tool_result updates it in place.)
      const merged: ChatMessage[] = [];
      for (let i = 0; i < loaded.length; i++) {
        const curr = loaded[i];
        if (curr.role === "tool_call" && i + 1 < loaded.length) {
          const next = loaded[i + 1];
          if (next.role === "tool" &&
            (next.toolCallId ? next.toolCallId === curr.toolCallId : next.toolName === curr.toolName)) {
            merged.push({
              ...next,
              id: curr.id,
              content: next.content || curr.content || "",
              toolCallId: next.toolCallId || (curr as any).toolCallId,
              toolStatus: next.toolStatus || "success",
            });
            i++; // consume the tool result
            continue;
          }
        }
        merged.push(curr);
      }

      setMessages(merged.length > 0 ? merged : [
        {
          id: "welcome",
          role: "agent",
          content: "Hello! How can I help you deploy this project?",
          timestamp: new Date(),
        },
      ]);
    } catch (e) {
      console.error(e);
    }
  };

  // Delete session
  const handleDeleteSession = async (sessionId: string, e: React.MouseEvent) => {
    e.stopPropagation();
    if (!id) return;
    await api.deleteSession(id, sessionId);
    if (activeSessionId === sessionId) {
      setActiveSessionId(null);
      setMessages([{
        id: "welcome",
        role: "agent",
        content: "Hello! I'm your DevOps agent. Select or create a session to start.",
        timestamp: new Date(),
      }]);
    }
    loadSessions();
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  const typeLabel = (t: string) => {
    switch (t) {
      case "node": return "Node.js";
      case "go": return "Go";
      case "python": return "Python";
      default: return t;
    }
  };

  return (
    <div className="h-full flex bg-background">
      {/* Session sidebar */}
      <div
        className={`${
          sidebarOpen ? "w-60" : "w-0"
        } border-r border-border bg-card flex flex-col transition-all duration-200 overflow-hidden flex-shrink-0`}
      >
        {/* Sidebar header */}
        <div className="p-3 border-b border-border flex items-center justify-between">
          <div className="flex items-center gap-2 min-w-0">
            <MessageSquare className="w-4 h-4 text-primary flex-shrink-0" />
            <span className="text-xs font-semibold truncate">Chats</span>
          </div>
          <button
            onClick={handleNewSession}
            className="p-1 rounded-md hover:bg-secondary text-muted-foreground hover:text-foreground transition-colors flex-shrink-0"
            title="New chat"
          >
            <Plus className="w-4 h-4" />
          </button>
        </div>

        {/* Session list */}
        <div className="flex-1 overflow-y-auto p-2 space-y-1">
          {sessions.length === 0 && (
            <p className="text-[11px] text-muted-foreground text-center py-8 px-2">
              No chats yet. Click + to start.
            </p>
          )}
          {sessions.map((s) => (
            <button
              key={s.id}
              onClick={() => handleSelectSession(s.id)}
              className={`w-full text-left px-3 py-2 rounded-lg text-xs transition-colors group flex items-center gap-2 ${
                activeSessionId === s.id
                  ? "bg-primary/10 text-primary"
                  : "text-muted-foreground hover:text-foreground hover:bg-secondary"
              }`}
            >
              <MessageSquare className="w-3 h-3 flex-shrink-0" />
              <span className="truncate flex-1">{s.name}</span>
              <button
                onClick={(e) => handleDeleteSession(s.id, e)}
                className="p-0.5 rounded opacity-0 group-hover:opacity-100 hover:bg-destructive/10 hover:text-destructive transition-all flex-shrink-0"
                title="Delete chat"
              >
                <Trash2 className="w-3 h-3" />
              </button>
            </button>
          ))}
        </div>
      </div>

      {/* Toggle sidebar */}
      <button
        onClick={() => setSidebarOpen(!sidebarOpen)}
        className="flex-shrink-0 w-5 border-r border-border bg-card flex items-center justify-center hover:bg-secondary transition-colors"
      >
        <ChevronLeft
          className={`w-3 h-3 text-muted-foreground transition-transform ${
            sidebarOpen ? "" : "rotate-180"
          }`}
        />
      </button>

      {/* Main chat area */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Header */}
        <header className="flex-shrink-0 h-12 border-b border-border bg-card flex items-center px-4 gap-3">
          <button
            onClick={() => navigate("/projects")}
            className="p-1 text-muted-foreground hover:text-foreground rounded transition-colors"
          >
            <ArrowLeft className="w-4 h-4" />
          </button>
          <FolderOpen className="w-4 h-4 text-primary flex-shrink-0" />
          <span className="text-sm font-semibold truncate">
            {project?.name || id || "Project"}
          </span>

          {/* Server selector */}
          {servers.length > 0 && (
            <div className="flex items-center gap-1.5 ml-2">
              <Server className="w-3 h-3 text-muted-foreground flex-shrink-0" />
              <select
                value={selectedServer}
                onChange={(e) => setSelectedServer(e.target.value)}
                className="bg-input border border-border rounded-md px-2 py-1 text-xs focus:outline-none focus:ring-1 focus:ring-ring appearance-none cursor-pointer pr-5"
                style={{
                  backgroundImage: `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='8' height='8' viewBox='0 0 8 8'%3E%3Cpath fill='%23666' d='M0 2l4 4 4-4z'/%3E%3C/svg%3E")`,
                  backgroundRepeat: "no-repeat",
                  backgroundPosition: "right 6px center",
                }}
              >
                {servers.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name} ({s.host})
                  </option>
                ))}
              </select>
            </div>
          )}

          {project && (
            <span className="text-[10px] text-muted-foreground bg-secondary px-2 py-0.5 rounded-full ml-auto">
              {typeLabel(project.type)}
            </span>
          )}
        </header>

        {/* Messages */}
        <div className="flex-1 overflow-y-auto px-4 py-4 space-y-4">
          {messages.map((msg) => (
            <MessageBubble key={msg.id} msg={msg} onChoice={handleChoice} />
          ))}

          {agentThinking && (
            <div className="flex gap-3">
              <div className="w-7 h-7 rounded-full bg-green-500/10 flex items-center justify-center flex-shrink-0">
                <Loader2 className="w-3.5 h-3.5 text-green-500 animate-spin" />
              </div>
              <div className="text-xs text-muted-foreground py-1">Thinking...</div>
            </div>
          )}

          <div ref={messagesEndRef} />
        </div>

        {/* Input */}
        <div className="flex-shrink-0 border-t border-border bg-card p-4">
          <div className="flex gap-2 max-w-3xl mx-auto">
            <input
              type="text"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="Type a message... (e.g. 'prepare to deploy')"
              className="flex-1 bg-input border border-border rounded-lg px-4 py-2.5 text-sm placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring transition-colors"
              disabled={agentThinking}
              autoFocus
            />
            {agentThinking ? (
              <button
                onClick={handleCancel}
                className="px-4 py-2.5 bg-destructive text-destructive-foreground rounded-lg text-sm flex items-center gap-2 hover:bg-destructive/90 transition-all font-medium flex-shrink-0"
              >
                <Square className="w-4 h-4" />
                Stop
              </button>
            ) : (
              <button
                onClick={handleSend}
                disabled={!input.trim()}
                className="px-4 py-2.5 bg-primary text-primary-foreground rounded-lg text-sm flex items-center gap-2 hover:bg-primary/90 disabled:opacity-40 transition-all font-medium flex-shrink-0"
              >
                <Send className="w-4 h-4" />
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// --- Message Bubble ---

function MessageBubble({
  msg,
  onChoice,
}: {
  msg: ChatMessage;
  onChoice: (id: string) => void;
}) {
  if (msg.role === "tool") {
    return (
      <div className="flex gap-3 pl-2">
        <div
          className={`w-7 h-7 rounded-full flex items-center justify-center flex-shrink-0 mt-0.5 ${
            msg.toolStatus === "running"
              ? "bg-yellow-500/10"
              : msg.toolStatus === "success"
              ? "bg-green-500/10"
              : "bg-red-500/10"
          }`}
        >
          {msg.toolStatus === "running" ? (
            <Loader2 className="w-3.5 h-3.5 text-yellow-500 animate-spin" />
          ) : msg.toolStatus === "success" ? (
            <Check className="w-3.5 h-3.5 text-green-500" />
          ) : (
            <X className="w-3.5 h-3.5 text-red-500" />
          )}
        </div>
        <div className="min-w-0 flex-1">
          <p className="text-[11px] text-muted-foreground font-medium mb-1">
            {msg.toolName}
          </p>
          <div className="bg-secondary/50 border border-border rounded-lg px-3 py-2">
            <p className="text-xs text-muted-foreground font-mono whitespace-pre-wrap break-all max-h-32 overflow-y-auto">
              {msg.content.length > 500
                ? msg.content.slice(0, 500) + "..."
                : msg.content}
            </p>
          </div>
        </div>
      </div>
    );
  }

  if (msg.role === "tool_call") {
    return (
      <div className="flex gap-3 pl-2">
        <div className="w-7 h-7 rounded-full bg-purple-500/10 flex items-center justify-center flex-shrink-0 mt-0.5">
          <Wrench className="w-3.5 h-3.5 text-purple-500" />
        </div>
        <div className="min-w-0 flex-1">
          <p className="text-[11px] text-muted-foreground font-medium mb-1">
            📞 Call: {msg.toolName}
          </p>
          <div className="bg-purple-500/5 border border-purple-500/20 rounded-lg px-3 py-2">
            <p className="text-xs text-muted-foreground font-mono whitespace-pre-wrap break-all">
              {msg.content}
            </p>
          </div>
        </div>
      </div>
    );
  }

  if (msg.role === "choice") {
    return (
      <div className="flex gap-3">
        <div className="w-7 h-7 rounded-full bg-blue-500/10 flex items-center justify-center flex-shrink-0 mt-0.5">
          <AlertTriangle className="w-3.5 h-3.5 text-blue-500" />
        </div>
        <div className="min-w-0 flex-1 max-w-[85%]">
          <p className="text-[11px] text-muted-foreground font-medium mb-1">
            Agent asks
          </p>
          <div className="bg-blue-500/5 border border-blue-500/20 rounded-lg px-4 py-3">
            <p className="text-sm whitespace-pre-wrap break-words mb-3">
              {msg.content}
            </p>
            {msg.choices && msg.choices.length > 0 && (
              <div className="flex flex-wrap gap-2">
                {msg.choices.map((c) => (
                  <button
                    key={c.id}
                    onClick={() => onChoice(c.id)}
                    className="px-4 py-2 bg-primary text-primary-foreground rounded-lg text-sm font-medium hover:bg-primary/90 transition-colors"
                  >
                    {c.title}
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    );
  }

  const isAgent = msg.role === "agent";

  return (
    <div className={`flex gap-3 ${isAgent ? "" : "justify-end"}`}>
      {isAgent && (
        <div className="w-7 h-7 rounded-full bg-green-500/10 flex items-center justify-center flex-shrink-0 mt-0.5">
          <Bot className="w-3.5 h-3.5 text-green-500" />
        </div>
      )}

      <div className={`min-w-0 max-w-[85%] ${isAgent ? "" : "order-first"}`}>
        <p
          className={`text-[11px] font-medium mb-1 ${
            isAgent ? "text-muted-foreground" : "text-right text-primary/70"
          }`}
        >
          {isAgent ? "Agent" : "You"}
        </p>
        <div
          className={`rounded-xl px-4 py-3 text-sm leading-relaxed ${
            isAgent
              ? "bg-card border border-border"
              : "bg-primary text-primary-foreground"
          }`}
        >
          {isAgent ? (
            <Markdown content={msg.content} />
          ) : (
            <div className="whitespace-pre-wrap break-words">{msg.content}</div>
          )}
        </div>
      </div>

      {!isAgent && (
        <div className="w-7 h-7 rounded-full bg-primary/20 flex items-center justify-center flex-shrink-0 mt-0.5">
          <User className="w-3.5 h-3.5 text-primary" />
        </div>
      )}
    </div>
  );
}
