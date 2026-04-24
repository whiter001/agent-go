package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whiter001/agent-go/internal/schema"
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

func TestDiscoverMetadataPromptAndLoadedOrder(t *testing.T) {
	root := t.TempDir()
	writeSkillFixture(t, root, "zeta", "# Zeta\nZeta line one.\n\nZeta line two.\n")
	writeSkillFixture(t, root, "Alpha", "# Alpha\nAlpha line one.\nAlpha line two.\n\nMore text.\n")
	writeSkillFixture(t, root, "beta", "# Beta\nBeta only paragraph.\n")

	loader := NewLoader(root, root)
	if err := loader.Discover(); err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	loaded := loader.Loaded()
	if got, want := len(loaded), 3; got != want {
		t.Fatalf("Loaded() len = %d, want %d", got, want)
	}

	if loaded[0].Name != "Alpha" || loaded[1].Name != "beta" || loaded[2].Name != "zeta" {
		t.Fatalf("Loaded() order = [%s, %s, %s], want [Alpha, beta, zeta]", loaded[0].Name, loaded[1].Name, loaded[2].Name)
	}

	if loaded[0].Description != "Alpha line one. Alpha line two." {
		t.Fatalf("Alpha description = %q, want %q", loaded[0].Description, "Alpha line one. Alpha line two.")
	}
	if loaded[1].Description != "Beta only paragraph." {
		t.Fatalf("Beta description = %q, want %q", loaded[1].Description, "Beta only paragraph.")
	}
	if loaded[2].Description != "Zeta line one." {
		t.Fatalf("Zeta description = %q, want %q", loaded[2].Description, "Zeta line one.")
	}

	prompt := loader.MetadataPrompt()
	if !strings.Contains(prompt, "## Available Skills") {
		t.Fatalf("MetadataPrompt() missing header: %q", prompt)
	}
	for _, want := range []string{
		"- Alpha: Alpha line one. Alpha line two.",
		"- beta: Beta only paragraph.",
		"- zeta: Zeta line one.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("MetadataPrompt() missing %q in %q", want, prompt)
		}
	}
}

func TestSelectAndBuildTurnContext(t *testing.T) {
	root := t.TempDir()
	writeSkillFixture(t, root, "zeta", "# Zeta\nDeploy notes.\n")
	writeSkillFixture(t, root, "Alpha", "# Alpha\nAlpha deploy helper.\nMore alpha deploy guidance.\n")
	writeSkillFixture(t, root, "beta", "# Beta\nUnrelated content.\n")

	loader := NewLoader(root)
	if err := loader.Discover(); err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	selected := loader.Select("alpha deploy", 1)
	if got, want := len(selected), 1; got != want {
		t.Fatalf("Select() len = %d, want %d", got, want)
	}
	if selected[0].Name != "Alpha" {
		t.Fatalf("Select() first skill = %q, want %q", selected[0].Name, "Alpha")
	}

	defaultLimited := loader.Select("", 0)
	if got, want := len(defaultLimited), 2; got != want {
		t.Fatalf("Select() with zero limit len = %d, want %d", got, want)
	}
	if defaultLimited[0].Name != "Alpha" || defaultLimited[1].Name != "beta" {
		t.Fatalf("Select() default order = [%s, %s], want [Alpha, beta]", defaultLimited[0].Name, defaultLimited[1].Name)
	}

	ctx := loader.BuildTurnContext("alpha deploy", 1)
	if got, want := len(ctx), 1; got != want {
		t.Fatalf("BuildTurnContext() len = %d, want %d", got, want)
	}
	if ctx[0].Role != schema.RoleSystem {
		t.Fatalf("BuildTurnContext() role = %q, want %q", ctx[0].Role, schema.RoleSystem)
	}
	if !strings.Contains(ctx[0].Content, "## Relevant Skills") {
		t.Fatalf("BuildTurnContext() missing header: %q", ctx[0].Content)
	}
	if !strings.Contains(ctx[0].Content, "1. Alpha: Alpha deploy helper. More alpha deploy guidance.") {
		t.Fatalf("BuildTurnContext() missing selected skill: %q", ctx[0].Content)
	}
}

func TestFirstParagraphSkipsFrontmatter(t *testing.T) {
	content := `---
name: autobrowser
description: "Use when driving the autobrowser CLI"
---

# autobrowser Skill

Use this skill when you need to control Chrome with autobrowser.

## More

Extra text.`

	if got, want := firstParagraph(content), "Use this skill when you need to control Chrome with autobrowser."; got != want {
		t.Fatalf("firstParagraph() = %q, want %q", got, want)
	}
}