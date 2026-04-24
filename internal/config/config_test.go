package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesEnvOverridesAndNormalizes(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	configJSON := `{
		"llm": {
			"api_key": "file-key",
			"api_base": "https://file.example/anthropic/",
			"model": "file-model",
			"provider": "anthropic"
		},
		"agent": {
			"max_steps": 11,
			"workspace_dir": "./workspace-from-file",
			"system_prompt_path": "./prompt-from-file.md"
		},
		"tools": {
			"auto_skills_limit": 9,
			"enable_mcp": true,
			"auto_skill_dir": "./auto-from-file",
			"skills_dir": "./skills-from-file",
			"skills_external_dirs": [" ./external-one ", "~/external-two"]
		}
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o644); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}

	t.Setenv("AGENT_GO_CONFIG", "  "+configPath+"  ")
	t.Setenv("AGENT_GO_API_KEY", "env-key")
	t.Setenv("MINI_AGENT_API_KEY", "fallback-key")
	t.Setenv("AGENT_GO_API_BASE", "https://env.example/api/")
	t.Setenv("AGENT_GO_MODEL", "env-model")
	t.Setenv("AGENT_GO_PROVIDER", " OpenAI ")
	t.Setenv("AGENT_GO_MAX_STEPS", "77")
	t.Setenv("AGENT_GO_WORKSPACE_DIR", " ./workspace-from-env ")
	t.Setenv("AGENT_GO_SYSTEM_PROMPT_PATH", " ./prompts/system.md ")
	t.Setenv("AGENT_GO_ENABLE_AUTO_SKILLS", "false")
	t.Setenv("AGENT_GO_AUTO_SKILLS_LIMIT", "4")
	t.Setenv("AGENT_GO_ENABLE_AUTO_SKILL_CREATION", "true")
	t.Setenv("AGENT_GO_AUTO_SKILL_MIN_TOOL_CALLS", "3")
	t.Setenv("AGENT_GO_SKILLS_DIR", " ./skills-from-env ")
	t.Setenv("AGENT_GO_AUTO_SKILL_DIR", "~/auto-skills")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}

	if cfg.LLM.APIKey != "env-key" {
		t.Fatalf("LLM.APIKey = %q, want %q", cfg.LLM.APIKey, "env-key")
	}
	if cfg.LLM.APIBase != "https://env.example/api" {
		t.Fatalf("LLM.APIBase = %q, want %q", cfg.LLM.APIBase, "https://env.example/api")
	}
	if cfg.LLM.Model != "env-model" {
		t.Fatalf("LLM.Model = %q, want %q", cfg.LLM.Model, "env-model")
	}
	if cfg.LLM.Provider != "openai" {
		t.Fatalf("LLM.Provider = %q, want %q", cfg.LLM.Provider, "openai")
	}
	if cfg.Agent.MaxSteps != 77 {
		t.Fatalf("Agent.MaxSteps = %d, want %d", cfg.Agent.MaxSteps, 77)
	}
	if cfg.Agent.WorkspaceDir != filepath.Clean("./workspace-from-env") {
		t.Fatalf("Agent.WorkspaceDir = %q, want %q", cfg.Agent.WorkspaceDir, filepath.Clean("./workspace-from-env"))
	}
	if cfg.Agent.SystemPromptPath != filepath.Clean("./prompts/system.md") {
		t.Fatalf("Agent.SystemPromptPath = %q, want %q", cfg.Agent.SystemPromptPath, filepath.Clean("./prompts/system.md"))
	}
	if cfg.Tools.AutoSkillDir != filepath.Join(home, "auto-skills") {
		t.Fatalf("Tools.AutoSkillDir = %q, want %q", cfg.Tools.AutoSkillDir, filepath.Join(home, "auto-skills"))
	}
	if cfg.Tools.SkillsDir != filepath.Clean("./skills-from-env") {
		t.Fatalf("Tools.SkillsDir = %q, want %q", cfg.Tools.SkillsDir, filepath.Clean("./skills-from-env"))
	}
	if cfg.Tools.EnableAutoSkills {
		t.Fatalf("Tools.EnableAutoSkills = true, want false")
	}
	if got, want := cfg.Tools.AutoSkillsLimit, 4; got != want {
		t.Fatalf("Tools.AutoSkillsLimit = %d, want %d", got, want)
	}
	if !cfg.Tools.EnableAutoSkillCreation {
		t.Fatalf("Tools.EnableAutoSkillCreation = false, want true")
	}
	if got, want := cfg.Tools.AutoSkillMinToolCalls, 3; got != want {
		t.Fatalf("Tools.AutoSkillMinToolCalls = %d, want %d", got, want)
	}
	if !cfg.Tools.EnableMCP {
		t.Fatalf("Tools.EnableMCP = false, want true")
	}
	if got, want := len(cfg.Tools.SkillsExternalDirs), 2; got != want {
		t.Fatalf("len(Tools.SkillsExternalDirs) = %d, want %d", got, want)
	}
	if cfg.Tools.SkillsExternalDirs[0] != filepath.Clean("./external-one") {
		t.Fatalf("Tools.SkillsExternalDirs[0] = %q, want %q", cfg.Tools.SkillsExternalDirs[0], filepath.Clean("./external-one"))
	}
	if cfg.Tools.SkillsExternalDirs[1] != filepath.Join(home, "external-two") {
		t.Fatalf("Tools.SkillsExternalDirs[1] = %q, want %q", cfg.Tools.SkillsExternalDirs[1], filepath.Join(home, "external-two"))
	}
}
