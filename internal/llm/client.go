package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/whiter001/agent-go/internal/config"
	"github.com/whiter001/agent-go/internal/schema"
)

type Provider string

const (
	ProviderAnthropic Provider = "anthropic"
	ProviderOpenAI    Provider = "openai"
)

type RetryCallback func(error, int)

type Client interface {
	Generate(ctx context.Context, messages []schema.Message, tools []schema.ToolSpec) (schema.LLMResponse, error)
	SetRetryCallback(callback RetryCallback)
}

type retrySupport struct {
	apiKey        string
	apiBase       string
	model         string
	retry         config.RetryConfig
	retryCallback RetryCallback
	httpClient    *http.Client
}

func newRetrySupport(apiKey, apiBase, model string, retry config.RetryConfig) retrySupport {
	return retrySupport{
		apiKey:     strings.TrimSpace(apiKey),
		apiBase:    strings.TrimSpace(apiBase),
		model:      strings.TrimSpace(model),
		retry:      retry,
		httpClient: &http.Client{},
	}
}

func (r *retrySupport) SetRetryCallback(callback RetryCallback) {
	r.retryCallback = callback
}

func (r *retrySupport) withRetry(ctx context.Context, operation func() (schema.LLMResponse, error)) (schema.LLMResponse, error) {
	if !r.retry.Enabled {
		return operation()
	}
	maxRetries := r.retry.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		response, err := operation()
		if err == nil {
			return response, nil
		}
		lastErr = err
		if attempt == maxRetries || ctx.Err() != nil {
			break
		}
		if r.retryCallback != nil {
			r.retryCallback(err, attempt+1)
		}
		delay := retryDelay(r.retry, attempt)
		select {
		case <-ctx.Done():
			return schema.LLMResponse{}, ctx.Err()
		case <-time.After(delay):
		}
	}
	return schema.LLMResponse{}, lastErr
}

func retryDelay(cfg config.RetryConfig, attempt int) time.Duration {
	initial := cfg.InitialDelay
	if initial <= 0 {
		initial = 1
	}
	base := cfg.ExponentialBase
	if base <= 0 {
		base = 2
	}
	maxDelay := cfg.MaxDelay
	if maxDelay <= 0 {
		maxDelay = 60
	}
	delay := initial * pow(base, attempt)
	if delay > maxDelay {
		delay = maxDelay
	}
	return time.Duration(delay * float64(time.Second))
}

func pow(base float64, exponent int) float64 {
	if exponent <= 0 {
		return 1
	}
	result := 1.0
	for i := 0; i < exponent; i++ {
		result *= base
	}
	return result
}

