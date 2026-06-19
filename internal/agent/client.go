package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// --- API Types ---

// UsageData captures token usage from LLM API responses.
type UsageData struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
	// Cache hit/miss (DeepSeek, some other providers)
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens,omitempty"`
}

type ChatMessage struct {
	Role       string      `json:"role"`
	Content    string      `json:"content,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	Name       string      `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolDef struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

type FunctionDef struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  JSONSchema `json:"parameters"`
}

type JSONSchema struct {
	Type       string              `json:"type"`
	Properties map[string]JSONProp `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type JSONProp struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

type ChatRequest struct {
	Model      string        `json:"model"`
	Tools      []ToolDef     `json:"tools,omitempty"`
	Messages   []ChatMessage `json:"messages"`
	ToolChoice string        `json:"tool_choice,omitempty"`
	Stream     bool          `json:"stream"`
}

type ChatResponse struct {
	Choices []struct {
		Message      ChatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage *UsageData `json:"usage,omitempty"`
}

type StreamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string     `json:"content"`
			ToolCalls        []ToolCall `json:"tool_calls"`
			ReasoningContent string     `json:"reasoning_content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *UsageData `json:"usage,omitempty"`
}

// --- Client ---

// DefaultBaseURL is the default OpenAI-compatible API endpoint.
const DefaultBaseURL = "https://api.openai.com/v1"

// DefaultModel is the default model name.
const DefaultModel = "gpt-4o"

// Client is an OpenAI-compatible chat completions client.
type Client struct {
	APIKey  string
	BaseURL string
	Model   string
}

// NewClient creates a new OpenAI-compatible client.
func NewClient(apiKey, baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		APIKey:  apiKey,
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
	}
}

// Chat sends a chat completion request and returns the response.
func (c *Client) Chat(messages []ChatMessage, tools []ToolDef) (*ChatResponse, error) {
	req := ChatRequest{
		Model:    c.Model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
	}
	if len(tools) > 0 {
		req.ToolChoice = "auto"
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

	// Log the request summary
	lastMsg := messages[len(messages)-1]
	log.Printf("[llm] ── request (iter) ─────────────────────────")
	log.Printf("[llm] model=%s  msgs=%d  tools=%d  last_role=%s", c.Model, len(messages), len(tools), lastMsg.Role)
	if lastMsg.Content != "" {
		preview := lastMsg.Content
		if len(preview) > 120 {
			preview = preview[:120] + "..."
		}
		log.Printf("[llm] last_msg: %s", preview)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("api call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api error %d: %s", resp.StatusCode, string(errBody))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Log the full LLM response
	log.Printf("[llm] ── response ──────────────────────────────")
	if len(chatResp.Choices) > 0 {
		msg := chatResp.Choices[0].Message
		log.Printf("[llm] finish_reason: %s", chatResp.Choices[0].FinishReason)
		if msg.Content != "" {
			log.Printf("[llm] content: %s", msg.Content)
		}
		for _, tc := range msg.ToolCalls {
			log.Printf("[llm] tool_call: %s(%s)", tc.Function.Name, tc.Function.Arguments)
		}
	}
	if chatResp.Usage != nil {
		u := chatResp.Usage
		hitRate := 0.0
		if u.PromptTokens > 0 {
			hitRate = float64(u.PromptCacheHitTokens) / float64(u.PromptTokens) * 100
		}
		log.Printf("[llm] usage: ↑%d ↓%d total=%d cache_hit=%d cache_miss=%d hit_rate=%.1f%%",
			u.PromptTokens, u.CompletionTokens, u.TotalTokens,
			u.PromptCacheHitTokens, u.PromptCacheMissTokens, hitRate)
	}
	log.Printf("[llm] ───────────────────────────────────────────")

	return &chatResp, nil
}

// ChatStream sends a streaming chat completion request.
// The callback is called for each content chunk and tool call delta.
func (c *Client) ChatStream(messages []ChatMessage, tools []ToolDef, onChunk func(chunk StreamChunk) error) error {
	req := ChatRequest{
		Model:    c.Model,
		Messages: messages,
		Tools:    tools,
		Stream:   true,
	}
	if len(tools) > 0 {
		req.ToolChoice = "auto"
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("api call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("api error %d: %s", resp.StatusCode, string(errBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if err := onChunk(chunk); err != nil {
			return err
		}
	}

	return scanner.Err()
}
