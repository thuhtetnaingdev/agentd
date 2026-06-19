package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentd/internal/agent"
	"agentd/internal/store"
)

// TestBuildStoreMessages_ErrorWithOutput verifies tool results with error + output
// produce correct IsError/ErrorDetail/Content.
func TestBuildStoreMessages_ErrorWithOutput(t *testing.T) {
	// Simulate what AgentRunner.Run() puts in messages for a failed deploy_rsync
	msgs := []agent.ChatMessage{
		{Role: "user", Content: "deploy"}, // index 0
		{ // index 1: assistant with tool_calls
			Role:    "assistant",
			Content: "Deploying...",
			ToolCalls: []agent.ToolCall{
				{ID: "call_1", Function: agent.FunctionCall{Name: "deploy_rsync", Arguments: `{"serverId":"vps","projectName":"foo"}`}},
			},
		},
		{ // index 2: tool result with output AND error
			Role:       "tool",
			Name:       "deploy_rsync",
			ToolCallID: "call_1",
			Content:    mustJSON(t, map[string]any{"success": false, "output": "tar: permission denied\n", "error": "remote extract failed: exit status 2"}),
		},
	}

	result := BuildStoreMessages(msgs)
	// Expected: user → tool_call(call_1) → tool(call_1)
	if len(result) < 4 {
		t.Fatalf("expected >= 4 messages, got %d", len(result))
	}

	// Find the tool result
	var toolMsg store.Message
	for _, m := range result {
		if m.Role == "tool" && m.ToolCallID == "call_1" {
			toolMsg = m
			break
		}
	}

	if !toolMsg.IsError {
		t.Error("IsError should be true")
	}
	if toolMsg.ErrorDetail == "" {
		t.Error("ErrorDetail should not be empty")
	}
	if toolMsg.Content == "" {
		t.Error("Content should not be empty (has output)")
	}
	t.Logf("With output: IsError=%v Content=%q ErrorDetail=%q", toolMsg.IsError, toolMsg.Content, toolMsg.ErrorDetail)
}

// TestBuildStoreMessages_ErrorEmptyOutput verifies the "empty bubble" bug is fixed:
// when a tool result has success=false, output="", error="...", Content falls back to Error.
func TestBuildStoreMessages_ErrorEmptyOutput(t *testing.T) {
	msgs := []agent.ChatMessage{
		{Role: "user", Content: "deploy"},
		{
			Role:    "assistant",
			Content: "Deploying...",
			ToolCalls: []agent.ToolCall{
				{ID: "call_1", Function: agent.FunctionCall{Name: "deploy_rsync", Arguments: `{}`}},
			},
		},
		{
			Role:       "tool",
			Name:       "deploy_rsync",
			ToolCallID: "call_1",
			// EMPTY output + error — this is what caused empty bubbles
			Content: mustJSON(t, map[string]any{"success": false, "output": "", "error": "remote extract failed: exit status 2"}),
		},
	}

	result := BuildStoreMessages(msgs)

	var toolMsg store.Message
	for _, m := range result {
		if m.Role == "tool" && m.ToolCallID == "call_1" {
			toolMsg = m
			break
		}
	}

	if !toolMsg.IsError {
		t.Error("IsError should be true")
	}
	if toolMsg.ErrorDetail == "" {
		t.Error("ErrorDetail should not be empty")
	}
	// THIS is the critical fix: Content must fall back to Error when Output is ""
	if toolMsg.Content == "" {
		t.Error("Content is empty — the 'empty bubble' bug! Content must fall back to ErrorDetail when Output is empty")
	}
	t.Logf("Empty output: IsError=%v Content=%q ErrorDetail=%q", toolMsg.IsError, toolMsg.Content, toolMsg.ErrorDetail)
}

