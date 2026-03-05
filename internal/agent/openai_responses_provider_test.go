// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"testing"

	"github.com/openai/openai-go/v3/responses"
)

func TestToResponsesInput_UserMessage(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "Hello"}}
	result := toResponsesInput(msgs)
	if len(result) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(result))
	}
	if result[0].OfMessage == nil {
		t.Fatal("Expected OfMessage to be set")
	}
	if result[0].OfMessage.Role != responses.EasyInputMessageRoleUser {
		t.Errorf("Expected role 'user', got '%s'", result[0].OfMessage.Role)
	}
}

func TestToResponsesInput_AssistantTextOnly(t *testing.T) {
	msgs := []Message{{Role: "assistant", Content: "I can help"}}
	result := toResponsesInput(msgs)
	if len(result) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(result))
	}
	if result[0].OfMessage == nil {
		t.Fatal("Expected OfMessage to be set")
	}
	if result[0].OfMessage.Role != responses.EasyInputMessageRoleAssistant {
		t.Errorf("Expected role 'assistant', got '%s'", result[0].OfMessage.Role)
	}
}

func TestToResponsesInput_AssistantWithToolCalls(t *testing.T) {
	msgs := []Message{
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{"city":"NYC"}`},
				{ID: "call_2", Name: "list_files", Arguments: `{}`},
			},
		},
	}
	result := toResponsesInput(msgs)
	// Should produce 2 function_call items (no text-only message since Content is empty)
	if len(result) != 2 {
		t.Fatalf("Expected 2 items, got %d", len(result))
	}
	if result[0].OfFunctionCall == nil {
		t.Fatal("Expected OfFunctionCall to be set")
	}
	if result[0].OfFunctionCall.CallID != "call_1" {
		t.Errorf("Expected CallID 'call_1', got '%s'", result[0].OfFunctionCall.CallID)
	}
	if result[0].OfFunctionCall.Name != "get_weather" {
		t.Errorf("Expected Name 'get_weather', got '%s'", result[0].OfFunctionCall.Name)
	}
	if result[1].OfFunctionCall == nil {
		t.Fatal("Expected OfFunctionCall to be set for second item")
	}
	if result[1].OfFunctionCall.CallID != "call_2" {
		t.Errorf("Expected CallID 'call_2', got '%s'", result[1].OfFunctionCall.CallID)
	}
}

func TestToResponsesInput_AssistantWithToolCallsAndContent(t *testing.T) {
	msgs := []Message{
		{
			Role:    "assistant",
			Content: "Let me check",
			ToolCalls: []ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{"city":"NYC"}`},
			},
		},
	}
	result := toResponsesInput(msgs)
	// 1 function_call + 1 assistant text message
	if len(result) != 2 {
		t.Fatalf("Expected 2 items, got %d", len(result))
	}
	if result[0].OfFunctionCall == nil {
		t.Fatal("Expected first item to be OfFunctionCall")
	}
	if result[1].OfMessage == nil {
		t.Fatal("Expected second item to be OfMessage")
	}
}

func TestToResponsesInput_ToolMessage(t *testing.T) {
	msgs := []Message{{Role: "tool", Content: "result data", ToolCallID: "call_123"}}
	result := toResponsesInput(msgs)
	if len(result) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(result))
	}
	if result[0].OfFunctionCallOutput == nil {
		t.Fatal("Expected OfFunctionCallOutput to be set")
	}
	if result[0].OfFunctionCallOutput.CallID != "call_123" {
		t.Errorf("Expected CallID 'call_123', got '%s'", result[0].OfFunctionCallOutput.CallID)
	}
}

func TestToResponsesTools(t *testing.T) {
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
	}

	result := toResponsesTools(tools)
	if len(result) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(result))
	}
	if result[0].OfFunction == nil {
		t.Fatal("Expected OfFunction to be set")
	}
	if result[0].OfFunction.Name != "get_weather" {
		t.Errorf("Expected name 'get_weather', got '%s'", result[0].OfFunction.Name)
	}
}

func TestFromResponsesOutput_TextOnly(t *testing.T) {
	resp := &responses.Response{
		Output: []responses.ResponseOutputItemUnion{
			{
				Type: "message",
				Content: []responses.ResponseOutputMessageContentUnion{
					{Type: "output_text", Text: "The answer is 42"},
				},
			},
		},
	}

	msg, err := fromResponsesOutput(resp)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if msg.Role != "assistant" {
		t.Errorf("Expected role 'assistant', got '%s'", msg.Role)
	}
	if msg.Content != "The answer is 42" {
		t.Errorf("Expected content 'The answer is 42', got '%s'", msg.Content)
	}
	if len(msg.ToolCalls) != 0 {
		t.Errorf("Expected 0 tool calls, got %d", len(msg.ToolCalls))
	}
}

func TestFromResponsesOutput_WithToolCalls(t *testing.T) {
	resp := &responses.Response{
		Output: []responses.ResponseOutputItemUnion{
			{
				Type:      "function_call",
				CallID:    "call_abc",
				Name:      "get_weather",
				Arguments: `{"city":"London"}`,
			},
		},
	}

	msg, err := fromResponsesOutput(resp)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
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

func TestFromResponsesOutput_Nil(t *testing.T) {
	_, err := fromResponsesOutput(nil)
	if err == nil {
		t.Fatal("Expected error for nil response, got nil")
	}
}
