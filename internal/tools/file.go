package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/whiter001/agent-go/internal/config"
	"github.com/whiter001/agent-go/internal/schema"
	"github.com/whiter001/agent-go/internal/utils"
)

type ReadTool struct {
	workspaceDir string
}

func NewReadTool(workspaceDir string) *ReadTool {
	return &ReadTool{workspaceDir: workspaceDir}
}

func (t *ReadTool) Spec() schema.ToolSpec {
	return Spec("read_file", "Read file contents from the filesystem. Output always includes line numbers in format 'LINE_NUMBER|LINE_CONTENT' (1-indexed). Supports reading partial content by specifying line offset and limit for large files.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
			"offset": map[string]any{"type": "integer", "description": "Starting line number (1-indexed)"},
			"limit": map[string]any{"type": "integer", "description": "Number of lines to read"},
		},
		"required": []string{"path"},
	})
}

func (t *ReadTool) Execute(ctx context.Context, args map[string]any) Result {
	path := stringArg(args, "path")
	if path == "" {
		return Result{Error: "path is required"}
	}
	filePath, err := t.resolvePath(path)
	if err != nil {
		return Result{Error: err.Error()}
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return Result{Error: err.Error()}
	}
	text := string(data)
	lines := strings.Split(text, "\n")
	offset := intArg(args, "offset")
	limit := intArg(args, "limit")
	start := 0
	if offset > 0 {
		start = offset - 1
	}
	if start < 0 {
		start = 0
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	if start > len(lines) {
		return Result{Success: true, Content: ""}
	}
	if end < start {
		end = start
	}
	selected := lines[start:end]
	formatted := make([]string, 0, len(selected))
	for index, line := range selected {
		formatted = append(formatted, fmt.Sprintf("%6d|%s", start+index+1, line))
	}
	content := strings.Join(formatted, "\n")
	content = utils.TruncateMiddle(content, 32000)
	return Result{Success: true, Content: content}
}

func (t *ReadTool) resolvePath(path string) (string, error) {
	resolved := config.ExpandPath(path)
	if filepath.IsAbs(resolved) {
		return resolved, nil
	}
	workspace := config.ExpandPath(t.workspaceDir)
	if workspace == "" {
		return filepath.Abs(resolved)
	}
	return filepath.Join(workspace, resolved), nil
}

type WriteTool struct {
	workspaceDir string
}

func NewWriteTool(workspaceDir string) *WriteTool {
	return &WriteTool{workspaceDir: workspaceDir}
}

func (t *WriteTool) Spec() schema.ToolSpec {
	return Spec("write_file", "Write content to a file. Will overwrite existing files completely.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
			"content": map[string]any{"type": "string", "description": "Complete content to write"},
		},
		"required": []string{"path", "content"},
	})
}

func (t *WriteTool) Execute(ctx context.Context, args map[string]any) Result {
	path := stringArg(args, "path")
	content := stringArg(args, "content")
	if path == "" {
		return Result{Error: "path is required"}
	}
	filePath, err := t.resolvePath(path)
	if err != nil {
		return Result{Error: err.Error()}
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return Result{Error: err.Error()}
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return Result{Error: err.Error()}
	}
	return Result{Success: true, Content: fmt.Sprintf("Successfully wrote to %s", filePath)}
}

func (t *WriteTool) resolvePath(path string) (string, error) {
	resolved := config.ExpandPath(path)
	if filepath.IsAbs(resolved) {
		return resolved, nil
	}
	workspace := config.ExpandPath(t.workspaceDir)
	if workspace == "" {
		return filepath.Abs(resolved)
	}
	return filepath.Join(workspace, resolved), nil
}

type EditTool struct {
	workspaceDir string
}

func NewEditTool(workspaceDir string) *EditTool {
	return &EditTool{workspaceDir: workspaceDir}
}

func (t *EditTool) Spec() schema.ToolSpec {
	return Spec("edit_file", "Perform exact string replacement in a file. The old_str must match exactly and appear uniquely in the file.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
			"old_str": map[string]any{"type": "string", "description": "Exact string to find and replace (must be unique in file)"},
			"new_str": map[string]any{"type": "string", "description": "Replacement string"},
		},
		"required": []string{"path", "old_str", "new_str"},
	})
}

func (t *EditTool) Execute(ctx context.Context, args map[string]any) Result {
	path := stringArg(args, "path")
	oldStr := stringArg(args, "old_str")
	newStr := stringArg(args, "new_str")
	if path == "" {
		return Result{Error: "path is required"}
	}
	if oldStr == "" {
		return Result{Error: "old_str is required"}
	}
	filePath, err := t.resolvePath(path)
	if err != nil {
		return Result{Error: err.Error()}
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return Result{Error: err.Error()}
	}
	content := string(data)
	if strings.Count(content, oldStr) != 1 {
		return Result{Error: fmt.Sprintf("old_str must appear exactly once in file: found %d occurrences", strings.Count(content, oldStr))}
	}
	updated := strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(filePath, []byte(updated), 0o644); err != nil {
		return Result{Error: err.Error()}
	}
	return Result{Success: true, Content: fmt.Sprintf("Successfully edited %s", filePath)}
}

func (t *EditTool) resolvePath(path string) (string, error) {
	resolved := config.ExpandPath(path)
	if filepath.IsAbs(resolved) {
		return resolved, nil
	}
	workspace := config.ExpandPath(t.workspaceDir)
	if workspace == "" {
		return filepath.Abs(resolved)
	}
	return filepath.Join(workspace, resolved), nil
}

func stringArg(args map[string]any, key string) string {
	value, ok := args[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func intArg(args map[string]any, key string) int {
	value, ok := args[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	default:
		return 0
	}
}
