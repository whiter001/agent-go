package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whiter001/agent-go/internal/schema"
)

func TestMaybeCreateAutoSkillDraftWritesDraft(t *testing.T) {
	root := t.TempDir()
	messages := []schema.Message{
		{Role: schema.RoleSystem, Content: "system"},
		{Role: schema.RoleUser, Content: "执行autobrowser help"},
		{Role: schema.RoleAssistant, ToolCalls: []schema.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "bash",
				Arguments: map[string]any{"command": "autobrowser help"},
			},
		}}},
		{Role: schema.RoleTool, ToolCallID: "call-1", Name: "bash", Content: "Usage: autobrowser help"},
		{Role: schema.RoleAssistant, Content: "Done."},
	}

	path, created, err := MaybeCreateAutoSkillDraft("执行autobrowser help", messages, root, 1)
	if err != nil {
		t.Fatalf("MaybeCreateAutoSkillDraft() error = %v", err)
	}
	if !created {
		t.Fatalf("MaybeCreateAutoSkillDraft() created = false, want true")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"generated_signature:",
		"Auto bash workflow",
		"tools:",
		"- \"bash\"",
		"执行autobrowser help",
		"Usage: autobrowser help",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("draft content missing %q in %q", want, content)
		}
	}
	if filepath.Base(path) != "SKILL.md" {
		t.Fatalf("draft path = %q, want SKILL.md file", path)
	}
}

func TestMaybeCreateAutoSkillDraftSkipsFailedOrShortRuns(t *testing.T) {
	root := t.TempDir()
	failedMessages := []schema.Message{
		{Role: schema.RoleAssistant, ToolCalls: []schema.ToolCall{{
			ID:       "call-1",
			Type:     "function",
			Function: schema.FunctionCall{Name: "bash", Arguments: map[string]any{"command": "bad"}},
		}}},
		{Role: schema.RoleTool, ToolCallID: "call-1", Name: "bash", Content: "Error: command failed"},
	}
	if path, created, err := MaybeCreateAutoSkillDraft("bad", failedMessages, root, 1); err != nil || created || path != "" {
		t.Fatalf("failed run result = (%q, %t, %v), want empty/false/nil", path, created, err)
	}

	shortMessages := []schema.Message{
		{Role: schema.RoleAssistant, ToolCalls: []schema.ToolCall{{
			ID:       "call-1",
			Type:     "function",
			Function: schema.FunctionCall{Name: "bash", Arguments: map[string]any{"command": "ok"}},
		}}},
		{Role: schema.RoleTool, ToolCallID: "call-1", Name: "bash", Content: "ok"},
	}
	if path, created, err := MaybeCreateAutoSkillDraft("ok", shortMessages, root, 2); err != nil || created || path != "" {
		t.Fatalf("short run result = (%q, %t, %v), want empty/false/nil", path, created, err)
	}
}

func TestMaybeCreateAutoSkillDraftDeduplicatesBySignature(t *testing.T) {
	root := t.TempDir()
	messages := []schema.Message{
		{Role: schema.RoleAssistant, ToolCalls: []schema.ToolCall{{
			ID:       "call-1",
			Type:     "function",
			Function: schema.FunctionCall{Name: "bash", Arguments: map[string]any{"command": "autobrowser help"}},
		}}},
		{Role: schema.RoleTool, ToolCallID: "call-1", Name: "bash", Content: "Usage: autobrowser help"},
		{Role: schema.RoleAssistant, Content: "Done."},
	}

	firstPath, created, err := MaybeCreateAutoSkillDraft("执行autobrowser help", messages, root, 1)
	if err != nil || !created {
		t.Fatalf("first draft result = (%q, %t, %v), want created draft", firstPath, created, err)
	}
	secondPath, created, err := MaybeCreateAutoSkillDraft("执行autobrowser help", messages, root, 1)
	if err != nil {
		t.Fatalf("second MaybeCreateAutoSkillDraft() error = %v", err)
	}
	if created {
		t.Fatalf("second MaybeCreateAutoSkillDraft() created = true, want false")
	}
	if secondPath != firstPath {
		t.Fatalf("secondPath = %q, want %q", secondPath, firstPath)
	}
	count := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && d != nil && !d.IsDir() && strings.EqualFold(d.Name(), "SKILL.md") {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("draft count = %d, want 1", count)
	}
}
