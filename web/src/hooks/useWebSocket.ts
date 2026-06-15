import { useEffect, useRef, useCallback } from "react";

export interface WSMessage {
  type: string;
  payload?: any;
  sessionId?: string;
}

export function useWebSocket(
  projectId: string | null,
  sessionId: string | null,
  onMessage: (msg: WSMessage) => void
) {
  const wsRef = useRef<WebSocket | null>(null);
  const onMessageRef = useRef(onMessage);
  onMessageRef.current = onMessage;

  const connect = useCallback(() => {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    let wsUrl = `${protocol}//${window.location.host}/api/ws?projectId=${projectId || ""}`;
    if (sessionId) {
      wsUrl += `&sid=${sessionId}`;
    }

    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => {
      console.log("[ws] connected", sessionId ? `(session: ${sessionId})` : "");
    };

    ws.onmessage = (event) => {
      try {
        const msg: WSMessage = JSON.parse(event.data);
        onMessageRef.current(msg);
      } catch (e) {
        console.error("[ws] parse error", e);
      }
    };

    ws.onclose = () => {
      console.log("[ws] disconnected, reconnecting in 2s...");
      setTimeout(connect, 2000);
    };

    ws.onerror = (err) => {
      console.error("[ws] error", err);
    };
  }, [projectId, sessionId]);

  useEffect(() => {
    connect();
    return () => {
      wsRef.current?.close();
    };
  }, [connect]);

  const send = useCallback((msg: WSMessage) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(msg));
    }
  }, []);

  return { send };
}
