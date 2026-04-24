package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/whiter001/agent-go/internal/schema"
)

type SessionNoteTool struct {
	memoryFile string
}

func NewSessionNoteTool(memoryFile string) *SessionNoteTool {
	return &SessionNoteTool{memoryFile: memoryFile}
}

func (t *SessionNoteTool) Spec() schema.ToolSpec {
	return Spec("record_note", "Record important information as session notes for future reference.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content":  map[string]any{"type": "string", "description": "The information to record as a note"},
			"category": map[string]any{"type": "string", "description": "Optional category/tag for this note"},
		},
		"required": []string{"content"},
	})
}

func (t *SessionNoteTool) Execute(ctx context.Context, args map[string]any) Result {
	content := stringArg(args, "content")
	category := stringArg(args, "category")
	if content == "" {
		return Result{Error: "content is required"}
	}
	if category == "" {
		category = "general"
	}
	notes, err := loadNotes(t.memoryFile)
	if err != nil {
		return Result{Error: err.Error()}
	}
	notes = append(notes, noteRecord{Timestamp: time.Now().UTC().Format(time.RFC3339), Category: category, Content: content})
	if err := saveNotes(t.memoryFile, notes); err != nil {
		return Result{Error: err.Error()}
	}
	return Result{Success: true, Content: fmt.Sprintf("Recorded note: %s (category: %s)", content, category)}
}

type RecallNoteTool struct {
	memoryFile string
}

func NewRecallNoteTool(memoryFile string) *RecallNoteTool {
	return &RecallNoteTool{memoryFile: memoryFile}
}

func (t *RecallNoteTool) Spec() schema.ToolSpec {
	return Spec("recall_notes", "Recall all previously recorded session notes.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"category": map[string]any{"type": "string", "description": "Optional category filter"},
		},
	})
}

func (t *RecallNoteTool) Execute(ctx context.Context, args map[string]any) Result {
	category := strings.TrimSpace(stringArg(args, "category"))
	notes, err := loadNotes(t.memoryFile)
	if err != nil {
		return Result{Error: err.Error()}
	}
	if len(notes) == 0 {
		return Result{Success: true, Content: "No notes recorded yet."}
	}
	formatted := make([]string, 0, len(notes))
	for index, note := range notes {
		if category != "" && note.Category != category {
			continue
		}
		formatted = append(formatted, fmt.Sprintf("%d. [%s] %s\n   (recorded at %s)", index+1, note.Category, note.Content, note.Timestamp))
	}
	if len(formatted) == 0 {
		if category == "" {
			return Result{Success: true, Content: "No notes recorded yet."}
		}
		return Result{Success: true, Content: fmt.Sprintf("No notes found in category: %s", category)}
	}
	return Result{Success: true, Content: "Recorded Notes:\n" + strings.Join(formatted, "\n")}
}

type noteRecord struct {
	Timestamp string `json:"timestamp"`
	Category  string `json:"category"`
	Content   string `json:"content"`
}

func loadNotes(memoryFile string) ([]noteRecord, error) {
	if strings.TrimSpace(memoryFile) == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Clean(memoryFile))
	if err != nil {
		if os.IsNotExist(err) {
			return []noteRecord{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return []noteRecord{}, nil
	}
	var notes []noteRecord
	if err := json.Unmarshal(data, &notes); err != nil {
		return nil, err
	}
	return notes, nil
}

func saveNotes(memoryFile string, notes []noteRecord) error {
	path := filepath.Clean(memoryFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(notes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
