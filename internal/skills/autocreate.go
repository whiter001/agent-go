package skills

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/whiter001/agent-go/internal/config"
	"github.com/whiter001/agent-go/internal/schema"
	"github.com/whiter001/agent-go/internal/utils"
)

type toolExecution struct {
	ID        string
	Name      string
	Arguments map[string]any
	Result    string
	Success   bool
}

func MaybeCreateAutoSkillDraft(prompt string, messages []schema.Message, autoSkillDir string, minToolCalls int) (string, bool, error) {
	resolvedDir := config.ExpandPath(autoSkillDir)
	if strings.TrimSpace(resolvedDir) == "" {
		return "", false, nil
	}
	if minToolCalls <= 0 {
		minToolCalls = 5
	}

	executions, finalAssistant, ok := extractSuccessfulExecutions(messages)
	if !ok || len(executions) < minToolCalls {
		return "", false, nil
	}

	signature := autoSkillSignature(prompt, executions)
	if signature == "" {
		return "", false, nil
	}
	existing, err := findDraftBySignature(resolvedDir, signature)
	if err != nil {
		return "", false, err
	}
	if existing != "" {
		return existing, false, nil
	}

	name := buildAutoSkillName(executions)
	draft := renderAutoSkillDraft(name, prompt, signature, finalAssistant, executions)
	directory := filepath.Join(resolvedDir, fmt.Sprintf("%s-%s", time.Now().Format("20060102-150405"), buildAutoSkillSlug(prompt, executions, signature)))
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", false, err
	}
	path := filepath.Join(directory, "SKILL.md")
	if err := os.WriteFile(path, []byte(draft), 0o644); err != nil {
		return "", false, err
	}
	return path, true, nil
}

func extractSuccessfulExecutions(messages []schema.Message) ([]toolExecution, string, bool) {
	executions := make([]toolExecution, 0, 4)
	indexes := map[string]int{}
	finalAssistant := ""
	for _, message := range messages {
		switch message.Role {
		case schema.RoleAssistant:
			if strings.TrimSpace(message.Content) != "" {
				finalAssistant = strings.TrimSpace(message.Content)
			}
			for _, call := range message.ToolCalls {
				executions = append(executions, toolExecution{
					ID:        call.ID,
					Name:      call.Function.Name,
					Arguments: call.Function.Arguments,
				})
				indexes[call.ID] = len(executions) - 1
			}
		case schema.RoleTool:
			index, ok := indexes[message.ToolCallID]
			if !ok {
				continue
			}
			executions[index].Result = strings.TrimSpace(message.Content)
			executions[index].Success = !strings.HasPrefix(strings.TrimSpace(message.Content), "Error:")
		}
	}
	if len(executions) == 0 {
		return nil, "", false
	}
	for _, execution := range executions {
		if !execution.Success {
			return nil, "", false
		}
	}
	return executions, finalAssistant, true
}

