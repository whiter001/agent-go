package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/whiter001/agent-go/internal/config"
	"github.com/whiter001/agent-go/internal/llm"
	"github.com/whiter001/agent-go/internal/logging"
	"github.com/whiter001/agent-go/internal/schema"
	"github.com/whiter001/agent-go/internal/tools"
	"github.com/whiter001/agent-go/internal/utils"
)

type Agent struct {
	client         llm.Client
	tools          map[string]tools.Tool
	toolSpecs      []schema.ToolSpec
	messages       []schema.Message
	ephemeral      []schema.Message
	systemPrompt   string
	workspaceDir   string
	maxSteps       int
	tokenLimit     int
	logger         *logging.Logger
	out            io.Writer
	apiTotalTokens int
	cancelled      bool
}

func New(client llm.Client, systemPrompt string, toolList []tools.Tool, maxSteps int, tokenLimit int, workspaceDir string, logger *logging.Logger, out io.Writer) *Agent {
	if out == nil {
		out = os.Stdout
	}
	resolvedWorkspace := config.ExpandPath(workspaceDir)
	if resolvedWorkspace == "" {
		resolvedWorkspace = "."
	}
	_ = os.MkdirAll(resolvedWorkspace, 0o755)
	if !strings.Contains(systemPrompt, "Current Workspace") {
		systemPrompt = strings.TrimSpace(systemPrompt)
		if systemPrompt != "" {
			systemPrompt += "\n\n"
		}
		systemPrompt += fmt.Sprintf("## Current Workspace\nYou are currently working in: `%s`\nAll relative paths will be resolved relative to this directory.", resolvedWorkspace)
	}

	toolMap := make(map[string]tools.Tool, len(toolList))
	toolSpecs := make([]schema.ToolSpec, 0, len(toolList))
	for _, tool := range toolList {
		if tool == nil {
			continue
		}
		spec := tool.Spec()
		toolMap[spec.Name] = tool
		toolSpecs = append(toolSpecs, spec)
	}

	return &Agent{
		client:       client,
		tools:        toolMap,
		toolSpecs:    toolSpecs,
		systemPrompt: systemPrompt,
		workspaceDir: resolvedWorkspace,
		maxSteps:     maxSteps,
		tokenLimit:   tokenLimit,
		logger:       logger,
		out:          out,
		messages:     []schema.Message{{Role: schema.RoleSystem, Content: systemPrompt}},
	}
}

func (a *Agent) AddUserMessage(content string) {
	a.messages = append(a.messages, schema.Message{Role: schema.RoleUser, Content: content})
}

func (a *Agent) SetEphemeralContext(messages []schema.Message) {
	a.ephemeral = append([]schema.Message(nil), messages...)
}

func (a *Agent) ClearEphemeralContext() {
	a.ephemeral = nil
}

func (a *Agent) History() []schema.Message {
	return append([]schema.Message(nil), a.messages...)
}

func (a *Agent) MessageCount() int {
	return len(a.messages)
}

func (a *Agent) ToolCount() int {
	return len(a.tools)
}

func (a *Agent) APITotalTokens() int {
	return a.apiTotalTokens
}

func (a *Agent) Run(ctx context.Context) (string, error) {
	if a.client == nil {
		return "", fmt.Errorf("llm client not configured")
	}
	if a.logger != nil {
		if path, err := a.logger.StartRun(); err == nil && path != "" {
			_, _ = fmt.Fprintf(a.out, "Log file: %s\n", path)
		}
	}

	start := time.Now()
	for step := 0; step < a.maxSteps; step++ {
		if ctx.Err() != nil {
			a.cancelled = true
			return "Task cancelled by user.", ctx.Err()
		}

		if err := a.compactHistory(ctx); err != nil {
			_, _ = fmt.Fprintf(a.out, "History compaction skipped: %v\n", err)
		}

		activeMessages := a.activeMessages()
		if a.logger != nil {
			a.logger.LogRequest(activeMessages, a.toolSpecs)
		}

		response, err := a.client.Generate(ctx, activeMessages, a.toolSpecs)
		if err != nil {
			return "", err
		}
		if response.Usage != nil {
			a.apiTotalTokens = response.Usage.TotalTokens
		}
		if a.logger != nil {
			a.logger.LogResponse(response)
		}

		a.messages = append(a.messages, schema.Message{
			Role:      schema.RoleAssistant,
			Content:   response.Content,
			Thinking:  response.Thinking,
			ToolCalls: append([]schema.ToolCall(nil), response.ToolCalls...),
		})

		if response.Thinking != "" {
			_, _ = fmt.Fprintln(a.out, "Thinking:")
			_, _ = fmt.Fprintln(a.out, response.Thinking)
		}
		if response.Content != "" {
			_, _ = fmt.Fprintln(a.out, "Assistant:")
			_, _ = fmt.Fprintln(a.out, response.Content)
		}

		if len(response.ToolCalls) == 0 {
			_, _ = fmt.Fprintf(a.out, "Step %d completed in %s\n", step+1, time.Since(start).Truncate(time.Millisecond))
			return response.Content, nil
		}

		for _, toolCall := range response.ToolCalls {
			result := a.executeTool(ctx, toolCall)
			toolContent := result.Content
			if !result.Success {
				toolContent = "Error: " + result.Error
			}
			a.messages = append(a.messages, schema.Message{
				Role:       schema.RoleTool,
				Content:    toolContent,
				ToolCallID: toolCall.ID,
				Name:       toolCall.Function.Name,
			})
			if a.logger != nil {
				a.logger.LogToolResult(toolCall.Function.Name, toolCall.Function.Arguments, result.Success, result.Content, result.Error)
			}
			if result.Success {
				_, _ = fmt.Fprintf(a.out, "Tool %s: %s\n", toolCall.Function.Name, result.Content)
			} else {
				_, _ = fmt.Fprintf(a.out, "Tool %s error: %s\n", toolCall.Function.Name, result.Error)
			}
		}

		_, _ = fmt.Fprintf(a.out, "Step %d completed in %s\n", step+1, time.Since(start).Truncate(time.Millisecond))
	}

	return fmt.Sprintf("Task couldn't be completed after %d steps.", a.maxSteps), nil
}

