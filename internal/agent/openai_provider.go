// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

// OpenAIProvider implements ChatProvider using the OpenAI SDK.
// It supports any OpenAI-compatible endpoint (OpenAI, Ollama, vLLM, Groq, etc.)
// via a configurable base URL.
type OpenAIProvider struct {
	client *openai.Client
}

// NewOpenAIProvider creates a new OpenAI-backed ChatProvider.
// If baseURL is non-empty it overrides the default API endpoint, which allows
// pointing at any OpenAI-compatible server.
func NewOpenAIProvider(apiKey string, baseURL string) *OpenAIProvider {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := openai.NewClient(opts...)
	return &OpenAIProvider{client: &client}
}

func (p *OpenAIProvider) CreateCompletion(ctx context.Context, model string, systemMsg string, messages []Message, tools []ToolDefinition) (*Message, error) {
	oaiMsgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages)+1)
	if systemMsg != "" {
		oaiMsgs = append(oaiMsgs, openai.SystemMessage(systemMsg))
	}
	for _, m := range messages {
		oaiMsgs = append(oaiMsgs, toOpenAIMessage(m))
	}

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(model),
		Messages: oaiMsgs,
	}
	if len(tools) > 0 {
		params.Tools = toOpenAITools(tools)
	}

	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}
	return fromOpenAIMessage(resp.Choices[0].Message), nil
}

// toOpenAITools converts provider-agnostic tool definitions to the OpenAI SDK
// representation.
func toOpenAITools(tools []ToolDefinition) []openai.ChatCompletionToolParam {
	out := make([]openai.ChatCompletionToolParam, len(tools))
	for i, t := range tools {
		out[i] = openai.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        t.Name,
				Description: openai.String(t.Description),
				Parameters:  shared.FunctionParameters(t.Parameters),
			},
		}
	}
	return out
}

// toOpenAIMessage converts a provider-agnostic Message to an OpenAI SDK message
// union.
func toOpenAIMessage(m Message) openai.ChatCompletionMessageParamUnion {
	switch m.Role {
	case "tool":
		return openai.ToolMessage(m.Content, m.ToolCallID)
	case "user":
		return openai.UserMessage(m.Content)
	default: // "assistant"
		asst := openai.ChatCompletionAssistantMessageParam{}
		if m.Content != "" {
			asst.Content.OfString = openai.String(m.Content)
		}
		if len(m.ToolCalls) > 0 {
			asst.ToolCalls = make([]openai.ChatCompletionMessageToolCallParam, len(m.ToolCalls))
			for i, tc := range m.ToolCalls {
				asst.ToolCalls[i] = openai.ChatCompletionMessageToolCallParam{
					ID: tc.ID,
					Function: openai.ChatCompletionMessageToolCallFunctionParam{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				}
			}
		}
		return openai.ChatCompletionMessageParamUnion{OfAssistant: &asst}
	}
}

// fromOpenAIMessage converts an OpenAI SDK response message to the
// provider-agnostic Message type.
func fromOpenAIMessage(m openai.ChatCompletionMessage) *Message {
	msg := &Message{
		Role:    "assistant",
		Content: m.Content,
	}
	if len(m.ToolCalls) > 0 {
		msg.ToolCalls = make([]ToolCall, len(m.ToolCalls))
		for i, tc := range m.ToolCalls {
			msg.ToolCalls[i] = ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			}
		}
	}
	return msg
}
