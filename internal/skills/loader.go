package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/whiter001/agent-go/internal/config"
	"github.com/whiter001/agent-go/internal/schema"
	"github.com/whiter001/agent-go/internal/utils"
)

var (
	skillTokenPattern      = regexp.MustCompile(`[\p{Han}]+|[A-Za-z0-9][A-Za-z0-9._/-]*`)
	skillTokenSplitPattern = regexp.MustCompile(`[._/-]+`)
)

type Skill struct {
	Name        string
	Description string
	Path        string
	Content     string
	Tags        []string
	Tools       []string
	Triggers    []string
	Platform    string
	Sections    []SkillSection
}

type SkillSection struct {
	Heading string
	Level   int
	Content string
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
	profile := buildQueryProfile(query)
	lines := []string{"## Relevant Skills", "Use these skill notes when they are helpful for the current request."}
	for index, skill := range selected {
		lines = append(lines, renderSkillContext(index+1, skill, profile))
	}
	return []schema.Message{{Role: schema.RoleSystem, Content: strings.Join(lines, "\n\n")}}
}

func (l *Loader) Select(query string, maxSkills int) []Skill {
	if maxSkills <= 0 {
		maxSkills = 2
	}
	profile := buildQueryProfile(query)
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
		score := scoreSkill(skill, profile)
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
	frontmatter, body := parseFrontmatter(content)
	directory := filepath.Base(filepath.Dir(path))
	name := directory
	if strings.TrimSpace(frontmatter.Name) != "" {
		name = strings.TrimSpace(frontmatter.Name)
	}
	description := strings.TrimSpace(frontmatter.Description)
	if description == "" {
		description = firstParagraph(body)
	}
	if description == "" {
		description = name
	}
	return Skill{
		Name:        name,
		Description: description,
		Path:        path,
		Content:     strings.TrimSpace(body),
		Tags:        frontmatter.Tags,
		Tools:       frontmatter.Tools,
		Triggers:    frontmatter.Triggers,
		Platform:    frontmatter.Platform,
		Sections:    parseSkillSections(body),
	}, nil
}

func firstParagraph(content string) string {
	lines := strings.Split(content, "\n")
	start := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for index := 1; index < len(lines); index++ {
			if strings.TrimSpace(lines[index]) == "---" {
				start = index + 1
				break
			}
		}
	}
	collecting := false
	var parts []string
	for _, line := range lines[start:] {
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

func scoreSkill(skill Skill, query queryProfile) int {
	if len(query.Tokens) == 0 {
		return 1
	}
	score := 0
	score += phraseScore(skill.Name, query.Compact, 28)
	score += phraseScore(strings.Join(skill.Tools, " "), query.Compact, 22)
	score += phraseScore(strings.Join(skill.Triggers, " "), query.Compact, 18)
	score += phraseScore(skill.Description, query.Compact, 12)

	score += fieldTokenScore(skill.Name, query.Tokens, 12)
	score += fieldTokenScore(strings.Join(skill.Tools, " "), query.Tokens, 10)
	score += fieldTokenScore(strings.Join(skill.Triggers, " "), query.Tokens, 9)
	score += fieldTokenScore(strings.Join(skill.Tags, " "), query.Tokens, 8)
	score += fieldTokenScore(skill.Platform, query.Tokens, 8)
	score += fieldTokenScore(skill.Description, query.Tokens, 6)
	for _, section := range skill.Sections {
		score += fieldTokenScore(section.Heading, query.Tokens, 4)
	}
	score += fieldTokenScore(skill.Content, query.Tokens, 2)

	if matchesAllTokens(strings.Join([]string{
		skill.Name,
		skill.Description,
		strings.Join(skill.Tools, " "),
		strings.Join(skill.Triggers, " "),
		strings.Join(skill.Tags, " "),
		skill.Platform,
	}, " "), query.Tokens) {
		score += 10
	}

	return score
}

func tokenize(query string) []string {
	query = strings.ToLower(query)
	matches := skillTokenPattern.FindAllString(query, -1)
	tokens := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, match := range matches {
		for _, token := range expandSkillToken(match) {
			token = strings.Trim(token, " ,.;:!?()[]{}<>\"'")
			if token == "" {
				continue
			}
			if _, ok := seen[token]; ok {
				continue
			}
			seen[token] = struct{}{}
			tokens = append(tokens, token)
		}
	}
	return tokens
}

type skillFrontmatter struct {
	Name        string
	Description string
	Tags        []string
	Tools       []string
	Triggers    []string
	Platform    string
}

type queryProfile struct {
	Raw     string
	Tokens  []string
	Compact string
}

func buildQueryProfile(query string) queryProfile {
	return queryProfile{
		Raw:     strings.TrimSpace(query),
		Tokens:  tokenize(query),
		Compact: compactText(query),
	}
}

func parseFrontmatter(content string) (skillFrontmatter, string) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return skillFrontmatter{}, strings.TrimSpace(content)
	}
	end := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			end = index
			break
		}
	}
	if end == -1 {
		return skillFrontmatter{}, strings.TrimSpace(content)
	}
	return parseFrontmatterLines(lines[1:end]), strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
}

