package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whiter001/agent-go/internal/schema"
)

func TestRecordSkillSelectionFeedbackWritesStats(t *testing.T) {
	feedbackPath := filepath.Join(t.TempDir(), "skill-feedback.json")
	selected := []Skill{{
		Name:  "Bash Helper",
		Path:  filepath.Join("skills", "bash-helper", "SKILL.md"),
		Tools: []string{"bash"},
	}}
	messages := []schema.Message{
		{Role: schema.RoleAssistant, ToolCalls: []schema.ToolCall{{
			ID:       "call-1",
			Type:     "function",
			Function: schema.FunctionCall{Name: "bash", Arguments: map[string]any{"command": "echo ok"}},
		}}},
		{Role: schema.RoleTool, ToolCallID: "call-1", Name: "bash", Content: "ok"},
		{Role: schema.RoleAssistant, Content: "Done."},
	}

	if err := RecordSkillSelectionFeedback(feedbackPath, "run bash helper", selected, messages); err != nil {
		t.Fatalf("RecordSkillSelectionFeedback() error = %v", err)
	}
	store, err := loadSkillFeedbackStore(feedbackPath)
	if err != nil {
		t.Fatalf("loadSkillFeedbackStore() error = %v", err)
	}
	stats := store.Skills[selected[0].Path]
	if stats.Selections != 1 || stats.SuccessfulRuns != 1 || stats.HelpfulRuns != 1 {
		t.Fatalf("stats = %#v, want one selection/success/helpful", stats)
	}
	if len(store.Events) != 1 {
		t.Fatalf("len(store.Events) = %d, want 1", len(store.Events))
	}
	if got := strings.Join(store.Events[0].UsedTools, ","); got != "bash" {
		t.Fatalf("store.Events[0].UsedTools = %q, want %q", got, "bash")
	}
}

func TestLoaderSelectUsesFeedbackRanking(t *testing.T) {
	root := t.TempDir()
	alphaPath := writeSkillFixture(t, root, "Alpha", "# Alpha\nGeneric workflow.\n")
	betaPath := writeSkillFixture(t, root, "Beta", "# Beta\nGeneric workflow.\n")
	feedbackPath := filepath.Join(t.TempDir(), "skill-feedback.json")
	store := skillFeedbackStore{
		Skills: map[string]SkillFeedbackStats{
			betaPath: {
				Selections:     3,
				SuccessfulRuns: 2,
				HelpfulRuns:    2,
				LastSelectedAt: time.Now().UTC().Format(time.RFC3339),
				LastSuccessAt:  time.Now().UTC().Format(time.RFC3339),
				LastHelpfulAt:  time.Now().UTC().Format(time.RFC3339),
			},
		},
	}
	if err := saveSkillFeedbackStore(feedbackPath, store); err != nil {
		t.Fatalf("saveSkillFeedbackStore() error = %v", err)
	}

	loader := NewLoader(root).SetFeedbackStorePath(feedbackPath)
	if err := loader.Discover(); err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	selected := loader.Select("", 1)
	if got, want := len(selected), 1; got != want {
		t.Fatalf("Select() len = %d, want %d", got, want)
	}
	if selected[0].Path != betaPath {
		t.Fatalf("Select() first path = %q, want %q (alpha path was %q)", selected[0].Path, betaPath, alphaPath)
	}
	if selected[0].Feedback.HelpfulRuns != 2 {
		t.Fatalf("selected[0].Feedback = %#v, want helpful runs", selected[0].Feedback)
	}
	if _, err := os.Stat(feedbackPath); err != nil {
		t.Fatalf("Stat(feedbackPath) error = %v", err)
	}
}
