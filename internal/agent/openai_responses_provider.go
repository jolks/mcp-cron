// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// OpenAIResponsesProvider implements ChatProvider using the OpenAI Responses
// API, which offers better caching, lower cost on reasoning models, and is the
// recommended path for direct OpenAI usage.
type OpenAIResponsesProvider struct {
	client *openai.Client
}

// NewOpenAIResponsesProvider creates a new ChatProvider backed by the Responses API.
// If baseURL is non-empty it overrides the default API endpoint.
func NewOpenAIResponsesProvider(apiKey string, baseURL string) *OpenAIResponsesProvider {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := openai.NewClient(opts...)
	return &OpenAIResponsesProvider{client: &client}
}

func (p *OpenAIResponsesProvider) CreateCompletion(ctx context.Context, model string, systemMsg string, messages []Message, tools []ToolDefinition) (*Message, error) {
	input := toResponsesInput(messages)

	params := responses.ResponseNewParams{
		Model: shared.ResponsesModel(model),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: responses.ResponseInputParam(input),
		},
	}
	if systemMsg != "" {
		params.Instructions = openai.String(systemMsg)
	}
	if len(tools) > 0 {
		params.Tools = toResponsesTools(tools)
	}

	resp, err := p.client.Responses.New(ctx, params)
	if err != nil {
		return nil, err
	}
	return fromResponsesOutput(resp)
}

// toResponsesInput converts provider-agnostic messages to Responses API input items.
func toResponsesInput(messages []Message) []responses.ResponseInputItemUnionParam {
	out := make([]responses.ResponseInputItemUnionParam, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case "user":
			out = append(out, responses.ResponseInputItemParamOfMessage(m.Content, responses.EasyInputMessageRoleUser))
		case "assistant":
			// Re-inject assistant tool calls as function_call input items so
			// the Responses API knows which calls to match with outputs.
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					out = append(out, responses.ResponseInputItemUnionParam{
						OfFunctionCall: &responses.ResponseFunctionToolCallParam{
							CallID:    tc.ID,
							Name:      tc.Name,
							Arguments: tc.Arguments,
						},
					})
				}
			}
			if m.Content != "" {
				out = append(out, responses.ResponseInputItemParamOfMessage(m.Content, responses.EasyInputMessageRoleAssistant))
			}
		case "tool":
			out = append(out, responses.ResponseInputItemUnionParam{
				OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
					CallID: m.ToolCallID,
					Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{
						OfString: openai.String(m.Content),
					},
				},
			})
		}
	}
	return out
}

// toResponsesTools converts provider-agnostic tool definitions to Responses API tools.
func toResponsesTools(tools []ToolDefinition) []responses.ToolUnionParam {
	out := make([]responses.ToolUnionParam, len(tools))
	for i, t := range tools {
		out[i] = responses.ToolUnionParam{
			OfFunction: &responses.FunctionToolParam{
				Name:        t.Name,
				Description: openai.String(t.Description),
				Parameters:  t.Parameters,
			},
		}
	}
	return out
}

// fromResponsesOutput converts a Responses API response to the provider-agnostic
// Message type.
func fromResponsesOutput(resp *responses.Response) (*Message, error) {
	if resp == nil {
		return nil, fmt.Errorf("nil response from Responses API")
	}
	msg := &Message{Role: "assistant"}

	// Collect text and tool calls from output items.
	msg.Content = resp.OutputText()

	for _, item := range resp.Output {
		if item.Type == "function_call" {
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: item.Arguments,
			})
		}
	}
	return msg, nil
}
