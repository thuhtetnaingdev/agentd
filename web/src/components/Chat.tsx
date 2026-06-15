import { useState, useRef, useEffect } from "react";
import { useWebSocket, WSMessage } from "../hooks/useWebSocket";
import { Send, Loader2, Bot, User, Wrench, Check, X, AlertTriangle } from "lucide-react";

interface ChatMessage {
  id: string;
  role: "user" | "agent" | "tool" | "choice";
  content: string;
  toolName?: string;
  toolStatus?: "running" | "success" | "error";
  choices?: { id: string; title: string }[];
  timestamp: Date;
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
  const messagesEndRef = useRef<HTMLDivElement>(null);

  const handleMessage = (msg: WSMessage) => {
    switch (msg.type) {
      case "chat_ack":
        // Acknowledged — agent is processing
        break;
      case "agent_message":
        setAgentThinking(false);
        setMessages((prev) => [
          ...prev,
          {
            id: crypto.randomUUID(),
            role: "agent",
            content: msg.payload?.content || "",
            timestamp: new Date(),
          },
        ]);
        break;
      case "tool_call":
        setMessages((prev) => [
          ...prev,
          {
            id: crypto.randomUUID(),
            role: "tool",
            content: msg.payload?.description || "",
            toolName: msg.payload?.toolName,
            toolStatus: "running",
            timestamp: new Date(),
          },
        ]);
        break;
      case "tool_result":
        setMessages((prev) =>
          prev.map((m) =>
            m.role === "tool" &&
            m.toolName === msg.payload?.toolName &&
            m.toolStatus === "running"
              ? {
                  ...m,
                  toolStatus: msg.payload?.success ? "success" : "error",
                  content: msg.payload?.output || m.content,
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
                  : "Agent asks"}
              </div>
              <div className="text-sm whitespace-pre-wrap break-words leading-relaxed">
                {msg.content}
              </div>

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

        {/* Thinking indicator */}
        {agentThinking && (
          <div className="flex gap-3">
            <div className="w-7 h-7 rounded-md bg-green-500/10 flex items-center justify-center">
              <Loader2 className="w-3.5 h-3.5 text-green-500 animate-spin" />
            </div>
            <div className="text-sm text-muted-foreground">Agent is thinking...</div>
          </div>
        )}

        <div ref={messagesEndRef} />
      </div>

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
          <button
            onClick={handleSend}
            disabled={!input.trim() || agentThinking}
            className="px-4 py-2 bg-primary text-primary-foreground rounded-md text-sm flex items-center gap-2 hover:bg-primary/90 disabled:opacity-50 transition-colors"
          >
            <Send className="w-4 h-4" />
          </button>
        </div>
      </div>
    </div>
  );
}
