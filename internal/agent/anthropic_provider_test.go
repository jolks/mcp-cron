// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestToAnthropicTools(t *testing.T) {
	tools := []ToolDefinition{
		{
			Name:        "calculator",
			Description: "Evaluate math expressions",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"expression": map[string]interface{}{
						"type":        "string",
						"description": "Math expression",
					},
				},
				"required": []interface{}{"expression"},
			},
		},
	}

	result := toAnthropicTools(tools)

	if len(result) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(result))
	}
	tool := result[0].OfTool
	if tool == nil {
		t.Fatal("Expected OfTool to be set")
	}
	if tool.Name != "calculator" {
		t.Errorf("Expected name 'calculator', got '%s'", tool.Name)
	}
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "expression" {
		t.Errorf("Expected required ['expression'], got %v", tool.InputSchema.Required)
	}
	props, ok := tool.InputSchema.Properties.(map[string]interface{})
	if !ok {
		t.Fatal("Expected properties to be map[string]interface{}")
	}
	if props["expression"] == nil {
		t.Error("Expected 'expression' property to exist")
	}
}

func TestToAnthropicTools_EmptyProperties(t *testing.T) {
	tools := []ToolDefinition{
		{
			Name:        "noop",
			Description: "Does nothing",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}

	result := toAnthropicTools(tools)

	if len(result) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(result))
	}
	emptyProps, ok := result[0].OfTool.InputSchema.Properties.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected properties to be map[string]interface{}, got %T", result[0].OfTool.InputSchema.Properties)
	}
	if len(emptyProps) != 0 {
		t.Errorf("Expected 0 properties, got %d", len(emptyProps))
	}
}

func TestToAnthropicTools_RequiredAsStringSlice(t *testing.T) {
	tools := []ToolDefinition{
		{
			Name:        "test",
			Description: "Test tool",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{"foo", "bar"},
			},
		},
	}

	result := toAnthropicTools(tools)

	if len(result[0].OfTool.InputSchema.Required) != 2 {
		t.Fatalf("Expected 2 required fields, got %d", len(result[0].OfTool.InputSchema.Required))
	}
	if result[0].OfTool.InputSchema.Required[0] != "foo" {
		t.Errorf("Expected 'foo', got '%s'", result[0].OfTool.InputSchema.Required[0])
	}
}

func TestToAnthropicMessages_UserMessage(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "Hello Claude"},
	}

	result := toAnthropicMessages(msgs)

	if len(result) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(result))
	}
	if result[0].Role != anthropic.MessageParamRoleUser {
		t.Errorf("Expected role 'user', got '%s'", result[0].Role)
	}
	if len(result[0].Content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(result[0].Content))
	}
	if result[0].Content[0].OfText == nil {
		t.Fatal("Expected text block")
	}
	if result[0].Content[0].OfText.Text != "Hello Claude" {
		t.Errorf("Expected 'Hello Claude', got '%s'", result[0].Content[0].OfText.Text)
	}
}

func TestToAnthropicMessages_ToolResult(t *testing.T) {
	msgs := []Message{
		{Role: "tool", Content: "42", ToolCallID: "toolu_123"},
	}

	result := toAnthropicMessages(msgs)

	if len(result) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(result))
	}
	// Tool results become user messages in Anthropic
	if result[0].Role != anthropic.MessageParamRoleUser {
		t.Errorf("Expected role 'user' for tool result, got '%s'", result[0].Role)
	}
	if len(result[0].Content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(result[0].Content))
	}
	if result[0].Content[0].OfToolResult == nil {
		t.Fatal("Expected tool result block")
	}
	if result[0].Content[0].OfToolResult.ToolUseID != "toolu_123" {
		t.Errorf("Expected ToolUseID 'toolu_123', got '%s'", result[0].Content[0].OfToolResult.ToolUseID)
	}
}

func TestToAnthropicMessages_AssistantWithToolCalls(t *testing.T) {
	msgs := []Message{
		{
			Role:    "assistant",
			Content: "Let me check that",
			ToolCalls: []ToolCall{
				{ID: "toolu_1", Name: "calculator", Arguments: `{"expression":"2+2"}`},
			},
		},
	}

	result := toAnthropicMessages(msgs)

	if len(result) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(result))
	}
	if result[0].Role != anthropic.MessageParamRoleAssistant {
		t.Errorf("Expected role 'assistant', got '%s'", result[0].Role)
	}
	// Should have text block + tool_use block
	if len(result[0].Content) != 2 {
		t.Fatalf("Expected 2 content blocks (text + tool_use), got %d", len(result[0].Content))
	}
	if result[0].Content[0].OfText == nil {
		t.Fatal("Expected first block to be text")
	}
	if result[0].Content[1].OfToolUse == nil {
		t.Fatal("Expected second block to be tool_use")
	}
	if result[0].Content[1].OfToolUse.Name != "calculator" {
		t.Errorf("Expected tool name 'calculator', got '%s'", result[0].Content[1].OfToolUse.Name)
	}
}