func (a *Agent) activeMessages() []schema.Message {
	messages := append([]schema.Message(nil), a.messages...)
	if len(a.ephemeral) > 0 {
		messages = append(messages, a.ephemeral...)
	}
	return messages
}

func (a *Agent) executeTool(ctx context.Context, call schema.ToolCall) tools.Result {
	tool, ok := a.tools[call.Function.Name]
	if !ok {
		return tools.Result{Error: fmt.Sprintf("unknown tool: %s", call.Function.Name)}
	}
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(a.out, "Tool panic recovered: %v\n", r)
		}
	}()
	return tool.Execute(ctx, call.Function.Arguments)
}

func (a *Agent) compactHistory(ctx context.Context) error {
	if a.tokenLimit <= 0 {
		return nil
	}
	if estimateMessages(a.messages) <= a.tokenLimit && a.apiTotalTokens <= a.tokenLimit {
		return nil
	}
	userIndices := make([]int, 0)
	for index, message := range a.messages {
		if index == 0 {
			continue
		}
		if message.Role == schema.RoleUser {
			userIndices = append(userIndices, index)
		}
	}
	if len(userIndices) == 0 {
		return nil
	}

	compressed := []schema.Message{a.messages[0]}
	for i, userIndex := range userIndices {
		compressed = append(compressed, a.messages[userIndex])
		nextBoundary := len(a.messages)
		if i+1 < len(userIndices) {
			nextBoundary = userIndices[i+1]
		}
		executionMessages := a.messages[userIndex+1 : nextBoundary]
		if len(executionMessages) == 0 {
			continue
		}
		summary := a.summarizeExecution(ctx, executionMessages, i+1)
		if summary != "" {
			compressed = append(compressed, schema.Message{Role: schema.RoleUser, Content: "[Assistant Execution Summary]\n\n" + summary})
		}
	}
	a.messages = compressed
	return nil
}

func (a *Agent) summarizeExecution(ctx context.Context, messages []schema.Message, round int) string {
	if len(messages) == 0 || a.client == nil {
		return ""
	}
	content := buildExecutionSummary(messages, round)
	if content == "" {
		return ""
	}
	content = utils.TruncateMiddle(content, 12000)
	summaryMessages := []schema.Message{
		{Role: schema.RoleSystem, Content: "You are an assistant skilled at summarizing agent execution processes."},
		{Role: schema.RoleUser, Content: fmt.Sprintf("Please provide a concise summary of the following Agent execution process:\n\n%s\n\nRequirements:\n1. Focus on what tasks were completed and which tools were called\n2. Keep key execution results and important findings\n3. Be concise and clear\n4. Use English\n5. Do not include user request details, only summarize the Agent's execution process", content)},
	}
	response, err := a.client.Generate(ctx, summaryMessages, nil)
	if err != nil {
		return fallbackSummary(messages, round)
	}
	if strings.TrimSpace(response.Content) == "" {
		return fallbackSummary(messages, round)
	}
	return strings.TrimSpace(response.Content)
}

func buildExecutionSummary(messages []schema.Message, round int) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Round %d execution process:\n\n", round))
	for _, message := range messages {
		switch message.Role {
		case schema.RoleAssistant:
			content := message.Content
			if content != "" {
				builder.WriteString("Assistant: ")
				builder.WriteString(content)
				builder.WriteString("\n")
			}
			if len(message.ToolCalls) > 0 {
				toolNames := make([]string, 0, len(message.ToolCalls))
				for _, toolCall := range message.ToolCalls {
					toolNames = append(toolNames, toolCall.Function.Name)
				}
				builder.WriteString("  → Called tools: ")
				builder.WriteString(strings.Join(toolNames, ", "))
				builder.WriteString("\n")
			}
		case schema.RoleTool:
			preview := message.Content
			preview = utils.TruncateMiddle(preview, 500)
			builder.WriteString("  ← Tool returned: ")
			builder.WriteString(preview)
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func fallbackSummary(messages []schema.Message, round int) string {
	return utils.TruncateMiddle(buildExecutionSummary(messages, round), 2000)
}

func estimateMessages(messages []schema.Message) int {
	total := 0
	for _, message := range messages {
		total += estimateText(message.Content)
		total += estimateText(message.Thinking)
		for _, call := range message.ToolCalls {
			arguments, _ := json.Marshal(call.Function.Arguments)
			total += estimateText(call.Function.Name)
			total += len(arguments) / 4
		}
		total += 4
	}
	return total
}

func estimateText(text string) int {
	if text == "" {
		return 0
	}
	return len([]rune(text)) / 4
}

func (a *Agent) Cancel() {
	a.cancelled = true
}
