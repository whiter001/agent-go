package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/whiter001/agent-go/internal/schema"
	"github.com/whiter001/agent-go/internal/store"
)

type RememberTool struct {
	memoryStore *store.Store
}

func NewRememberTool(memoryStore *store.Store) *RememberTool {
	return &RememberTool{memoryStore: memoryStore}
}

func (t *RememberTool) Spec() schema.ToolSpec {
	return Spec("remember", "Store a durable agent memory note in ~/.agent-go/.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{"type": "string", "description": "Short note title"},
			"content": map[string]any{"type": "string", "description": "The durable fact or workflow to remember"},
			"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional tags for recall"},
		},
		"required": []string{"content"},
	})
}

func (t *RememberTool) Execute(ctx context.Context, args map[string]any) Result {
	if t.memoryStore == nil {
		return Result{Error: "memory store not available"}
	}
	content := stringArg(args, "content")
	title := stringArg(args, "title")
	tags := stringSliceArg(args, "tags")
	entry := t.memoryStore.StoreMemory(content, title, tags)
	return Result{Success: true, Content: fmt.Sprintf("Stored memory #%d in %s", entry.ID, t.memoryStore.MemoryPath())}
}

type RememberUserTool struct {
	memoryStore *store.Store
}

func NewRememberUserTool(memoryStore *store.Store) *RememberUserTool {
	return &RememberUserTool{memoryStore: memoryStore}
}

func (t *RememberUserTool) Spec() schema.ToolSpec {
	return Spec("remember_user", "Store a durable user profile fact in ~/.agent-go/.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{"type": "string", "description": "Short profile title"},
			"content": map[string]any{"type": "string", "description": "The user preference or profile fact to remember"},
			"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional tags for recall"},
		},
		"required": []string{"content"},
	})
}

func (t *RememberUserTool) Execute(ctx context.Context, args map[string]any) Result {
	if t.memoryStore == nil {
		return Result{Error: "memory store not available"}
	}
	content := stringArg(args, "content")
	title := stringArg(args, "title")
	tags := stringSliceArg(args, "tags")
	entry := t.memoryStore.StoreUserProfile(content, title, tags)
	return Result{Success: true, Content: fmt.Sprintf("Stored user fact #%d in %s", entry.ID, t.memoryStore.UserPath())}
}

type SearchMemoryTool struct {
	memoryStore *store.Store
}

func NewSearchMemoryTool(memoryStore *store.Store) *SearchMemoryTool {
	return &SearchMemoryTool{memoryStore: memoryStore}
}

func (t *SearchMemoryTool) Spec() schema.ToolSpec {
	return Spec("search_memory", "Search durable memory stored in ~/.agent-go/.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Search query"},
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20, "default": 5},
			"kind": map[string]any{"type": "string", "enum": []string{"memory", "user", "all"}, "default": "all", "description": "Which memory bucket to search"},
		},
		"required": []string{"query"},
	})
}

func (t *SearchMemoryTool) Execute(ctx context.Context, args map[string]any) Result {
	if t.memoryStore == nil {
		return Result{Error: "memory store not available"}
	}
	query := stringArg(args, "query")
	limit := intArg(args, "limit")
	kind := strings.TrimSpace(stringArg(args, "kind"))
	var kinds []string
	switch kind {
	case "memory", "user":
		kinds = []string{kind}
	default:
		kinds = nil
	}
	results := t.memoryStore.Search(query, limit, kinds)
	if len(results) == 0 {
		return Result{Success: true, Content: "No memory matches found."}
	}
	lines := []string{"Memory search results:"}
	for index, entry := range results {
		lines = append(lines, fmt.Sprintf("%d. [%s] %s: %s", index+1, entry.Kind, entry.Title, entry.Content))
	}
	return Result{Success: true, Content: strings.Join(lines, "\n")}
}

func CreateMemoryTools(memoryStore *store.Store) ([]Tool, *store.Store) {
	if memoryStore == nil {
		return []Tool{}, nil
	}
	return []Tool{
		NewRememberTool(memoryStore),
		NewRememberUserTool(memoryStore),
		NewSearchMemoryTool(memoryStore),
	}, memoryStore
}

func stringSliceArg(args map[string]any, key string) []string {
	value, ok := args[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return append([]string{}, typed...)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			result = append(result, fmt.Sprint(item))
		}
		return result
	default:
		return []string{fmt.Sprint(typed)}
	}
}