func parseFrontmatterLines(lines []string) skillFrontmatter {
	var metadata skillFrontmatter
	currentListKey := ""
	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") && currentListKey != "" {
			metadata.appendListValue(currentListKey, trimFrontmatterValue(strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))))
			continue
		}
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			currentListKey = ""
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		currentListKey = ""
		switch key {
		case "name", "description", "platform":
			if value != "" {
				metadata.setScalarValue(key, trimFrontmatterValue(value))
			}
		case "tags", "tools", "triggers":
			if value == "" {
				currentListKey = key
				continue
			}
			for _, item := range parseFrontmatterList(value) {
				metadata.appendListValue(key, item)
			}
		}
	}
	metadata.Tags = uniqueOrderedStrings(metadata.Tags)
	metadata.Tools = uniqueOrderedStrings(metadata.Tools)
	metadata.Triggers = uniqueOrderedStrings(metadata.Triggers)
	return metadata
}

func parseFrontmatterList(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		trimmed = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	}
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		item := trimFrontmatterValue(part)
		if item != "" {
			items = append(items, item)
		}
	}
	return uniqueOrderedStrings(items)
}

func trimFrontmatterValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 2 {
		if (trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'') || (trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"') {
			trimmed = trimmed[1 : len(trimmed)-1]
		}
	}
	return strings.TrimSpace(trimmed)
}

func (metadata *skillFrontmatter) setScalarValue(key string, value string) {
	switch key {
	case "name":
		metadata.Name = value
	case "description":
		metadata.Description = value
	case "platform":
		metadata.Platform = value
	}
}

func (metadata *skillFrontmatter) appendListValue(key string, value string) {
	if value == "" {
		return
	}
	switch key {
	case "tags":
		metadata.Tags = append(metadata.Tags, value)
	case "tools":
		metadata.Tools = append(metadata.Tools, value)
	case "triggers":
		metadata.Triggers = append(metadata.Triggers, value)
	}
}

func parseSkillSections(content string) []SkillSection {
	lines := strings.Split(content, "\n")
	sections := make([]SkillSection, 0, 4)
	current := SkillSection{}
	currentLines := make([]string, 0, len(lines))
	flush := func() {
		text := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if current.Heading == "" && text == "" {
			currentLines = currentLines[:0]
			return
		}
		sections = append(sections, SkillSection{Heading: current.Heading, Level: current.Level, Content: text})
		current = SkillSection{}
		currentLines = currentLines[:0]
	}

	for _, line := range lines {
		if heading, level, ok := parseHeading(line); ok {
			flush()
			current.Heading = heading
			current.Level = level
			continue
		}
		currentLines = append(currentLines, line)
	}
	flush()
	return sections
}

func parseHeading(line string) (string, int, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.HasPrefix(trimmed, "#") {
		return "", 0, false
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level >= len(trimmed) || trimmed[level] != ' ' {
		return "", 0, false
	}
	return strings.TrimSpace(trimmed[level:]), level, true
}

func renderSkillContext(index int, skill Skill, query queryProfile) string {
	lines := []string{fmt.Sprintf("%d. %s: %s", index, skill.Name, skill.Description)}
	metadata := make([]string, 0, 4)
	if len(skill.Tools) > 0 {
		metadata = append(metadata, "Tools: "+strings.Join(skill.Tools, ", "))
	}
	if len(skill.Tags) > 0 {
		metadata = append(metadata, "Tags: "+strings.Join(skill.Tags, ", "))
	}
	if len(skill.Triggers) > 0 {
		metadata = append(metadata, "Triggers: "+strings.Join(skill.Triggers, ", "))
	}
	if strings.TrimSpace(skill.Platform) != "" {
		metadata = append(metadata, "Platform: "+skill.Platform)
	}
	if len(metadata) > 0 {
		lines = append(lines, strings.Join(metadata, "\n"))
	}
	if excerpt := buildRelevantExcerpt(skill, query); excerpt != "" {
		lines = append(lines, excerpt)
	}
	return strings.Join(lines, "\n")
}

func buildRelevantExcerpt(skill Skill, query queryProfile) string {
	sections := selectRelevantSections(skill, query, 2)
	if len(sections) == 0 {
		return utils.TruncateMiddle(strings.TrimSpace(skill.Content), 2000)
	}
	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		text := strings.TrimSpace(section.Content)
		if text == "" {
			continue
		}
		heading := section.Heading
		if heading == "" {
			heading = "Overview"
		}
		parts = append(parts, fmt.Sprintf("### %s\n%s", heading, utils.TruncateMiddle(text, 900)))
	}
	if len(parts) == 0 {
		return utils.TruncateMiddle(strings.TrimSpace(skill.Content), 2000)
	}
	return utils.TruncateMiddle(strings.Join(parts, "\n\n"), 2000)
}