func NewClient(apiKey, apiBase, model, provider string, retry config.RetryConfig) (Client, error) {
	normalizedProvider := Provider(strings.ToLower(strings.TrimSpace(provider)))
	if normalizedProvider == "" {
		normalizedProvider = ProviderAnthropic
	}
	switch normalizedProvider {
	case ProviderAnthropic:
		return newAnthropicClient(apiKey, apiBase, model, retry), nil
	case ProviderOpenAI:
		return newOpenAIClient(apiKey, apiBase, model, retry), nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
}

type anthropicClient struct {
	retrySupport
	baseURL string
}

func newAnthropicClient(apiKey, apiBase, model string, retry config.RetryConfig) *anthropicClient {
	return &anthropicClient{
		retrySupport: newRetrySupport(apiKey, apiBase, model, retry),
		baseURL:      resolveBaseURL(apiBase, ProviderAnthropic),
	}
}

func (c *anthropicClient) SetRetryCallback(callback RetryCallback) {
	c.retrySupport.SetRetryCallback(callback)
}

func (c *anthropicClient) Generate(ctx context.Context, messages []schema.Message, tools []schema.ToolSpec) (schema.LLMResponse, error) {
	return c.withRetry(ctx, func() (schema.LLMResponse, error) {
		return c.generateOnce(ctx, messages, tools)
	})
}

func (c *anthropicClient) generateOnce(ctx context.Context, messages []schema.Message, tools []schema.ToolSpec) (schema.LLMResponse, error) {
	systemPrompt, apiMessages := convertMessagesForAnthropic(messages)
	request := anthropicRequest{
		Model:     c.model,
		MaxTokens: 4096,
		System:    systemPrompt,
		Messages:  apiMessages,
		Thinking: &anthropicThinking{
			Type:         "enabled",
			BudgetTokens: 1024,
		},
	}
	if len(tools) != 0 {
		request.Tools = make([]anthropicTool, 0, len(tools))
		for _, tool := range tools {
			request.Tools = append(request.Tools, anthropicTool{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
			})
		}
	}

	var response anthropicResponse
	if err := c.postJSON(ctx, c.baseURL+"/v1/messages", request, &response); err != nil {
		return schema.LLMResponse{}, err
	}

	return parseAnthropicResponse(response), nil
}

type openAIClient struct {
	retrySupport
	baseURL string
}

func newOpenAIClient(apiKey, apiBase, model string, retry config.RetryConfig) *openAIClient {
	return &openAIClient{
		retrySupport: newRetrySupport(apiKey, apiBase, model, retry),
		baseURL:      resolveBaseURL(apiBase, ProviderOpenAI),
	}
}

func (c *openAIClient) SetRetryCallback(callback RetryCallback) {
	c.retrySupport.SetRetryCallback(callback)
}

func (c *openAIClient) Generate(ctx context.Context, messages []schema.Message, tools []schema.ToolSpec) (schema.LLMResponse, error) {
	return c.withRetry(ctx, func() (schema.LLMResponse, error) {
		return c.generateOnce(ctx, messages, tools)
	})
}

func (c *openAIClient) generateOnce(ctx context.Context, messages []schema.Message, tools []schema.ToolSpec) (schema.LLMResponse, error) {
	apiMessages := convertMessagesForOpenAI(messages)
	request := openAIRequest{
		Model:      c.model,
		Messages:   apiMessages,
		ToolChoice: "auto",
	}
	if len(tools) != 0 {
		request.Tools = make([]openAITool, 0, len(tools))
		for _, tool := range tools {
			request.Tools = append(request.Tools, openAITool{
				Type: "function",
				Function: openAIFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			})
		}
	}

	var response openAIResponse
	if err := c.postJSON(ctx, c.baseURL+"/chat/completions", request, &response); err != nil {
		return schema.LLMResponse{}, err
	}
	return parseOpenAIResponse(response), nil
}

func resolveBaseURL(apiBase string, provider Provider) string {
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if base == "" {
		base = "https://api.minimaxi.com"
	}
	switch provider {
	case ProviderAnthropic:
		if strings.Contains(base, "/anthropic") {
			return base
		}
		if strings.HasSuffix(base, "/v1") {
			return strings.TrimSuffix(base, "/v1") + "/anthropic"
		}
		return base + "/anthropic"
	case ProviderOpenAI:
		if strings.HasSuffix(base, "/v1") {
			return base
		}
		if strings.Contains(base, "/anthropic") {
			return strings.TrimSuffix(base, "/anthropic") + "/v1"
		}
		return base + "/v1"
	default:
		return base
	}
}

func (c *retrySupport) postJSON(ctx context.Context, endpoint string, requestBody any, responseBody any) error {
	data, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("content-type", "application/json")
	request.Header.Set("accept", "application/json")
	if strings.Contains(endpoint, "/anthropic/") {
		request.Header.Set("x-api-key", c.apiKey)
		request.Header.Set("anthropic-version", "2023-06-01")
	} else {
		request.Header.Set("authorization", "Bearer "+c.apiKey)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = response.Status
		}
		return &httpStatusError{statusCode: response.StatusCode, message: message}
	}

	return json.NewDecoder(response.Body).Decode(responseBody)
}

type httpStatusError struct {
	statusCode int
	message    string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("http %d: %s", e.statusCode, e.message)
}

func (e *httpStatusError) retryable() bool {
	return e.statusCode == http.StatusTooManyRequests || e.statusCode >= 500
}

func parseAnthropicResponse(response anthropicResponse) schema.LLMResponse {
	contentParts := make([]string, 0)
	thinkingParts := make([]string, 0)
	toolCalls := make([]schema.ToolCall, 0)
	for _, block := range response.Content {
		switch block.Type {
		case "text":
			if trimmed := strings.TrimSpace(block.Text); trimmed != "" {
				contentParts = append(contentParts, trimmed)
			}
		case "thinking":
			if trimmed := strings.TrimSpace(block.Text); trimmed != "" {
				thinkingParts = append(thinkingParts, trimmed)
			}
		case "tool_use":
			arguments := map[string]any{}
			if block.Input != nil {
				arguments = block.Input
			}
			toolCalls = append(toolCalls, schema.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: schema.FunctionCall{
					Name:      block.Name,
					Arguments: arguments,
				},
			})
		}
	}
	usage := &schema.TokenUsage{
		PromptTokens:     response.Usage.InputTokens,
		CompletionTokens: response.Usage.OutputTokens,
		TotalTokens:      response.Usage.InputTokens + response.Usage.OutputTokens,
	}
	if usage.TotalTokens == 0 {
		usage = nil
	}
	return schema.LLMResponse{
		Content:      strings.TrimSpace(strings.Join(contentParts, "\n")),
		Thinking:     strings.TrimSpace(strings.Join(thinkingParts, "\n")),
		ToolCalls:    toolCalls,
		FinishReason: response.StopReason,
		Usage:        usage,
	}
}

