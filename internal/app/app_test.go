package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/whiter001/agent-go/internal/config"
	"github.com/whiter001/agent-go/internal/llm"
	"github.com/whiter001/agent-go/internal/logging"
	"github.com/whiter001/agent-go/internal/schema"
	"github.com/whiter001/agent-go/internal/store"
)

func writeSkillFixture(t *testing.T, root, name, content string) string {
	t.Helper()

	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() = %v", err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	return path
}

func TestIsSkillListingPrompt(t *testing.T) {
	tests := []struct {
		prompt string
		want   bool
	}{
		{prompt: "列出当前所有skills", want: true},
		{prompt: "show all skills", want: true},
		{prompt: "/skills", want: true},
		{prompt: "how do tools work", want: false},
	}

	for _, tt := range tests {
		if got := isSkillListingPrompt(tt.prompt); got != tt.want {
			t.Fatalf("isSkillListingPrompt(%q) = %t, want %t", tt.prompt, got, tt.want)
		}
	}
}

func TestConfiguredSkillDirectoriesDeduplicates(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Tools.SkillsDir = root
	cfg.Tools.AutoSkillDir = root
	cfg.Tools.SkillsExternalDirs = []string{" ", root}

	directories := configuredSkillDirectories(cfg)
	if got, want := len(directories), 1; got != want {
		t.Fatalf("len(configuredSkillDirectories()) = %d, want %d", got, want)
	}
	if directories[0] != root {
		t.Fatalf("configuredSkillDirectories()[0] = %q, want %q", directories[0], root)
	}
}

func TestParseDotEnvLine(t *testing.T) {
	tests := []struct {
		line     string
		wantKey  string
		wantVal  string
		wantOkay bool
	}{
		{line: "AGENT_GO_API_KEY=secret", wantKey: "AGENT_GO_API_KEY", wantVal: "secret", wantOkay: true},
		{line: "export AGENT_GO_MODEL=MiniMax-M2.7", wantKey: "AGENT_GO_MODEL", wantVal: "MiniMax-M2.7", wantOkay: true},
		{line: "QUOTED=\"hello world\"", wantKey: "QUOTED", wantVal: "hello world", wantOkay: true},
		{line: "# comment", wantOkay: false},
	}

	for _, tt := range tests {
		key, value, ok := parseDotEnvLine(tt.line)
		if key != tt.wantKey || value != tt.wantVal || ok != tt.wantOkay {
			t.Fatalf("parseDotEnvLine(%q) = (%q, %q, %t), want (%q, %q, %t)", tt.line, key, value, ok, tt.wantKey, tt.wantVal, tt.wantOkay)
		}
	}
}

func TestLoadDotEnvFileSetsMissingValues(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".env")
	content := "AGENT_GO_API_KEY=dotenv-key\nAGENT_GO_MODEL=dotenv-model\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}

	t.Setenv("AGENT_GO_API_KEY", "")
	t.Setenv("AGENT_GO_MODEL", "already-set")
	if err := loadDotEnvFile(path); err != nil {
		t.Fatalf("loadDotEnvFile() error = %v", err)
	}

	if got := os.Getenv("AGENT_GO_API_KEY"); got != "dotenv-key" {
		t.Fatalf("AGENT_GO_API_KEY = %q, want %q", got, "dotenv-key")
	}
	if got := os.Getenv("AGENT_GO_MODEL"); got != "already-set" {
		t.Fatalf("AGENT_GO_MODEL = %q, want %q", got, "already-set")
	}
}

type stubClient struct {
	responses []schema.LLMResponse
	seenTools [][]schema.ToolSpec
	seenMsgs  [][]schema.Message
	callback  llm.RetryCallback
}

func (s *stubClient) Generate(_ context.Context, messages []schema.Message, tools []schema.ToolSpec) (schema.LLMResponse, error) {
	s.seenMsgs = append(s.seenMsgs, append([]schema.Message(nil), messages...))
	s.seenTools = append(s.seenTools, append([]schema.ToolSpec(nil), tools...))
	if len(s.responses) == 0 {
		return schema.LLMResponse{}, nil
	}
	response := s.responses[0]
	s.responses = s.responses[1:]
	return response, nil
}

func (s *stubClient) SetRetryCallback(callback llm.RetryCallback) {
	s.callback = callback
}

