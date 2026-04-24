package llm

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/whiter001/agent-go/internal/config"
	"github.com/whiter001/agent-go/internal/schema"
)

func strPtr(value string) *string {
	return &value
}

func TestRetryDelay(t *testing.T) {
	if got, want := retryDelay(config.RetryConfig{}, 0), time.Second; got != want {
		t.Fatalf("retryDelay() = %v, want %v", got, want)
	}

	if got, want := retryDelay(config.RetryConfig{InitialDelay: 2, ExponentialBase: 3, MaxDelay: 5}, 2), 5*time.Second; got != want {
		t.Fatalf("retryDelay() = %v, want %v", got, want)
	}
}

func TestResolveBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		provider Provider
		want     string
	}{
		{name: "anthropic default base", base: "", provider: ProviderAnthropic, want: "https://api.minimaxi.com/anthropic"},
		{name: "anthropic v1 to anthropic", base: "https://example.com/v1/", provider: ProviderAnthropic, want: "https://example.com/anthropic"},
		{name: "openai default base", base: "", provider: ProviderOpenAI, want: "https://api.minimaxi.com/v1"},
		{name: "openai anthropic base", base: "https://example.com/anthropic/", provider: ProviderOpenAI, want: "https://example.com/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveBaseURL(tt.base, tt.provider); got != tt.want {
				t.Fatalf("resolveBaseURL(%q, %q) = %q, want %q", tt.base, tt.provider, got, tt.want)
			}
		})
	}
}

func TestNewClientProviderSelection(t *testing.T) {
	client, err := NewClient("api-key", "https://example.com/anthropic/", "model", "", config.RetryConfig{})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	anthropic, ok := client.(*anthropicClient)
	if !ok {
		t.Fatalf("NewClient() type = %T, want *anthropicClient", client)
	}
	if anthropic.baseURL != "https://example.com/anthropic" {
		t.Fatalf("anthropic baseURL = %q, want %q", anthropic.baseURL, "https://example.com/anthropic")
	}

	client, err = NewClient("api-key", "https://example.com/anthropic/", "model", " OpenAI ", config.RetryConfig{})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	openai, ok := client.(*openAIClient)
	if !ok {
		t.Fatalf("NewClient() type = %T, want *openAIClient", client)
	}
	if openai.baseURL != "https://example.com/v1" {
		t.Fatalf("openAI baseURL = %q, want %q", openai.baseURL, "https://example.com/v1")
	}

	if _, err := NewClient("api-key", "", "model", "unsupported", config.RetryConfig{}); err == nil {
		t.Fatalf("NewClient() error = nil, want error")
	}
}

func TestConvertMessagesForAnthropicAndOpenAI(t *testing.T) {
	messages := []schema.Message{
		{Role: schema.RoleSystem, Content: " first system "},
		{Role: schema.RoleSystem, Content: "second system"},
		{Role: schema.RoleUser, Content: " hello "},
		{
			Role:    schema.RoleAssistant,
			Content: " answer ",
			ToolCalls: []schema.ToolCall{{
				ID:   "tool-1",
				Type: "function",
				Function: schema.FunctionCall{
					Name:      "lookup",
					Arguments: map[string]any{"id": 7},
				},
			}},
		},
		{Role: schema.RoleTool, Content: "result ok", ToolCallID: "tool-1"},
	}

	systemPrompt, anthropicMessages := convertMessagesForAnthropic(messages)
	if systemPrompt != "first system\n\nsecond system" {
		t.Fatalf("systemPrompt = %q, want %q", systemPrompt, "first system\n\nsecond system")
	}
	wantAnthropic := []anthropicMessage{
		{
			Role: "user",
			Content: []anthropicBlock{{
				Type: "text",
				Text: " hello ",
			}},
		},
		{
			Role: "assistant",
			Content: []anthropicBlock{{
				Type: "text",
				Text: "answer",
			}, {
				Type:  "tool_use",
				ID:    "tool-1",
				Name:  "lookup",
				Input: map[string]any{"id": 7},
			}},
		},
		{
			Role: "user",
			Content: []anthropicBlock{{
				Type:      "tool_result",
				ToolUseID: "tool-1",
				Content:   "result ok",
				IsError:   false,
			}},
		},
	}
	if !reflect.DeepEqual(anthropicMessages, wantAnthropic) {
		t.Fatalf("convertMessagesForAnthropic() = %#v, want %#v", anthropicMessages, wantAnthropic)
	}

	openAIMessages := convertMessagesForOpenAI(messages)
	wantOpenAI := []openAIMessage{
		{Role: "system", Content: strPtr("first system\n\nsecond system")},
		{Role: "user", Content: strPtr(" hello ")},
		{
			Role:    "assistant",
			Content: strPtr(" answer "),
			ToolCalls: []openAIToolCall{{
				ID:   "tool-1",
				Type: "function",
				Function: openAIToolFunction{
					Name:      "lookup",
					Arguments: `{"id":7}`,
				},
			}},
		},
		{Role: "tool", Content: strPtr("result ok"), ToolCallID: "tool-1"},
	}
	if !reflect.DeepEqual(openAIMessages, wantOpenAI) {
		t.Fatalf("convertMessagesForOpenAI() = %#v, want %#v", openAIMessages, wantOpenAI)
	}
}

func TestParseAnthropicResponse(t *testing.T) {
	var response anthropicResponse
	if err := json.Unmarshal([]byte(`{
		"content": [
			{"type": "text", "text": " hello "},
			{"type": "thinking", "text": " reasoning "},
			{"type": "tool_use", "id": "tool-1", "name": "lookup", "input": {"id": 7}}
		],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 3, "output_tokens": 4}
	}`), &response); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	got := parseAnthropicResponse(response)
	want := schema.LLMResponse{
		Content:      "hello",
		Thinking:     "reasoning",
		ToolCalls:    []schema.ToolCall{{ID: "tool-1", Type: "function", Function: schema.FunctionCall{Name: "lookup", Arguments: map[string]any{"id": float64(7)}}}},
		FinishReason: "end_turn",
		Usage:        &schema.TokenUsage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseAnthropicResponse() = %#v, want %#v", got, want)
	}
}

func TestParseOpenAIResponse(t *testing.T) {
	var response openAIResponse
	if err := json.Unmarshal([]byte(`{
		"choices": [
			{
				"message": {
					"role": "assistant",
					"content": " response ",
					"tool_calls": [
						{
							"id": "call-1",
							"type": "function",
							"function": {
								"name": "lookup",
								"arguments": "{\"id\":7}"
							}
						}
					]
				},
				"finish_reason": "stop"
			}
		],
		"usage": {"prompt_tokens": 5, "completion_tokens": 6, "total_tokens": 11}
	}`), &response); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	got := parseOpenAIResponse(response)
	want := schema.LLMResponse{
		Content:      "response",
		ToolCalls:    []schema.ToolCall{{ID: "call-1", Type: "function", Function: schema.FunctionCall{Name: "lookup", Arguments: map[string]any{"id": float64(7)}}}},
		FinishReason: "stop",
		Usage:        &schema.TokenUsage{PromptTokens: 5, CompletionTokens: 6, TotalTokens: 11},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseOpenAIResponse() = %#v, want %#v", got, want)
	}
}