func TestToAnthropicMessages_AssistantEmptyArguments(t *testing.T) {
	msgs := []Message{
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "toolu_1", Name: "noop", Arguments: ""},
			},
		},
	}

	result := toAnthropicMessages(msgs)

	if len(result[0].Content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(result[0].Content))
	}
	tu := result[0].Content[0].OfToolUse
	if tu == nil {
		t.Fatal("Expected tool_use block")
	}
	// Empty arguments should default to "{}"
	inputBytes, ok := tu.Input.(json.RawMessage)
	if !ok {
		t.Fatalf("Expected Input to be json.RawMessage, got %T", tu.Input)
	}
	if string(inputBytes) != "{}" {
		t.Errorf("Expected input '{}', got '%s'", string(inputBytes))
	}
}

func TestFromAnthropicMessage_TextOnly(t *testing.T) {
	resp := &anthropic.Message{
		Content: []anthropic.ContentBlockUnion{
			makeTextBlock("The answer is 42"),
		},
	}

	result := fromAnthropicMessage(resp)

	if result.Role != "assistant" {
		t.Errorf("Expected role 'assistant', got '%s'", result.Role)
	}
	if result.Content != "The answer is 42" {
		t.Errorf("Expected 'The answer is 42', got '%s'", result.Content)
	}
	if len(result.ToolCalls) != 0 {
		t.Errorf("Expected 0 tool calls, got %d", len(result.ToolCalls))
	}
}

func TestFromAnthropicMessage_ToolUseOnly(t *testing.T) {
	resp := &anthropic.Message{
		Content: []anthropic.ContentBlockUnion{
			makeToolUseBlock("toolu_abc", "get_weather", `{"city":"NYC"}`),
		},
	}

	result := fromAnthropicMessage(resp)

	if result.Content != "" {
		t.Errorf("Expected empty content, got '%s'", result.Content)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.ID != "toolu_abc" {
		t.Errorf("Expected ID 'toolu_abc', got '%s'", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("Expected name 'get_weather', got '%s'", tc.Name)
	}
	if tc.Arguments != `{"city":"NYC"}` {
		t.Errorf("Expected arguments, got '%s'", tc.Arguments)
	}
}

func TestFromAnthropicMessage_MixedTextAndToolUse(t *testing.T) {
	resp := &anthropic.Message{
		Content: []anthropic.ContentBlockUnion{
			makeTextBlock("Let me check"),
			makeToolUseBlock("toolu_1", "calculator", `{"expr":"2+2"}`),
			makeToolUseBlock("toolu_2", "search", `{"q":"test"}`),
		},
	}

	result := fromAnthropicMessage(resp)

	if result.Content != "Let me check" {
		t.Errorf("Expected 'Let me check', got '%s'", result.Content)
	}
	if len(result.ToolCalls) != 2 {
		t.Fatalf("Expected 2 tool calls, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "calculator" {
		t.Errorf("Expected first tool 'calculator', got '%s'", result.ToolCalls[0].Name)
	}
	if result.ToolCalls[1].Name != "search" {
		t.Errorf("Expected second tool 'search', got '%s'", result.ToolCalls[1].Name)
	}
}

func TestFromAnthropicMessage_MultipleTextBlocks(t *testing.T) {
	resp := &anthropic.Message{
		Content: []anthropic.ContentBlockUnion{
			makeTextBlock("First part"),
			makeTextBlock("Second part"),
		},
	}

	result := fromAnthropicMessage(resp)

	if result.Content != "First part\nSecond part" {
		t.Errorf("Expected 'First part\\nSecond part', got '%s'", result.Content)
	}
}

// makeTextBlock creates a ContentBlockUnion with type "text" for testing.
func makeTextBlock(text string) anthropic.ContentBlockUnion {
	raw := `{"type":"text","text":` + mustJSON(text) + `}`
	var block anthropic.ContentBlockUnion
	if err := json.Unmarshal([]byte(raw), &block); err != nil {
		panic("makeTextBlock: " + err.Error())
	}
	return block
}

// makeToolUseBlock creates a ContentBlockUnion with type "tool_use" for testing.
func makeToolUseBlock(id, name, inputJSON string) anthropic.ContentBlockUnion {
	raw := `{"type":"tool_use","id":` + mustJSON(id) + `,"name":` + mustJSON(name) + `,"input":` + inputJSON + `}`
	var block anthropic.ContentBlockUnion
	if err := json.Unmarshal([]byte(raw), &block); err != nil {
		panic("makeToolUseBlock: " + err.Error())
	}
	return block
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
