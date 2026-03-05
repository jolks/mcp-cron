// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"testing"

	"github.com/openai/openai-go/v3"
)

func TestToOpenAITools(t *testing.T) {
	tools := []ToolDefinition{
		{
			Name:        "get_weather",
			Description: "Get current weather",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"city": map[string]interface{}{
						"type":        "string",
						"description": "City name",
					},
				},
				"required": []string{"city"},
			},
		},
		{
			Name:        "list_files",
			Description: "List files in a directory",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}

	result := toOpenAITools(tools)

	if len(result) != 2 {
		t.Fatalf("Expected 2 tools, got %d", len(result))
	}
	if result[0].OfFunction == nil {
		t.Fatal("Expected OfFunction to be set")
	}
	if result[0].OfFunction.Function.Name != "get_weather" {
		t.Errorf("Expected tool name 'get_weather', got '%s'", result[0].OfFunction.Function.Name)
	}
	if result[1].OfFunction == nil {
		t.Fatal("Expected OfFunction to be set")
	}
	if result[1].OfFunction.Function.Name != "list_files" {
		t.Errorf("Expected tool name 'list_files', got '%s'", result[1].OfFunction.Function.Name)
	}
}

func TestToOpenAIMessage_User(t *testing.T) {
	msg := Message{Role: "user", Content: "Hello"}
	result := toOpenAIMessage(msg)

	if result.OfUser == nil {
		t.Fatal("Expected user message, got nil")
	}
}

func TestToOpenAIMessage_Tool(t *testing.T) {
	msg := Message{Role: "tool", Content: "result data", ToolCallID: "call_123"}
	result := toOpenAIMessage(msg)

	if result.OfTool == nil {
		t.Fatal("Expected tool message, got nil")
	}
	if result.OfTool.ToolCallID != "call_123" {
		t.Errorf("Expected ToolCallID 'call_123', got '%s'", result.OfTool.ToolCallID)
	}
}

func TestToOpenAIMessage_AssistantWithContent(t *testing.T) {
	msg := Message{Role: "assistant", Content: "I can help with that"}
	result := toOpenAIMessage(msg)

	if result.OfAssistant == nil {
		t.Fatal("Expected assistant message, got nil")
	}
}

func TestToOpenAIMessage_AssistantWithToolCalls(t *testing.T) {
	msg := Message{
		Role: "assistant",
		ToolCalls: []ToolCall{
			{ID: "call_1", Name: "get_weather", Arguments: `{"city":"NYC"}`},
			{ID: "call_2", Name: "list_files", Arguments: `{}`},
		},
	}
	result := toOpenAIMessage(msg)

	if result.OfAssistant == nil {
		t.Fatal("Expected assistant message, got nil")
	}
	if len(result.OfAssistant.ToolCalls) != 2 {
		t.Fatalf("Expected 2 tool calls, got %d", len(result.OfAssistant.ToolCalls))
	}
	tc0 := result.OfAssistant.ToolCalls[0].OfFunction
	if tc0 == nil {
		t.Fatal("Expected OfFunction to be set for tool call 0")
	}
	if tc0.ID != "call_1" {
		t.Errorf("Expected tool call ID 'call_1', got '%s'", tc0.ID)
	}
	if tc0.Function.Name != "get_weather" {
		t.Errorf("Expected function name 'get_weather', got '%s'", tc0.Function.Name)
	}
	tc1 := result.OfAssistant.ToolCalls[1].OfFunction
	if tc1 == nil {
		t.Fatal("Expected OfFunction to be set for tool call 1")
	}
	if tc1.Function.Arguments != `{}` {
		t.Errorf("Expected arguments '{}', got '%s'", tc1.Function.Arguments)
	}
}

func TestFromOpenAIMessage_TextOnly(t *testing.T) {
	oaiMsg := openai.ChatCompletionMessage{
		Content: "The answer is 42",
	}

	result := fromOpenAIMessage(oaiMsg)

	if result.Role != "assistant" {
		t.Errorf("Expected role 'assistant', got '%s'", result.Role)
	}
	if result.Content != "The answer is 42" {
		t.Errorf("Expected content 'The answer is 42', got '%s'", result.Content)
	}
	if len(result.ToolCalls) != 0 {
		t.Errorf("Expected 0 tool calls, got %d", len(result.ToolCalls))
	}
}

func TestFromOpenAIMessage_WithToolCalls(t *testing.T) {
	oaiMsg := openai.ChatCompletionMessage{
		Content: "",
		ToolCalls: []openai.ChatCompletionMessageToolCallUnion{
			{
				ID: "call_abc",
				Function: openai.ChatCompletionMessageFunctionToolCallFunction{
					Name:      "get_weather",
					Arguments: `{"city":"London"}`,
				},
			},
		},
	}

	result := fromOpenAIMessage(oaiMsg)

	if result.Role != "assistant" {
		t.Errorf("Expected role 'assistant', got '%s'", result.Role)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("Expected ID 'call_abc', got '%s'", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("Expected name 'get_weather', got '%s'", tc.Name)
	}
	if tc.Arguments != `{"city":"London"}` {
		t.Errorf("Expected arguments '{\"city\":\"London\"}', got '%s'", tc.Arguments)
	}
}
