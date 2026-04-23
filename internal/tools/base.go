package tools

import (
	"context"

	"github.com/whiter001/agent-go/internal/schema"
)

type Result struct {
	Success bool
	Content string
	Error   string
}

type Tool interface {
	Spec() schema.ToolSpec
	Execute(ctx context.Context, args map[string]any) Result
}

func Spec(name, description string, inputSchema map[string]any) schema.ToolSpec {
	return schema.ToolSpec{Name: name, Description: description, InputSchema: inputSchema}
}
