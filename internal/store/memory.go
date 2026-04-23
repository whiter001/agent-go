package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/whiter001/agent-go/internal/config"
	"github.com/whiter001/agent-go/internal/schema"
	"github.com/whiter001/agent-go/internal/utils"
)

type MemoryEntry struct {
	ID        int       `json:"id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Store struct {
	rootDir    string
	filePath   string
	memoryPath string
	userPath   string
	mu         sync.Mutex
	entries    []MemoryEntry
}

func New(rootDir string) (*Store, error) {
	resolved := config.ExpandPath(rootDir)
	if resolved == "" {
		resolved = defaultRootDir()
	}
	if err := os.MkdirAll(resolved, 0o755); err != nil {
		return nil, err
	}
	store := &Store{
		rootDir:    resolved,
		filePath:   filepath.Join(resolved, "memory.json"),
		memoryPath: filepath.Join(resolved, "MEMORY.md"),
		userPath:   filepath.Join(resolved, "USER.md"),
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	if err := store.refreshSnapshots(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) RootDir() string {
	return s.rootDir
}

func (s *Store) MemoryPath() string {
	return s.memoryPath
}

func (s *Store) UserPath() string {
	return s.userPath
}

func (s *Store) StoreMemory(content string, title string, tags []string) MemoryEntry {
	return s.insert("memory", content, title, tags)
}

func (s *Store) StoreUserProfile(content string, title string, tags []string) MemoryEntry {
	return s.insert("user", content, title, tags)
}

func (s *Store) Search(query string, limit int, kinds []string) []MemoryEntry {
	query = strings.TrimSpace(query)
	if limit <= 0 {
		limit = 5
	}

	s.mu.Lock()
	entries := append([]MemoryEntry(nil), s.entries...)
	s.mu.Unlock()

	if query == "" {
		return latestEntries(entries, limit, kinds)
	}

	tokens := tokenize(query)
	if len(tokens) == 0 {
		return latestEntries(entries, limit, kinds)
	}

	allowedKinds := kindSet(kinds)
	type scored struct {
		entry MemoryEntry
		score int
	}
	results := make([]scored, 0, len(entries))
	for _, entry := range entries {
		if len(allowedKinds) != 0 && !allowedKinds[strings.ToLower(entry.Kind)] {
			continue
		}
		score := scoreEntry(entry, tokens)
		if score <= 0 {
			continue
		}
		results = append(results, scored{entry: entry, score: score})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].score == results[j].score {
			return results[i].entry.UpdatedAt.After(results[j].entry.UpdatedAt)
		}
		return results[i].score > results[j].score
	})

	if len(results) > limit {
		results = results[:limit]
	}
	output := make([]MemoryEntry, 0, len(results))
	for _, item := range results {
		output = append(output, item.entry)
	}
	return output
}

func (s *Store) BuildSystemPrompt() string {
	parts := []string{"## Persistent Memory", "The following durable notes and user facts are stored under `~/.agent-go/`."}
	userSnapshot := readSnapshot(s.userPath, 1200)
	if userSnapshot != "" {
		parts = append(parts, "### USER.md", userSnapshot)
	}
	memorySnapshot := readSnapshot(s.memoryPath, 1800)
	if memorySnapshot != "" {
		parts = append(parts, "### MEMORY.md", memorySnapshot)
	}
	if len(parts) == 2 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

func (s *Store) BuildTurnContext(query string, limit int) []schema.Message {
	hits := s.Search(query, limit, nil)
	if len(hits) == 0 {
		hits = latestEntries(s.snapshotEntries(), limit, nil)
	}
	if len(hits) == 0 {
		return nil
	}
	lines := []string{"## Relevant Persistent Memory", "Use these stored facts and preferences when answering the current request."}
	for index, hit := range hits {
		tags := ""
		if len(hit.Tags) > 0 {
			tags = fmt.Sprintf(" [%s]", strings.Join(hit.Tags, ", "))
		}
		lines = append(lines, fmt.Sprintf("%d. [%s] %s%s: %s", index+1, hit.Kind, hit.Title, tags, hit.Content))
	}
	return []schema.Message{{Role: schema.RoleSystem, Content: strings.Join(lines, "\n")}}
}

func (s *Store) insert(kind, content, title string, tags []string) MemoryEntry {
	now := time.Now().UTC()
	entry := MemoryEntry{
		Kind:      kind,
		Title:     normalizeTitle(title, content),
		Content:   strings.TrimSpace(content),
		Tags:      normalizeTags(tags),
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	entry.ID = nextID(s.entries)
	s.entries = append(s.entries, entry)
	if err := s.persistLocked(); err != nil {
		return entry
	}
	_ = s.refreshSnapshotsLocked()
	return entry
}

func (s *Store) snapshotEntries() []MemoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]MemoryEntry(nil), s.entries...)
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var entries []MemoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	s.entries = entries
	return nil
}

func (s *Store) persistLocked() error {
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.filePath, data, 0o644); err != nil {
		return err
	}
	return nil
}

func (s *Store) refreshSnapshots() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshSnapshotsLocked()
}

func (s *Store) refreshSnapshotsLocked() error {
	var memoryLines []string
	var userLines []string
	for _, entry := range s.entries {
		line := formatEntry(entry)
		switch entry.Kind {
		case "user":
			userLines = append(userLines, line)
		default:
			memoryLines = append(memoryLines, line)
		}
	}
	if err := writeSnapshotFile(s.memoryPath, "Persistent Memory", memoryLines); err != nil {
		return err
	}
	if err := writeSnapshotFile(s.userPath, "User Facts", userLines); err != nil {
		return err
	}
	return nil
}

func latestEntries(entries []MemoryEntry, limit int, kinds []string) []MemoryEntry {
	if limit <= 0 {
		limit = 5
	}
	allowedKinds := kindSet(kinds)
	filtered := make([]MemoryEntry, 0, len(entries))
	for _, entry := range entries {
		if len(allowedKinds) != 0 && !allowedKinds[strings.ToLower(entry.Kind)] {
			continue
		}
		filtered = append(filtered, entry)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
	})
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

func scoreEntry(entry MemoryEntry, tokens []string) int {
	text := strings.ToLower(entry.Title + " " + entry.Content + " " + strings.Join(entry.Tags, " "))
	score := 0
	for _, token := range tokens {
		if strings.Contains(text, token) {
			score++
		}
		if strings.Contains(strings.ToLower(entry.Title), token) {
			score++
		}
		for _, tag := range entry.Tags {
			if strings.EqualFold(tag, token) {
				score += 2
			}
		}
	}
	if score > 0 {
		ageHours := time.Since(entry.UpdatedAt).Hours()
		if ageHours < 24 {
			score += 2
		} else if ageHours < 24*7 {
			score += 1
		}
	}
	return score
}

func tokenize(query string) []string {
	stopwords := map[string]struct{}{
		"a": {}, "an": {}, "and": {}, "do": {}, "for": {}, "from": {}, "how": {}, "i": {}, "in": {}, "into": {},
		"is": {}, "of": {}, "on": {}, "or": {}, "the": {}, "to": {}, "via": {}, "with": {},
	}
	fields := strings.Fields(strings.ToLower(query))
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, " ,.;:!?()[]{}<>")
		if field == "" {
			continue
		}
		if _, ok := stopwords[field]; ok {
			continue
		}
		tokens = append(tokens, field)
	}
	return tokens
}

func kindSet(kinds []string) map[string]bool {
	if len(kinds) == 0 {
		return nil
	}
	result := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		result[strings.ToLower(kind)] = true
	}
	return result
}

func normalizeTitle(title, content string) string {
	trimmed := strings.TrimSpace(title)
	if trimmed != "" {
		return trimmed
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "Untitled"
	}
	if newline := strings.IndexByte(content, '\n'); newline >= 0 {
		content = content[:newline]
	}
	words := strings.Fields(content)
	if len(words) > 8 {
		words = words[:8]
	}
	return strings.Join(words, " ")
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	cleaned := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			cleaned = append(cleaned, tag)
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

func nextID(entries []MemoryEntry) int {
	maxID := 0
	for _, entry := range entries {
		if entry.ID > maxID {
			maxID = entry.ID
		}
	}
	return maxID + 1
}

func formatEntry(entry MemoryEntry) string {
	tags := ""
	if len(entry.Tags) > 0 {
		tags = fmt.Sprintf(" [%s]", strings.Join(entry.Tags, ", "))
	}
	return fmt.Sprintf("- %s%s: %s", entry.Title, tags, entry.Content)
}

func writeSnapshotFile(path, title string, lines []string) error {
	var builder strings.Builder
	builder.WriteString("# ")
	builder.WriteString(title)
	builder.WriteString("\n\n")
	if len(lines) == 0 {
		builder.WriteString("_No entries yet._\n")
	} else {
		for _, line := range lines {
			builder.WriteString(line)
			builder.WriteString("\n")
		}
	}
	return os.WriteFile(path, []byte(builder.String()), 0o644)
}

func readSnapshot(path string, maxChars int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return ""
	}
	if maxChars > 0 {
		content = utils.TruncateMiddle(content, maxChars)
	}
	return content
}

func defaultRootDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".agent-go"
	}
	return filepath.Join(home, ".agent-go")
}