func TestRunPromptModeExecutesWithInjectedClient(t *testing.T) {
	originalClientFactory := promptClientFactory
	originalLoggerFactory := promptLoggerFactory
	originalMemoryFactory := promptMemoryStoreFactory
	t.Cleanup(func() {
		promptClientFactory = originalClientFactory
		promptLoggerFactory = originalLoggerFactory
		promptMemoryStoreFactory = originalMemoryFactory
	})

	stub := &stubClient{responses: []schema.LLMResponse{
		{
			ToolCalls: []schema.ToolCall{{
				ID:   "bash-1",
				Type: "function",
				Function: schema.FunctionCall{
					Name:      "bash",
					Arguments: map[string]any{"command": "echo autobrowser help", "timeout": 10},
				},
			}},
		},
		{Content: "Done."},
	}}

	promptClientFactory = func(apiKey, apiBase, model, provider string, retry config.RetryConfig) (llm.Client, error) {
		return stub, nil
	}
	promptLoggerFactory = func() (*logging.Logger, error) {
		return nil, nil
	}
	promptMemoryStoreFactory = func() (*store.Store, error) {
		return nil, nil
	}

	cfg := config.Default()
	cfg.LLM.APIKey = "test-key"
	cfg.Tools.EnableFileTools = false
	cfg.Tools.EnableBash = true
	cfg.Tools.EnableNote = false
	cfg.Tools.EnableMemory = false
	cfg.Tools.EnableSkills = false
	cfg.Agent.MaxSteps = 4

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runPromptMode(&stdout, &stderr, cfg, "执行autobrowser help 获取辅助, 然后用autobrowser打开百度"); err != nil {
		t.Fatalf("runPromptMode() error = %v", err)
	}

	output := stdout.String()
	for _, want := range []string{"Tool bash:", "autobrowser help", "Assistant:\nDone.", "Step 2 completed"} {
		if !strings.Contains(output, want) {
			t.Fatalf("runPromptMode() output missing %q in %q", want, output)
		}
	}
	if strings.Contains(output, "Prompt mode is not wired yet") {
		t.Fatalf("runPromptMode() output fell back to placeholder: %q", output)
	}
	if len(stub.seenTools) == 0 {
		t.Fatalf("stub client did not receive tool specs")
	}
	toolNames := make([]string, 0, len(stub.seenTools[0]))
	for _, spec := range stub.seenTools[0] {
		toolNames = append(toolNames, spec.Name)
	}
	if !reflect.DeepEqual(toolNames, []string{"bash", "bash_output", "bash_kill"}) {
		t.Fatalf("tool names = %#v, want %#v", toolNames, []string{"bash", "bash_output", "bash_kill"})
	}
}

func TestRunPromptModeListsAvailableSkills(t *testing.T) {
	root := t.TempDir()
	alphaPath := writeSkillFixture(t, root, "Alpha", "# Alpha\nAlpha workflow.\n")
	betaPath := writeSkillFixture(t, root, "Beta", "# Beta\nBeta workflow.\n")

	cfg := config.Default()
	cfg.Tools.EnableSkills = true
	cfg.Tools.SkillsDir = root
	cfg.Tools.AutoSkillDir = ""
	cfg.Tools.SkillsExternalDirs = nil

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runPromptMode(&stdout, &stderr, cfg, "列出当前所有skills"); err != nil {
		t.Fatalf("runPromptMode() error = %v", err)
	}

	output := stdout.String()
	for _, want := range []string{
		"Available skills (2):",
		"1. Alpha",
		"Description: Alpha workflow.",
		"Path: " + alphaPath,
		"2. Beta",
		"Description: Beta workflow.",
		"Path: " + betaPath,
		"Searched directories:",
		"- " + root,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("runPromptMode() output missing %q in %q", want, output)
		}
	}
	if strings.Contains(output, "Prompt mode is not wired yet") {
		t.Fatalf("runPromptMode() fell back to bootstrap placeholder: %q", output)
	}
}

func TestRunPromptModeReportsNoSkillsFound(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Tools.EnableSkills = true
	cfg.Tools.SkillsDir = root
	cfg.Tools.AutoSkillDir = ""
	cfg.Tools.SkillsExternalDirs = nil

	var stdout bytes.Buffer
	if err := runPromptMode(&stdout, &bytes.Buffer{}, cfg, "list current skills"); err != nil {
		t.Fatalf("runPromptMode() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "No skills found.") {
		t.Fatalf("runPromptMode() output = %q, want no-skills message", output)
	}
	if !strings.Contains(output, "- "+root) {
		t.Fatalf("runPromptMode() output = %q, want searched directory %q", output, root)
	}
}

func TestRunPromptModeCreatesAutoSkillDraft(t *testing.T) {
	originalClientFactory := promptClientFactory
	originalLoggerFactory := promptLoggerFactory
	originalMemoryFactory := promptMemoryStoreFactory
	t.Cleanup(func() {
		promptClientFactory = originalClientFactory
		promptLoggerFactory = originalLoggerFactory
		promptMemoryStoreFactory = originalMemoryFactory
	})

	stub := &stubClient{responses: []schema.LLMResponse{
		{
			ToolCalls: []schema.ToolCall{{
				ID:   "bash-1",
				Type: "function",
				Function: schema.FunctionCall{
					Name:      "bash",
					Arguments: map[string]any{"command": "echo autobrowser help", "timeout": 10},
				},
			}},
		},
		{Content: "Done."},
	}}

	promptClientFactory = func(apiKey, apiBase, model, provider string, retry config.RetryConfig) (llm.Client, error) {
		return stub, nil
	}
	promptLoggerFactory = func() (*logging.Logger, error) {
		return nil, nil
	}
	promptMemoryStoreFactory = func() (*store.Store, error) {
		return nil, nil
	}

	autoDir := t.TempDir()
	cfg := config.Default()
	cfg.LLM.APIKey = "test-key"
	cfg.Tools.EnableFileTools = false
	cfg.Tools.EnableBash = true
	cfg.Tools.EnableNote = false
	cfg.Tools.EnableMemory = false
	cfg.Tools.EnableSkills = false
	cfg.Tools.EnableAutoSkillCreation = true
	cfg.Tools.AutoSkillDir = autoDir
	cfg.Tools.AutoSkillMinToolCalls = 1
	cfg.Agent.MaxSteps = 4

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runPromptMode(&stdout, &stderr, cfg, "执行autobrowser help 获取辅助"); err != nil {
		t.Fatalf("runPromptMode() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Auto-skill draft saved:") {
		t.Fatalf("runPromptMode() output missing auto-skill message in %q", output)
	}
	entries, err := os.ReadDir(autoDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected auto-skill draft in %q", autoDir)
	}
}
