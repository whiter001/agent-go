package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/whiter001/agent-go/internal/config"
	"github.com/whiter001/agent-go/internal/schema"
	"github.com/whiter001/agent-go/internal/utils"
)

type Skill struct {
	Name        string
	Description string
	Path        string
	Content     string
}

type Loader struct {
	directories []string
	mu          sync.Mutex
	skills      []Skill
}

func NewLoader(directories ...string) *Loader {
	return &Loader{directories: append([]string{}, directories...)}
}

func (l *Loader) Discover() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var skills []Skill
	seen := map[string]struct{}{}
	for _, directory := range l.directories {
		resolved := config.ExpandPath(directory)
		if resolved == "" {
			continue
		}
		_ = filepath.WalkDir(resolved, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d == nil || d.IsDir() || strings.ToUpper(d.Name()) != "SKILL.MD" {
				return nil
			}
			if _, ok := seen[path]; ok {
				return nil
			}
			seen[path] = struct{}{}
			skill, err := readSkill(path)
			if err != nil {
				return nil
			}
			skills = append(skills, skill)
			return nil
		})
	}
	sort.Slice(skills, func(i, j int) bool {
		return strings.ToLower(skills[i].Name) < strings.ToLower(skills[j].Name)
	})
	l.skills = skills
	return nil
}

func (l *Loader) Loaded() []Skill {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Skill(nil), l.skills...)
}

func (l *Loader) MetadataPrompt() string {
	skills := l.Loaded()
	if len(skills) == 0 {
		return ""
	}
	lines := []string{"## Available Skills", "The following reusable workflows are available:"}
	for _, skill := range skills {
		lines = append(lines, fmt.Sprintf("- %s: %s", skill.Name, skill.Description))
	}
	return strings.Join(lines, "\n")
}

func (l *Loader) BuildTurnContext(query string, maxSkills int) []schema.Message {
	selected := l.Select(query, maxSkills)
	if len(selected) == 0 {
		return nil
	}
	lines := []string{"## Relevant Skills", "Use these skill notes when they are helpful for the current request."}
	for index, skill := range selected {
		lines = append(lines, fmt.Sprintf("%d. %s: %s\n%s", index+1, skill.Name, skill.Description, utils.TruncateMiddle(skill.Content, 2000)))
	}
	return []schema.Message{{Role: schema.RoleSystem, Content: strings.Join(lines, "\n\n")}}
}

func (l *Loader) Select(query string, maxSkills int) []Skill {
	if maxSkills <= 0 {
		maxSkills = 2
	}
	queryTokens := tokenize(query)
	skills := l.Loaded()
	if len(skills) == 0 {
		return nil
	}
	type scored struct {
		skill Skill
		score int
	}
	results := make([]scored, 0, len(skills))
	for _, skill := range skills {
		score := scoreSkill(skill, queryTokens)
		if score > 0 {
			results = append(results, scored{skill: skill, score: score})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].score == results[j].score {
			return results[i].skill.Name < results[j].skill.Name
		}
		return results[i].score > results[j].score
	})
	if len(results) > maxSkills {
		results = results[:maxSkills]
	}
	selected := make([]Skill, 0, len(results))
	for _, item := range results {
		selected = append(selected, item.skill)
	}
	return selected
}

func readSkill(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	content := string(data)
	directory := filepath.Base(filepath.Dir(path))
	description := firstParagraph(content)
	if description == "" {
		description = directory
	}
	return Skill{
		Name:        directory,
		Description: description,
		Path:        path,
		Content:     content,
	}, nil
}

func firstParagraph(content string) string {
	lines := strings.Split(content, "\n")
	collecting := false
	var parts []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if collecting && len(parts) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") && !collecting {
			continue
		}
		collecting = true
		parts = append(parts, trimmed)
		if len(parts) >= 2 {
			break
		}
	}
	result := strings.Join(parts, " ")
	result = strings.TrimSpace(result)
	return result
}

func scoreSkill(skill Skill, tokens []string) int {
	if len(tokens) == 0 {
		return 1
	}
	text := strings.ToLower(skill.Name + " " + skill.Description + " " + skill.Content)
	score := 0
	for _, token := range tokens {
		if strings.Contains(text, token) {
			score++
		}
	}
	return score
}

func tokenize(query string) []string {
	query = strings.ToLower(query)
	fields := strings.Fields(query)
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, " ,.;:!?()[]{}<>")
		if field != "" {
			tokens = append(tokens, field)
		}
	}
	return tokens
}