func parseOpenAIResponse(response openAIResponse) schema.LLMResponse {
	if len(response.Choices) == 0 {
		return schema.LLMResponse{FinishReason: "stop"}
	}
	choice := response.Choices[0]
	toolCalls := make([]schema.ToolCall, 0, len(choice.Message.ToolCalls))
	for _, call := range choice.Message.ToolCalls {
		arguments := map[string]any{}
		if strings.TrimSpace(call.Function.Arguments) != "" {
			_ = json.Unmarshal([]byte(call.Function.Arguments), &arguments)
		}
		toolCalls = append(toolCalls, schema.ToolCall{
			ID:   call.ID,
			Type: call.Type,
			Function: schema.FunctionCall{
				Name:      call.Function.Name,
				Arguments: arguments,
			},
		})
	}
	content := ""
	if choice.Message.Content != nil {
		content = strings.TrimSpace(*choice.Message.Content)
	}
	usage := &schema.TokenUsage{
		PromptTokens:     response.Usage.PromptTokens,
		CompletionTokens: response.Usage.CompletionTokens,
		TotalTokens:      response.Usage.TotalTokens,
	}
	if usage.TotalTokens == 0 {
		usage = nil
	}
	return schema.LLMResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		FinishReason: choice.FinishReason,
		Usage:        usage,
	}
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	Thinking  *anthropicThinking `json:"thinking,omitempty"`
}

type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
}

type anthropicBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   any            `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type anthropicResponse struct {
	Content    []anthropicBlock `json:"content"`
	StopReason string           `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type openAIRequest struct {
	Model      string          `json:"model"`
	Messages   []openAIMessage `json:"messages"`
	Tools      []openAITool    `json:"tools,omitempty"`
	ToolChoice string          `json:"tool_choice,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    *string          `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Role      string           `json:"role"`
			Content   *string          `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func convertMessagesForAnthropic(messages []schema.Message) (string, []anthropicMessage) {
	var systemParts []string
	apiMessages := make([]anthropicMessage, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case schema.RoleSystem:
			if trimmed := strings.TrimSpace(message.Content); trimmed != "" {
				systemParts = append(systemParts, trimmed)
			}
		case schema.RoleUser:
			apiMessages = append(apiMessages, anthropicMessage{
				Role:    "user",
				Content: []anthropicBlock{{Type: "text", Text: message.Content}},
			})
		case schema.RoleAssistant:
			blocks := make([]anthropicBlock, 0, 1+len(message.ToolCalls))
			if trimmed := strings.TrimSpace(message.Content); trimmed != "" {
				blocks = append(blocks, anthropicBlock{Type: "text", Text: trimmed})
			}
			for _, toolCall := range message.ToolCalls {
				blocks = append(blocks, anthropicBlock{
					Type:  "tool_use",
					ID:    toolCall.ID,
					Name:  toolCall.Function.Name,
					Input: toolCall.Function.Arguments,
				})
			}
			if len(blocks) > 0 {
				apiMessages = append(apiMessages, anthropicMessage{Role: "assistant", Content: blocks})
			}
		case schema.RoleTool:
			blocks := []anthropicBlock{{
				Type:      "tool_result",
				ToolUseID: message.ToolCallID,
				Content:   message.Content,
				IsError:   strings.HasPrefix(message.Content, "Error:"),
			}}
			apiMessages = append(apiMessages, anthropicMessage{Role: "user", Content: blocks})
		}
	}
	return strings.Join(systemParts, "\n\n"), apiMessages
}

func convertMessagesForOpenAI(messages []schema.Message) []openAIMessage {
	apiMessages := make([]openAIMessage, 0, len(messages))
	var systemParts []string
	for _, message := range messages {
		switch message.Role {
		case schema.RoleSystem:
			if trimmed := strings.TrimSpace(message.Content); trimmed != "" {
				systemParts = append(systemParts, trimmed)
			}
		case schema.RoleUser:
			content := message.Content
			apiMessages = append(apiMessages, openAIMessage{Role: "user", Content: &content})
		case schema.RoleAssistant:
			content := message.Content
			entry := openAIMessage{Role: "assistant"}
			if content != "" {
				entry.Content = &content
			}
			if len(message.ToolCalls) > 0 {
				entry.ToolCalls = make([]openAIToolCall, 0, len(message.ToolCalls))
				for _, toolCall := range message.ToolCalls {
					arguments, _ := json.Marshal(toolCall.Function.Arguments)
					entry.ToolCalls = append(entry.ToolCalls, openAIToolCall{
						ID:   toolCall.ID,
						Type: toolCall.Type,
						Function: openAIToolFunction{
							Name:      toolCall.Function.Name,
							Arguments: string(arguments),
						},
					})
				}
			}
			apiMessages = append(apiMessages, entry)
		case schema.RoleTool:
			content := message.Content
			apiMessages = append(apiMessages, openAIMessage{Role: "tool", Content: &content, ToolCallID: message.ToolCallID})
		}
	}
	if len(systemParts) > 0 {
		content := strings.Join(systemParts, "\n\n")
		apiMessages = append([]openAIMessage{{Role: "system", Content: &content}}, apiMessages...)
	}
	return apiMessages
}

func (r *retrySupport) shouldRetry(err error) bool {
	if err == nil {
		return false
	}
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		return statusErr.retryable()
	}
	return true
}

func (r *retrySupport) DoRequest(ctx context.Context, request *http.Request) (*http.Response, error) {
	return r.httpClient.Do(request)
}

func (r *retrySupport) postJSONWithRetry(ctx context.Context, endpoint string, requestBody any, responseBody any) error {
	return r.postJSON(ctx, endpoint, requestBody, responseBody)
}
