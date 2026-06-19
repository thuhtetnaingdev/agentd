import { useState, useRef, useEffect } from "react";
import { useWebSocket, WSMessage } from "../hooks/useWebSocket";
import { Send, Loader2, Bot, User, Wrench, Check, X, AlertTriangle, Square, BrainCircuit } from "lucide-react";

interface ChatMessage {
  id: string;
  role: "user" | "agent" | "tool" | "tool_call" | "choice";
  content: string;
  reasoning?: string;
  toolName?: string;
  toolCallId?: string;
  toolStatus?: "running" | "success" | "error";
  toolError?: string;
  choices?: { id: string; title: string }[];
  timestamp: Date;
}

interface UsageStats {
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  cacheHitRate: number;
  contextWindow: number;
  contextUsagePercent: number;
}

export default function Chat({ projectId }: { projectId: string }) {
  const [messages, setMessages] = useState<ChatMessage[]>([
    {
      id: "welcome",
      role: "agent",
      content:
        "Hello! I'm your AI-powered DevOps agent. I can help you deploy this project. Try saying:\n\n• \"prepare to deploy\"\n• \"analyze this project\"\n• \"check my servers\"",
      timestamp: new Date(),
    },
  ]);
  const [input, setInput] = useState("");
  const [agentThinking, setAgentThinking] = useState(false);
  const [usage, setUsage] = useState<UsageStats | null>(null);
  const [streamingContent, setStreamingContent] = useState("");
  const [streamingReasoning, setStreamingReasoning] = useState("");
  const [showReasoning, setShowReasoning] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  const handleMessage = (msg: WSMessage) => {
    switch (msg.type) {
      case "chat_ack":
        // Acknowledged — agent is processing
        break;
      case "reasoning_update":
        // Accumulate reasoning content as it arrives
        setStreamingReasoning((prev) => prev + (msg.payload?.content || ""));
        setShowReasoning(true);
        break;
      case "content_chunk":
        // Accumulate content chunks into the streaming message
        setStreamingContent((prev) => prev + (msg.payload?.content || ""));
        break;
      case "agent_message":
        // Finalize the streamed content into a permanent message
        setAgentThinking(false);
        setStreamingContent("");
        setShowReasoning(false);
        // Use accumulated streaming content if available, otherwise payload
        const content = msg.payload?.content || "";
        setMessages((prev) => [
          ...prev,
          {
            id: crypto.randomUUID(),
            role: "agent",
            content: content,
            reasoning: streamingReasoning || undefined,
            timestamp: new Date(),
          },
        ]);
        setStreamingReasoning("");
        break;
      case "tool_call":
        setMessages((prev) => [
          ...prev,
          {
            id: crypto.randomUUID(),
            role: "tool",
            content: msg.payload?.description || "",
            toolName: msg.payload?.toolName,
            toolCallId: msg.payload?.toolCallId,
            toolStatus: "running",
            timestamp: new Date(),
          },
        ]);
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
        setMessages((prev) => [
          ...prev,
          {
            id: crypto.randomUUID(),
            role: "choice",
            content: msg.payload?.prompt || "",
            choices: msg.payload?.choices || [],
            timestamp: new Date(),
          },
        ]);
        break;
      case "agent_error":
        setAgentThinking(false);
        setMessages((prev) => [
          ...prev,
          {
            id: crypto.randomUUID(),
            role: "agent",
            content: `❌ Error: ${msg.payload?.message || "Something went wrong"}`,
            timestamp: new Date(),
          },
        ]);
        break;
      case "agent_cancelled":
        setAgentThinking(false);
        setMessages((prev) => [
          ...prev,
          {
            id: crypto.randomUUID(),
            role: "agent",
            content: "⏹️ Agent stopped.",
            timestamp: new Date(),
          },
        ]);
        break;
      case "usage_update":
        setUsage(msg.payload);
        break;
    }
  };

  const { send } = useWebSocket(projectId, null, handleMessage);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

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
      payload: { message: userMsg.content, projectId },
    });
  };

  const handleChoice = (choiceId: string) => {
    send({
      type: "choice_response",
      payload: { choiceId, projectId },
    });
    // Remove the choice message
    setMessages((prev) =>
      prev.map((m) =>
        m.role === "choice" ? { ...m, content: `Selected: ${choiceId}`, choices: undefined } : m
      )
    );
  };

  const handleCancel = () => {
    send({ type: "cancel", payload: {} });
    setAgentThinking(false);
  };

  const fmtTokens = (n: number) => {
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + "M";
    if (n >= 1_000) return (n / 1_000).toFixed(0) + "k";
    return String(n);
  };

  return (
    <div className="h-full flex flex-col">
      {/* Messages */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {messages.map((msg) => (
          <div key={msg.id} className="flex gap-3">
            {/* Avatar */}
            <div className="flex-shrink-0 mt-0.5">
              {msg.role === "user" && (
                <div className="w-7 h-7 rounded-md bg-primary/10 flex items-center justify-center">
                  <User className="w-3.5 h-3.5 text-primary" />
                </div>
              )}
              {msg.role === "agent" && (
                <div className="w-7 h-7 rounded-md bg-green-500/10 flex items-center justify-center">
                  <Bot className="w-3.5 h-3.5 text-green-500" />
                </div>
              )}
              {msg.role === "tool" && (
                <div className="w-7 h-7 rounded-md bg-yellow-500/10 flex items-center justify-center">
                  {msg.toolStatus === "running" ? (
                    <Loader2 className="w-3.5 h-3.5 text-yellow-500 animate-spin" />
                  ) : msg.toolStatus === "success" ? (
                    <Check className="w-3.5 h-3.5 text-green-500" />
                  ) : (
                    <X className="w-3.5 h-3.5 text-red-500" />
                  )}
                </div>
              )}
              {msg.role === "tool_call" && (
                <div className="w-7 h-7 rounded-md bg-purple-500/10 flex items-center justify-center">
                  <Wrench className="w-3.5 h-3.5 text-purple-500" />
                </div>
              )}
              {msg.role === "choice" && (
                <div className="w-7 h-7 rounded-md bg-blue-500/10 flex items-center justify-center">
                  <AlertTriangle className="w-3.5 h-3.5 text-blue-500" />
                </div>
              )}
            </div>

            {/* Content */}
            <div className="flex-1 min-w-0">
              <div className="text-xs text-muted-foreground mb-1">
                {msg.role === "user"
                  ? "You"
                  : msg.role === "agent"
                  ? "Agent"
                  : msg.role === "tool"
                  ? `Tool: ${msg.toolName}`
                  : msg.role === "tool_call"
                  ? `📞 Call: ${msg.toolName}`
                  : "Agent asks"}
                {" · "}
                {msg.timestamp.toLocaleTimeString()}
              </div>
              {/* Reasoning (chain-of-thought) — collapsible */}
              {msg.reasoning && (
                <details className="mt-1 mb-2">
                  <summary className="text-[11px] text-muted-foreground cursor-pointer hover:text-foreground transition-colors select-none flex items-center gap-1">
                    <span className="inline-block w-3.5 h-3.5 rounded-full bg-purple-500/20 flex items-center justify-center text-[9px] text-purple-500 font-bold">?</span>
                    Reasoning
                  </summary>
                  <div className="text-xs text-muted-foreground/80 bg-muted/50 rounded p-2 mt-1 border border-border/50 whitespace-pre-wrap break-words leading-relaxed">
                    {msg.reasoning}
                  </div>
                </details>
              )}
              <div className="text-sm whitespace-pre-wrap break-words leading-relaxed">
                {msg.content}
              </div>

              {/* Error detail for failed tools */}
              {msg.role === "tool" && msg.toolStatus === "error" && msg.toolError && (
                <div className="mt-1 text-xs text-red-500 break-words bg-red-500/5 rounded p-2 border border-red-500/20">
                  {msg.toolError}
                </div>
              )}

              {/* Choice buttons */}
              {msg.choices && msg.choices.length > 0 && (
                <div className="flex flex-wrap gap-2 mt-2">
                  {msg.choices.map((c) => (
                    <button
                      key={c.id}
                      onClick={() => handleChoice(c.id)}
                      className="px-3 py-1.5 bg-primary text-primary-foreground rounded-md text-xs hover:bg-primary/90 transition-colors"
                    >
                      {c.title}
                    </button>
                  ))}
                </div>
              )}
            </div>
          </div>
        ))}

        {/* Thinking indicator (shown while waiting for first stream chunk) */}
        {agentThinking && !streamingContent && !streamingReasoning && (
          <div className="flex gap-3">
            <div className="w-7 h-7 rounded-md bg-green-500/10 flex items-center justify-center">
              <Loader2 className="w-3.5 h-3.5 text-green-500 animate-spin" />
            </div>
            <div className="text-sm text-muted-foreground">Agent is thinking...</div>
          </div>
        )}

        {/* Live streaming content — shown while agent is generating */}
        {streamingContent && (
          <div className="flex gap-3">
            <div className="w-7 h-7 rounded-md bg-green-500/10 flex items-center justify-center">
              <Bot className="w-3.5 h-3.5 text-green-500" />
            </div>
            <div className="flex-1 min-w-0">
              <div className="text-xs text-muted-foreground mb-1">
                Agent &middot; {new Date().toLocaleTimeString()}
              </div>
              {streamingReasoning && (
                <details open={showReasoning} className="mb-2">
                  <summary className="text-[11px] text-muted-foreground cursor-pointer hover:text-foreground transition-colors select-none flex items-center gap-1">
                    <span className="inline-block w-3.5 h-3.5 rounded-full bg-purple-500/20 flex items-center justify-center text-[9px] text-purple-500 font-bold">?</span>
                    Reasoning
                  </summary>
                  <div className="text-xs text-muted-foreground/80 bg-muted/50 rounded p-2 mt-1 border border-border/50 whitespace-pre-wrap break-words leading-relaxed">
                    {streamingReasoning}
                  </div>
                </details>
              )}
              <div className="text-sm whitespace-pre-wrap break-words leading-relaxed">
                {streamingContent}
                <span className="inline-block w-1.5 h-4 bg-primary/50 animate-pulse ml-0.5 align-text-bottom" />
              </div>
            </div>
          </div>
        )}

        <div ref={messagesEndRef} />
      </div>

      {/* Usage footer */}
      {usage && (
        <div className="border-t border-border px-4 py-1.5 flex items-center gap-3 text-[11px] text-muted-foreground font-mono">
          <BrainCircuit className="w-3 h-3 flex-shrink-0" />
          <span title={`Prompt tokens: ${usage.promptTokens.toLocaleString()}`}>
            ↑{fmtTokens(usage.promptTokens)}
          </span>
          <span title={`Completion tokens: ${usage.completionTokens.toLocaleString()}`}>
            ↓{fmtTokens(usage.completionTokens)}
          </span>
          <span title={`Total tokens (cumulative): ${usage.totalTokens.toLocaleString()}`}>
            R{fmtTokens(usage.totalTokens)}
          </span>
          <span
            title={`Cache hit: ${usage.cacheHitTokens.toLocaleString()} · Cache miss: ${usage.cacheMissTokens.toLocaleString()}`}
            className={usage.cacheHitRate >= 90 ? "text-green-500" : usage.cacheHitRate >= 50 ? "text-yellow-500" : "text-red-500"}
          >
            CH{usage.cacheHitRate.toFixed(1)}%
          </span>
          {usage.contextWindow > 0 && (
            <span title={`Context usage: ${usage.promptTokens.toLocaleString()} / ${usage.contextWindow.toLocaleString()} (${usage.contextUsagePercent.toFixed(1)}%)`}>
              {usage.contextUsagePercent.toFixed(1)}%/{fmtTokens(usage.contextWindow)}
            </span>
          )}
          {usage.totalTokens > 0 && (
            <span className="text-muted-foreground/60">
              (auto)
            </span>
          )}
        </div>
      )}

      {/* Input */}
      <div className="border-t border-border p-4">
        <div className="flex gap-2">
          <input
            type="text"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                handleSend();
              }
            }}
            placeholder="Type a message... (e.g. 'prepare to deploy')"
            className="flex-1 bg-input border border-border rounded-md px-4 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            disabled={agentThinking}
          />
          {agentThinking ? (
            <button
              onClick={handleCancel}
              className="px-4 py-2 bg-destructive text-destructive-foreground rounded-md text-sm flex items-center gap-2 hover:bg-destructive/90 transition-colors"
            >
              <Square className="w-4 h-4" />
              Stop
            </button>
          ) : (
            <button
              onClick={handleSend}
              disabled={!input.trim()}
              className="px-4 py-2 bg-primary text-primary-foreground rounded-md text-sm flex items-center gap-2 hover:bg-primary/90 disabled:opacity-50 transition-colors"
            >
              <Send className="w-4 h-4" />
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