// TestBuildStoreMessages_Success verifies successful tool results.
func TestBuildStoreMessages_Success(t *testing.T) {
	msgs := []agent.ChatMessage{
		{Role: "user", Content: "ls"},
		{
			Role:    "assistant",
			Content: "Running ls...",
			ToolCalls: []agent.ToolCall{
				{ID: "call_1", Function: agent.FunctionCall{Name: "run_shell", Arguments: `{"command":"ls"}`}},
			},
		},
		{
			Role:       "tool",
			Name:       "run_shell",
			ToolCallID: "call_1",
			Content:    mustJSON(t, map[string]any{"success": true, "output": "file1.txt\nfile2.txt", "error": ""}),
		},
	}

	result := BuildStoreMessages(msgs)

	var toolMsg store.Message
	for _, m := range result {
		if m.Role == "tool" && m.ToolCallID == "call_1" {
			toolMsg = m
			break
		}
	}

	if toolMsg.IsError {
		t.Error("IsError should be false for successful tool")
	}
	if toolMsg.ErrorDetail != "" {
		t.Error("ErrorDetail should be empty for successful tool")
	}
	if toolMsg.Content != "file1.txt\nfile2.txt" {
		t.Errorf("Content mismatch: got %q", toolMsg.Content)
	}
	t.Logf("Success: IsError=%v Content=%q ErrorDetail=%q", toolMsg.IsError, toolMsg.Content, toolMsg.ErrorDetail)
}

// TestBuildStoreMessages_Interleaving verifies tool_call + tool result adjacency.
func TestBuildStoreMessages_Interleaving(t *testing.T) {
	msgs := []agent.ChatMessage{
		{Role: "user", Content: "deploy and ls"},
		{
			Role:    "assistant",
			Content: "Running...",
			ToolCalls: []agent.ToolCall{
				{ID: "tc1", Function: agent.FunctionCall{Name: "run_shell", Arguments: `{"command":"ls"}`}},
				{ID: "tc2", Function: agent.FunctionCall{Name: "deploy_rsync", Arguments: `{"serverId":"vps"}`}},
			},
		},
		{Role: "tool", Name: "run_shell", ToolCallID: "tc1", Content: `{"success":true,"output":"a","error":""}`},
		{Role: "tool", Name: "deploy_rsync", ToolCallID: "tc2", Content: `{"success":false,"output":"","error":"perm denied"}`},
	}

	result := BuildStoreMessages(msgs)

	// Expected: user → agent(content) → tool_call(tc1) → tool(tc1) → tool_call(tc2) → tool(tc2)
	expected := []struct {
		idx  int
		role string
	}{
		{0, "user"},
		{1, "agent"},
		{2, "tool_call"},
		{3, "tool"},
		{4, "tool_call"},
		{5, "tool"},
	}

	if len(result) != len(expected) {
		t.Fatalf("expected %d messages, got %d", len(expected), len(result))
	}

	for _, exp := range expected {
		got := result[exp.idx].Role
		if got != exp.role {
			t.Errorf("messages[%d]: expected role=%q, got %q", exp.idx, exp.role, got)
		}
	}

	// Verify adjacency: every tool_call must be followed by tool
	for i := 0; i < len(result)-1; i++ {
		if result[i].Role == "tool_call" {
			if result[i+1].Role != "tool" {
				t.Errorf("tool_call at %d not immediately followed by tool (got %q)", i, result[i+1].Role)
			}
		}
	}
}

// TestBuildStoreMessages_WhitespaceOnlyAgentContent verifies that when the LLM
// returns whitespace-only content (e.g. "\n") alongside tool_calls, no empty
// agent bubble is stored. This was the bug where `content: "\n"` produced an
// empty bubble in the chat history.
func TestBuildStoreMessages_WhitespaceOnlyAgentContent(t *testing.T) {
	msgs := []agent.ChatMessage{
		{Role: "user", Content: "check my servers"},
		{
			Role:    "assistant",
			Content: "\n", // <-- LLM returns newline with tool_calls
			ToolCalls: []agent.ToolCall{
				{ID: "call_1", Function: agent.FunctionCall{Name: "check_ssh_credentials", Arguments: `{"serverId":"srv_1"}`}},
			},
		},
		{Role: "tool", Name: "check_ssh_credentials", ToolCallID: "call_1", Content: `{"success":true,"output":"ok"}`},
		{
			Role:    "assistant",
			Content: "Here's what I found...",
		},
	}

	result := BuildStoreMessages(msgs)

	// Expected: user → tool_call(call_1) → tool(call_1) → agent("Here's...")
	// No empty agent bubble with "\n" should appear.
	expected := []struct {
		idx  int
		role string
	}{
		{0, "user"},
		{1, "tool_call"},
		{2, "tool"},
		{3, "agent"},
	}

	if len(result) != len(expected) {
		t.Fatalf("expected %d messages, got %d", len(expected), len(result))
	}

	for _, exp := range expected {
		got := result[exp.idx].Role
		if got != exp.role {
			t.Errorf("messages[%d]: expected role=%q, got %q", exp.idx, exp.role, got)
		}
	}

	// Also test whitespace variants
	tests := []string{"\n", "  ", "\t", "\n\n", " \n "}
	for _, ws := range tests {
		msgs := []agent.ChatMessage{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: ws, ToolCalls: []agent.ToolCall{
				{ID: "c1", Function: agent.FunctionCall{Name: "list_servers", Arguments: `{}`}},
			}},
			{Role: "tool", Name: "list_servers", ToolCallID: "c1", Content: `[]`},
		}
		result := BuildStoreMessages(msgs)
		// Should be: user → tool_call → tool (no agent bubble)
		if len(result) != 3 {
			t.Errorf("whitespace=%q: expected 3 messages, got %d: %+v", ws, len(result), result)
		}
		for _, m := range result {
			if m.Role == "agent" {
				t.Errorf("whitespace=%q: agent bubble should not appear (content=%q)", ws, m.Content)
			}
		}
	}
}