func autoSkillSignature(prompt string, executions []toolExecution) string {
	parts := []string{compactText(prompt)}
	for _, execution := range executions {
		arguments, _ := json.Marshal(execution.Arguments)
		parts = append(parts, strings.ToLower(execution.Name), string(arguments))
	}
	hash := sha1.Sum([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(hash[:])[:12]
}

func findDraftBySignature(root string, signature string) (string, error) {
	if signature == "" {
		return "", nil
	}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d == nil || d.IsDir() || strings.ToUpper(d.Name()) != "SKILL.MD" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if strings.Contains(string(data), "generated_signature: "+signature) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return "", err
	}
	return found, nil
}

func buildAutoSkillName(executions []toolExecution) string {
	names := uniqueToolNames(executions)
	if len(names) == 0 {
		return "Auto-generated workflow"
	}
	if len(names) > 2 {
		names = names[:2]
	}
	return fmt.Sprintf("Auto %s workflow", strings.Join(names, " + "))
}

func buildAutoSkillSlug(prompt string, executions []toolExecution, signature string) string {
	parts := make([]string, 0, 4)
	for _, toolName := range uniqueToolNames(executions) {
		if slug := slugPart(toolName); slug != "" {
			parts = append(parts, slug)
		}
		if len(parts) >= 2 {
			break
		}
	}
	for _, token := range tokenize(prompt) {
		if slug := slugPart(token); slug != "" {
			parts = append(parts, slug)
		}
		if len(parts) >= 4 {
			break
		}
	}
	parts = uniqueOrderedStrings(parts)
	if len(parts) == 0 {
		parts = []string{"autoskill"}
	}
	slug := strings.Join(parts, "-")
	if len(slug) > 48 {
		slug = strings.Trim(slug[:48], "-")
	}
	return fmt.Sprintf("%s-%s", slug, signature[:6])
}

func slugPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func renderAutoSkillDraft(name string, prompt string, signature string, finalAssistant string, executions []toolExecution) string {
	toolNames := uniqueToolNames(executions)
	description := fmt.Sprintf("Auto-generated draft based on a successful workflow using %s.", strings.Join(toolNames, ", "))
	if len(toolNames) == 0 {
		description = "Auto-generated draft based on a successful workflow."
	}
	if strings.TrimSpace(finalAssistant) == "" {
		finalAssistant = "Successful completion was observed during the original run."
	}

	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("name: ")
	builder.WriteString(yamlQuote(name))
	builder.WriteString("\n")
	builder.WriteString("description: ")
	builder.WriteString(yamlQuote(description))
	builder.WriteString("\n")
	builder.WriteString("tags:\n")
	builder.WriteString("  - auto-generated\n")
	builder.WriteString("  - draft\n")
	if len(toolNames) > 0 {
		builder.WriteString("tools:\n")
		for _, toolName := range toolNames {
			builder.WriteString("  - ")
			builder.WriteString(yamlQuote(toolName))
			builder.WriteString("\n")
		}
	}
	builder.WriteString("triggers:\n")
	builder.WriteString("  - ")
	builder.WriteString(yamlQuote(strings.TrimSpace(prompt)))
	builder.WriteString("\n")
	builder.WriteString("draft: true\n")
	builder.WriteString("generated_signature: ")
	builder.WriteString(signature)
	builder.WriteString("\n")
	builder.WriteString("generated_at: ")
	builder.WriteString(yamlQuote(time.Now().UTC().Format(time.RFC3339)))
	builder.WriteString("\n")
	builder.WriteString("---\n\n")
	builder.WriteString("# Overview\n\n")
	builder.WriteString("This draft autoskill was created from a successful execution trace. Review and refine it before relying on it broadly.\n\n")
	builder.WriteString("## When to use\n\n")
	builder.WriteString("- Requests similar to: `")
	builder.WriteString(strings.TrimSpace(prompt))
	builder.WriteString("`\n")
	if len(toolNames) > 0 {
		builder.WriteString("- When the workflow is expected to use: `")
		builder.WriteString(strings.Join(toolNames, "`, `"))
		builder.WriteString("`\n")
	}
	builder.WriteString("\n## Steps\n\n")
	for index, execution := range executions {
		builder.WriteString(fmt.Sprintf("%d. Use `%s`", index+1, execution.Name))
		if arguments := executionArgumentsPreview(execution.Arguments); arguments != "" {
			builder.WriteString(" with arguments `")
			builder.WriteString(arguments)
			builder.WriteString("`")
		}
		builder.WriteString(".\n")
		if preview := executionResultPreview(execution.Result); preview != "" {
			builder.WriteString("   - Expected result: ")
			builder.WriteString(preview)
			builder.WriteString("\n")
		}
	}
	builder.WriteString("\n## Validation\n\n")
	builder.WriteString("- Successful runs should end with an outcome similar to: ")
	builder.WriteString(utils.TruncateMiddle(strings.TrimSpace(finalAssistant), 240))
	builder.WriteString("\n")
	builder.WriteString("- Confirm that each tool step completes without an error result.\n")
	builder.WriteString("\n## Example request\n\n")
	builder.WriteString(strings.TrimSpace(prompt))
	builder.WriteString("\n\n## Tool trace\n\n")
	for index, execution := range executions {
		builder.WriteString(fmt.Sprintf("%d. `%s`\n", index+1, execution.Name))
		if arguments := executionArgumentsPreview(execution.Arguments); arguments != "" {
			builder.WriteString("   - Arguments: `")
			builder.WriteString(arguments)
			builder.WriteString("`\n")
		}
		if preview := executionResultPreview(execution.Result); preview != "" {
			builder.WriteString("   - Result preview: ")
			builder.WriteString(preview)
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func uniqueToolNames(executions []toolExecution) []string {
	names := make([]string, 0, len(executions))
	for _, execution := range executions {
		names = append(names, execution.Name)
	}
	return uniqueOrderedStrings(names)
}

func executionArgumentsPreview(arguments map[string]any) string {
	if len(arguments) == 0 {
		return ""
	}
	data, err := json.Marshal(arguments)
	if err != nil {
		return ""
	}
	return utils.TruncateMiddle(string(data), 200)
}

func executionResultPreview(result string) string {
	trimmed := strings.TrimSpace(result)
	if trimmed == "" {
		return ""
	}
	return utils.TruncateMiddle(trimmed, 240)
}

func yamlQuote(value string) string {
	escaped := strings.ReplaceAll(strings.TrimSpace(value), "\"", "\\\"")
	return `"` + escaped + `"`
}
