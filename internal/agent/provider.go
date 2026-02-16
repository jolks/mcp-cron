// SPDX-License-Identifier: AGPL-3.0-only
package agent

import "context"

// ToolDefinition is a provider-agnostic representation of a tool that can be
// offered to an LLM during a chat completion.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]interface{}
}

// ToolCall represents a single tool invocation requested by the model.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Message is a provider-agnostic chat message.
type Message struct {
	Role       string     // "user", "assistant", "tool"
	Content    string     // text content
	ToolCalls  []ToolCall // tool calls requested by the assistant
	ToolCallID string     // set when Role == "tool" to correlate with a ToolCall
}

// ChatProvider abstracts a chat-completion backend so the agent loop can work
// with any LLM provider.
type ChatProvider interface {
	// CreateCompletion sends a chat completion request and returns the
	// assistant's response message. systemMsg is an optional system-level
	// instruction prepended to the conversation (empty string to omit).
	CreateCompletion(ctx context.Context, model string, systemMsg string, messages []Message, tools []ToolDefinition) (*Message, error)
}