// TestJSONLRoundtrip verifies the full JSONL store/load pipeline preserves all fields.
func TestJSONLRoundtrip(t *testing.T) {
	tmpDir := t.TempDir()
	ss := store.NewSessionStore(filepath.Join(tmpDir, "store"))

	msg := store.Message{
		Role:        "tool",
		ToolName:    "deploy_rsync",
		ToolCallID:  "call_abc123",
		IsError:     true,
		Content:     "Permission denied",
		ErrorDetail: "cannot create remote directory /var/www/foo: Permission denied",
		Timestamp:   time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC),
	}

	if err := ss.SaveMessages("pj", "sid", []store.Message{msg}); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	swm, err := ss.Get("pj", "sid")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(swm.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(swm.Messages))
	}

	got := swm.Messages[0]

	checks := []struct {
		field string
		want  string
		got   string
	}{
		{"role", "tool", got.Role},
		{"toolName", "deploy_rsync", got.ToolName},
		{"toolCallId", "call_abc123", got.ToolCallID},
		{"content", "Permission denied", got.Content},
		{"errorDetail", "cannot create remote directory /var/www/foo: Permission denied", got.ErrorDetail},
	}

	for _, c := range checks {
		if c.want != c.got {
			t.Errorf("%s: want=%q got=%q", c.field, c.want, c.got)
		}
	}

	if !got.IsError {
		t.Error("IsError: want=true got=false")
	}
	if got.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}

	// Verify the raw JSONL on disk has all the new fields
	data, err := os.ReadFile(filepath.Join(tmpDir, "store", "pj", "sid.jsonl"))
	if err != nil {
		t.Fatalf("reading JSONL file: %v", err)
	}

	requiredJSONLFields := []string{
		`"isError"`,
		`"errorDetail"`,
		`"timestamp"`,
		`"toolCallId"`,
	}

	for _, f := range requiredJSONLFields {
		if !containsStr(string(data), f) {
			t.Errorf("JSONL on disk missing field %s. Raw: %s", f, string(data))
		}
	}
}

// TestTimestampRoundtrip verifies timestamps survive store/load roundtrip.
func TestTimestampRoundtrip(t *testing.T) {
	tmpDir := t.TempDir()
	ss := store.NewSessionStore(filepath.Join(tmpDir, "store"))

	now := time.Now().Truncate(time.Second)
	msgs := []store.Message{
		{Role: "user", Content: "hi", Timestamp: now},
	}

	if err := ss.SaveMessages("p", "s", msgs); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	swm, err := ss.Get("p", "s")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(swm.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(swm.Messages))
	}

	if swm.Messages[0].Timestamp.IsZero() {
		t.Error("Timestamp is zero after load — not preserved")
	}

	if !swm.Messages[0].Timestamp.Equal(now) {
		t.Errorf("Timestamp mismatch: saved=%v loaded=%v", now, swm.Messages[0].Timestamp)
	}
}

// --- helpers ---

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(b)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
