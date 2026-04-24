package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/whiter001/agent-go/internal/config"
	"github.com/whiter001/agent-go/internal/schema"
)

const skillFeedbackEventLimit = 200

type SkillFeedbackStats struct {
	Selections     int    `json:"selections"`
	SuccessfulRuns int    `json:"successful_runs"`
	HelpfulRuns    int    `json:"helpful_runs"`
	LastSelectedAt string `json:"last_selected_at,omitempty"`
	LastSuccessAt  string `json:"last_success_at,omitempty"`
	LastHelpfulAt  string `json:"last_helpful_at,omitempty"`
}

type skillFeedbackEvent struct {
	Time           string   `json:"time"`
	Prompt         string   `json:"prompt"`
	SelectedSkills []string `json:"selected_skills"`
	UsedTools      []string `json:"used_tools,omitempty"`
	HelpfulSkills  []string `json:"helpful_skills,omitempty"`
	Success        bool     `json:"success"`
}

type skillFeedbackStore struct {
	Skills map[string]SkillFeedbackStats `json:"skills"`
	Events []skillFeedbackEvent          `json:"events,omitempty"`
}

func DefaultFeedbackStorePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".agent-go", "skill-feedback.json")
	}
	return filepath.Join(home, ".agent-go", "skill-feedback.json")
}

func RecordSkillSelectionFeedback(path string, prompt string, selected []Skill, messages []schema.Message) error {
	resolved := config.ExpandPath(path)
	if strings.TrimSpace(resolved) == "" || len(selected) == 0 {
		return nil
	}
	store, err := loadSkillFeedbackStore(resolved)
	if err != nil {
		return err
	}
	executions, finalAssistant := extractToolExecutions(messages)
	usedTools := uniqueToolNames(executions)
	success := inferRunSuccess(executions, finalAssistant)
	helpfulSkills := helpfulSkillPaths(selected, usedTools, success)
	helpfulSet := map[string]struct{}{}
	for _, skillPath := range helpfulSkills {
		helpfulSet[skillPath] = struct{}{}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if store.Skills == nil {
		store.Skills = map[string]SkillFeedbackStats{}
	}
	selectedPaths := selectedSkillPaths(selected)
	for _, skill := range selected {
		if strings.TrimSpace(skill.Path) == "" {
			continue
		}
		stats := store.Skills[skill.Path]
		stats.Selections++
		stats.LastSelectedAt = now
		if success {
			stats.SuccessfulRuns++
			stats.LastSuccessAt = now
		}
		if _, ok := helpfulSet[skill.Path]; ok {
			stats.HelpfulRuns++
			stats.LastHelpfulAt = now
		}
		store.Skills[skill.Path] = stats
	}
	store.Events = append(store.Events, skillFeedbackEvent{
		Time:           now,
		Prompt:         strings.TrimSpace(prompt),
		SelectedSkills: selectedPaths,
		UsedTools:      usedTools,
		HelpfulSkills:  helpfulSkills,
		Success:        success,
	})
	if len(store.Events) > skillFeedbackEventLimit {
		store.Events = store.Events[len(store.Events)-skillFeedbackEventLimit:]
	}
	return saveSkillFeedbackStore(resolved, store)
}

func loadSkillFeedbackStats(path string) (map[string]SkillFeedbackStats, error) {
	store, err := loadSkillFeedbackStore(path)
	if err != nil {
		return nil, err
	}
	if store.Skills == nil {
		store.Skills = map[string]SkillFeedbackStats{}
	}
	return store.Skills, nil
}

func loadSkillFeedbackStore(path string) (skillFeedbackStore, error) {
	resolved := config.ExpandPath(path)
	if strings.TrimSpace(resolved) == "" {
		return skillFeedbackStore{Skills: map[string]SkillFeedbackStats{}}, nil
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return skillFeedbackStore{Skills: map[string]SkillFeedbackStats{}}, nil
		}
		return skillFeedbackStore{}, err
	}
	if len(data) == 0 {
		return skillFeedbackStore{Skills: map[string]SkillFeedbackStats{}}, nil
	}
	var store skillFeedbackStore
	if err := json.Unmarshal(data, &store); err != nil {
		return skillFeedbackStore{}, err
	}
	if store.Skills == nil {
		store.Skills = map[string]SkillFeedbackStats{}
	}
	return store, nil
}

func saveSkillFeedbackStore(path string, store skillFeedbackStore) error {
	resolved := config.ExpandPath(path)
	if strings.TrimSpace(resolved) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(resolved, data, 0o644)
}

func selectedSkillPaths(selected []Skill) []string {
	paths := make([]string, 0, len(selected))
	for _, skill := range selected {
		if strings.TrimSpace(skill.Path) == "" {
			continue
		}
		paths = append(paths, skill.Path)
	}
	return uniqueOrderedStrings(paths)
}

func helpfulSkillPaths(selected []Skill, usedTools []string, success bool) []string {
	if !success || len(usedTools) == 0 {
		return nil
	}
	usedToolSet := map[string]struct{}{}
	for _, toolName := range usedTools {
		usedToolSet[strings.ToLower(toolName)] = struct{}{}
	}
	helpful := make([]string, 0, len(selected))
	for _, skill := range selected {
		if len(skill.Tools) == 0 || strings.TrimSpace(skill.Path) == "" {
			continue
		}
		for _, toolName := range skill.Tools {
			if _, ok := usedToolSet[strings.ToLower(toolName)]; ok {
				helpful = append(helpful, skill.Path)
				break
			}
		}
	}
	return uniqueOrderedStrings(helpful)
}

func feedbackRankingBonus(stats SkillFeedbackStats) int {
	bonus := 0
	bonus += minInt(stats.HelpfulRuns*6, 24)
	bonus += minInt(stats.SuccessfulRuns*3, 15)
	bonus += minInt(stats.Selections, 5)
	bonus += recencyBonus(stats.LastHelpfulAt, 4, 2)
	bonus += recencyBonus(stats.LastSuccessAt, 2, 1)
	return bonus
}

func recencyBonus(value string, recentBonus int, warmBonus int) int {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0
	}
	timestamp, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return 0
	}
	age := time.Since(timestamp)
	if age <= 7*24*time.Hour {
		return recentBonus
	}
	if age <= 30*24*time.Hour {
		return warmBonus
	}
	return 0
}