func selectRelevantSections(skill Skill, query queryProfile, maxSections int) []SkillSection {
	if len(skill.Sections) == 0 {
		return nil
	}
	if maxSections <= 0 {
		maxSections = 2
	}
	if len(query.Tokens) == 0 {
		return firstPopulatedSections(skill.Sections, maxSections)
	}
	type scoredSection struct {
		section SkillSection
		score   int
		index   int
	}
	results := make([]scoredSection, 0, len(skill.Sections))
	for index, section := range skill.Sections {
		if strings.TrimSpace(section.Content) == "" {
			continue
		}
		score := scoreSection(section, query)
		if index == 0 && score == 0 {
			score = 1
		}
		if score > 0 {
			results = append(results, scoredSection{section: section, score: score, index: index})
		}
	}
	if len(results) == 0 {
		return firstPopulatedSections(skill.Sections, maxSections)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].score == results[j].score {
			return results[i].index < results[j].index
		}
		return results[i].score > results[j].score
	})
	if len(results) > maxSections {
		results = results[:maxSections]
	}
	selected := make([]SkillSection, 0, len(results))
	for _, item := range results {
		selected = append(selected, item.section)
	}
	return selected
}

func firstPopulatedSections(sections []SkillSection, maxSections int) []SkillSection {
	selected := make([]SkillSection, 0, maxSections)
	for _, section := range sections {
		if strings.TrimSpace(section.Content) == "" {
			continue
		}
		selected = append(selected, section)
		if len(selected) >= maxSections {
			break
		}
	}
	return selected
}

func scoreSection(section SkillSection, query queryProfile) int {
	score := 0
	score += phraseScore(section.Heading, query.Compact, 14)
	score += phraseScore(section.Content, query.Compact, 6)
	score += fieldTokenScore(section.Heading, query.Tokens, 6)
	score += fieldTokenScore(section.Content, query.Tokens, 2)
	return score
}

func fieldTokenScore(text string, tokens []string, weight int) int {
	if len(tokens) == 0 || weight <= 0 {
		return 0
	}
	normalized := strings.ToLower(text)
	score := 0
	for _, token := range tokens {
		if strings.Contains(normalized, token) {
			score += weight
		}
	}
	return score
}

func phraseScore(text string, queryCompact string, weight int) int {
	if queryCompact == "" || weight <= 0 {
		return 0
	}
	if strings.Contains(compactText(text), queryCompact) {
		return weight
	}
	return 0
}

func compactText(text string) string {
	matches := skillTokenPattern.FindAllString(strings.ToLower(text), -1)
	return strings.Join(matches, "")
}

func matchesAllTokens(text string, tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	normalized := strings.ToLower(text)
	for _, token := range tokens {
		if !strings.Contains(normalized, token) {
			return false
		}
	}
	return true
}

func expandSkillToken(token string) []string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return nil
	}
	items := []string{trimmed}
	if strings.ContainsAny(trimmed, "._/-") {
		for _, part := range skillTokenSplitPattern.Split(trimmed, -1) {
			part = strings.TrimSpace(part)
			if part != "" {
				items = append(items, part)
			}
		}
	}
	if containsHan(trimmed) {
		runes := []rune(trimmed)
		if len(runes) > 2 {
			maxGram := minInt(4, len(runes))
			for size := 2; size <= maxGram; size++ {
				for start := 0; start+size <= len(runes); start++ {
					items = append(items, string(runes[start:start+size]))
				}
			}
		}
	}
	return uniqueOrderedStrings(items)
}

func containsHan(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func uniqueOrderedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
