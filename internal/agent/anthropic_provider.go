// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicProvider implements ChatProvider using the Anthropic SDK.
type AnthropicProvider struct {
	client *anthropic.Client
}

// NewAnthropicProvider creates a new Anthropic-backed ChatProvider.
func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &AnthropicProvider{client: &client}
}

func (p *AnthropicProvider) CreateCompletion(ctx context.Context, model string, systemMsg string, messages []Message, tools []ToolDefinition) (*Message, error) {
	antMsgs := toAnthropicMessages(messages)

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		Messages:  antMsgs,
		MaxTokens: 4096,
	}
	if systemMsg != "" {
		params.System = []anthropic.TextBlockParam{
			{Text: systemMsg},
		}
	}
	if len(tools) > 0 {
		params.Tools = toAnthropicTools(tools)
	}

	resp, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, err
	}
	return fromAnthropicMessage(resp), nil
}

// toAnthropicTools converts provider-agnostic tool definitions to Anthropic SDK
// tool params.
func toAnthropicTools(tools []ToolDefinition) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, len(tools))
	for i, t := range tools {
		// Extract properties and required from the JSON-schema map.
		props, _ := t.Parameters["properties"].(map[string]interface{})
		if props == nil {
			props = map[string]interface{}{}
		}
		var required []string
		if req, ok := t.Parameters["required"].([]interface{}); ok {
			for _, r := range req {
				if s, ok := r.(string); ok {
					required = append(required, s)
				}
			}
		}
		// Also handle the case where required is already []string (e.g. from typed code).
		if req, ok := t.Parameters["required"].([]string); ok {
			required = req
		}

		out[i] = anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: props,
					Required:   required,
				},
			},
		}
	}
	return out
}

// toAnthropicMessages converts provider-agnostic messages to Anthropic SDK
// message params.
//
// Anthropic's API requires:
//   - Only "user" and "assistant" roles (no "tool" role)
//   - Tool results are sent as user messages with ToolResultBlockParam content
//   - Assistant messages with tool calls use ToolUseBlockParam content
func toAnthropicMessages(messages []Message) []anthropic.MessageParam {
	out := make([]anthropic.MessageParam, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case "user":
			out = append(out, anthropic.NewUserMessage(
				anthropic.NewTextBlock(m.Content),
			))
		case "tool":
			out = append(out, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(m.ToolCallID, m.Content, false),
			))
		case "assistant":
			blocks := make([]anthropic.ContentBlockParamUnion, 0)
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				var input json.RawMessage
				if tc.Arguments != "" {
					input = json.RawMessage(tc.Arguments)
				} else {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, anthropic.ContentBlockParamUnion{
					OfToolUse: &anthropic.ToolUseBlockParam{
						ID:    tc.ID,
						Name:  tc.Name,
						Input: input,
					},
				})
			}
			out = append(out, anthropic.NewAssistantMessage(blocks...))
		}
	}
	return out
}

// fromAnthropicMessage converts an Anthropic SDK response to the
// provider-agnostic Message type.
func fromAnthropicMessage(resp *anthropic.Message) *Message {
	msg := &Message{
		Role: "assistant",
	}
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			if msg.Content != "" {
				msg.Content += "\n"
			}
			msg.Content += block.AsText().Text
		case "tool_use":
			tu := block.AsToolUse()
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID:        tu.ID,
				Name:      tu.Name,
				Arguments: string(tu.Input),
			})
		}
	}
	return msg
}
