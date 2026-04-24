package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	agentruntime "github.com/whiter001/agent-go/internal/agent"
	"github.com/whiter001/agent-go/internal/config"
	"github.com/whiter001/agent-go/internal/llm"
	"github.com/whiter001/agent-go/internal/logging"
	"github.com/whiter001/agent-go/internal/schema"
	"github.com/whiter001/agent-go/internal/skills"
	"github.com/whiter001/agent-go/internal/store"
	agenttools "github.com/whiter001/agent-go/internal/tools"
	"github.com/whiter001/agent-go/internal/utils"
)

const Version = "0.1.0"

const (
	defaultPromptTokenLimit = 12000
	defaultSystemPrompt     = `You are agent-go, a practical multi-step assistant.
Use the available tools whenever they help you complete the user's request.
When a user asks you to operate a local CLI or inspect command help, prefer using the bash tool and report real results instead of guessing.
If skills or memory context is provided, use it when relevant.
Keep responses concise, factual, and grounded in actual tool output.`
)

var (
	promptClientFactory = llm.NewClient
	promptLoggerFactory = func() (*logging.Logger, error) {
		return logging.New("")
	}
	promptMemoryStoreFactory = func() (*store.Store, error) {
		return store.New("")
	}
)

type Args struct {
	Workspace string
	Prompt    string
	Command   string
	Topic     string
	Filename  string
	Version   bool
}

func Main(argv []string, stdout, stderr io.Writer) error {
	args := ParseArgs(argv)

	if args.Version {
		_, _ = fmt.Fprintf(stdout, "agent-go %s\n", Version)
		return nil
	}

	switch args.Command {
	case "help":
		printHelp(stdout, args.Topic)
		return nil
	case "log":
		if args.Filename != "" {
			return readLogFile(stdout, stderr, args.Filename)
		}
		return showLogDirectory(stdout, stderr)
	}

	if err := loadDefaultDotEnv(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if args.Workspace != "" {
		cfg.Agent.WorkspaceDir = args.Workspace
	}

	if args.Prompt != "" {
		return runPromptMode(stdout, stderr, cfg, args.Prompt)
	}

	return runInteractiveMode(stdout, stderr, cfg)
}

func ParseArgs(argv []string) Args {
	args := Args{}
	for i := 0; i < len(argv); i++ {
		current := argv[i]
		switch current {
		case "-w", "--workspace":
			if i+1 < len(argv) {
				args.Workspace = argv[i+1]
				i++
			}
		case "-p", "--prompt", "-t", "--task":
			if i+1 < len(argv) {
				args.Prompt = argv[i+1]
				i++
			}
		case "-v", "--version":
			args.Version = true
		case "help":
			args.Command = "help"
			if i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "-") {
				args.Topic = argv[i+1]
				i++
			}
		case "log":
			args.Command = "log"
			if i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "-") {
				args.Filename = argv[i+1]
				i++
			}
		default:
			if strings.HasPrefix(current, "-") {
				continue
			}
		}
	}
	return args
}

func printHelp(stdout io.Writer, topic string) {
	if strings.EqualFold(strings.TrimSpace(topic), "log") {
		_, _ = fmt.Fprintln(stdout, "Log Command:")
		_, _ = fmt.Fprintln(stdout, "  agent-go log              Show log directory and recent files")
		_, _ = fmt.Fprintln(stdout, "  agent-go log <file>       Read a specific log file")
		_, _ = fmt.Fprintln(stdout, "  agent-go help log         Show log command help")
		return
	}

	_, _ = fmt.Fprintln(stdout, "Mini-Agent CLI:")
	_, _ = fmt.Fprintln(stdout, "  agent-go                  Start interactive mode")
	_, _ = fmt.Fprintln(stdout, "  agent-go -p \"<text>\"    Run a prompt non-interactively and exit")
	_, _ = fmt.Fprintln(stdout, "  agent-go --workspace DIR  Use a specific workspace directory")
	_, _ = fmt.Fprintln(stdout, "  agent-go log [file]       Show logs or read a specific log file")
	_, _ = fmt.Fprintln(stdout, "  agent-go help [topic]     Show this help or a topic-specific help")
	_, _ = fmt.Fprintln(stdout, "  agent-go --version        Show version information")
	_, _ = fmt.Fprintln(stdout, "")
	_, _ = fmt.Fprintln(stdout, "Interactive Commands:")
	_, _ = fmt.Fprintln(stdout, "  /help      Show this help message")
	_, _ = fmt.Fprintln(stdout, "  /clear     Clear session history (keep system prompt)")
	_, _ = fmt.Fprintln(stdout, "  /history   Show current session message count")
	_, _ = fmt.Fprintln(stdout, "  /stats     Show session statistics")
	_, _ = fmt.Fprintln(stdout, "  /log       Show log directory and recent files")
	_, _ = fmt.Fprintln(stdout, "  /log <f>   Read a specific log file")
	_, _ = fmt.Fprintln(stdout, "  /exit      Exit program (also: exit, quit, q)")
}

func showLogDirectory(stdout, stderr io.Writer) error {
	logDir := defaultLogDir()
	_, _ = fmt.Fprintf(stdout, "\nLog Directory: %s\n", logDir)
	info, err := os.Stat(logDir)
	if err != nil || !info.IsDir() {
		_, _ = fmt.Fprintf(stdout, "Log directory does not exist: %s\n\n", logDir)
		return nil
	}

	entries, err := os.ReadDir(logDir)
	if err != nil {
		return err
	}

	files := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".log") {
			files = append(files, entry)
		}
	}
	if len(files) == 0 {
		_, _ = fmt.Fprintln(stdout, "No log files found in directory.")
		return nil
	}

	sort.Slice(files, func(i, j int) bool {
		left, leftErr := os.Stat(filepath.Join(logDir, files[i].Name()))
		right, rightErr := os.Stat(filepath.Join(logDir, files[j].Name()))
		if leftErr != nil || rightErr != nil {
			return files[i].Name() > files[j].Name()
		}
		return left.ModTime().After(right.ModTime())
	})

	_, _ = fmt.Fprintln(stdout, "Available Log Files (newest first):")
	for i, file := range files {
		if i >= 10 {
			_, _ = fmt.Fprintf(stdout, "... and %d more files\n", len(files)-10)
			break
		}
		stat, err := os.Stat(filepath.Join(logDir, file.Name()))
		if err != nil {
			continue
		}
		size := humanFileSize(stat.Size())
		_, _ = fmt.Fprintf(stdout, "  %2d. %s (modified %s, size %s)\n", i+1, file.Name(), stat.ModTime().Format("2006-01-02 15:04:05"), size)
	}
	_, _ = fmt.Fprintln(stdout)
	return nil
}

func readLogFile(stdout, stderr io.Writer, filename string) error {
	logDir := defaultLogDir()
	path := filename
	if !filepath.IsAbs(path) {
		path = filepath.Join(logDir, filename)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "log file not found: %s\n", path)
		return nil
	}
	_, _ = fmt.Fprintf(stdout, "\nReading: %s\n", path)
	_, _ = fmt.Fprintln(stdout, strings.Repeat("-", 80))
	_, _ = fmt.Fprint(stdout, string(data))
	_, _ = fmt.Fprintln(stdout, strings.Repeat("-", 80))
	_, _ = fmt.Fprintln(stdout, "End of file")
	return nil
}

func runPromptMode(stdout, stderr io.Writer, cfg config.Config, prompt string) error {
	if handled, err := tryBuiltinPrompt(stdout, cfg, prompt); handled || err != nil {
		return err
	}
	if strings.TrimSpace(cfg.LLM.APIKey) == "" {
		_, _ = fmt.Fprintln(stdout, "Missing API key. Set AGENT_GO_API_KEY or a config file before running the agent.")
		return nil
	}

	client, err := promptClientFactory(cfg.LLM.APIKey, cfg.LLM.APIBase, cfg.LLM.Model, cfg.LLM.Provider, cfg.LLM.Retry)
	if err != nil {
		return err
	}
	client.SetRetryCallback(func(err error, attempt int) {
		_, _ = fmt.Fprintf(stderr, "Retry %d after error: %v\n", attempt, err)
	})

	logger, err := promptLoggerFactory()
	if err != nil {
		return err
	}
	if logger != nil {
		defer logger.Close()
	}

	memoryStore, err := configuredMemoryStore(cfg)
	if err != nil {
		return err
	}
	skillLoader, err := configuredSkillLoader(cfg)
	if err != nil {
		return err
	}
	systemPrompt, err := configuredSystemPrompt(cfg, memoryStore, skillLoader)
	if err != nil {
		return err
	}

	workspace := currentWorkspace(cfg)
	toolList := configuredTools(cfg, workspace, memoryStore)
	agent := agentruntime.New(client, systemPrompt, toolList, normalizedMaxSteps(cfg.Agent.MaxSteps), defaultPromptTokenLimit, workspace, logger, stdout)
	agent.SetEphemeralContext(configuredTurnContext(cfg, prompt, memoryStore, skillLoader))
	agent.AddUserMessage(prompt)
	_, err = agent.Run(context.Background())
	return err
}

func tryBuiltinPrompt(stdout io.Writer, cfg config.Config, prompt string) (bool, error) {
	if isSkillListingPrompt(prompt) {
		return true, printAvailableSkills(stdout, cfg)
	}
	return false, nil
}

func isSkillListingPrompt(prompt string) bool {
	normalized := strings.ToLower(strings.TrimSpace(prompt))
	if normalized == "" {
		return false
	}
	if normalized == "skills" || normalized == "/skills" {
		return true
	}
	mentionsSkills := strings.Contains(normalized, "skill") || strings.Contains(normalized, "skills") || strings.Contains(normalized, "技能")
	if !mentionsSkills {
		return false
	}
	for _, keyword := range []string{"list", "show", "display", "列出", "显示", "查看", "哪些", "所有", "全部", "当前"} {
		if strings.Contains(normalized, keyword) {
			return true
		}
	}
	return false
}

func printAvailableSkills(stdout io.Writer, cfg config.Config) error {
	if !cfg.Tools.EnableSkills {
		_, _ = fmt.Fprintln(stdout, "Skills are disabled in configuration.")
		return nil
	}

	directories := configuredSkillDirectories(cfg)
	loader := skills.NewLoader(directories...)
	if err := loader.Discover(); err != nil {
		return err
	}

	loaded := loader.Loaded()
	if len(loaded) == 0 {
		_, _ = fmt.Fprintln(stdout, "No skills found.")
	} else {
		_, _ = fmt.Fprintf(stdout, "Available skills (%d):\n", len(loaded))
		for index, skill := range loaded {
			_, _ = fmt.Fprintf(stdout, "%d. %s\n", index+1, skill.Name)
			_, _ = fmt.Fprintf(stdout, "   Description: %s\n", skill.Description)
			_, _ = fmt.Fprintf(stdout, "   Path: %s\n", skill.Path)
		}
	}

	if len(directories) > 0 {
		_, _ = fmt.Fprintln(stdout, "Searched directories:")
		for _, directory := range directories {
			_, _ = fmt.Fprintf(stdout, "- %s\n", directory)
		}
	}
	return nil
}

func configuredSkillDirectories(cfg config.Config) []string {
	seen := map[string]struct{}{}
	directories := make([]string, 0, 2+len(cfg.Tools.SkillsExternalDirs))
	for _, candidate := range append([]string{cfg.Tools.SkillsDir, cfg.Tools.AutoSkillDir}, cfg.Tools.SkillsExternalDirs...) {
		resolved := config.ExpandPath(candidate)
		if resolved == "" {
			continue
		}
		if _, ok := seen[resolved]; ok {
			continue
		}
		seen[resolved] = struct{}{}
		directories = append(directories, resolved)
	}
	return directories
}

func configuredMemoryStore(cfg config.Config) (*store.Store, error) {
	if !cfg.Tools.EnableMemory {
		return nil, nil
	}
	return promptMemoryStoreFactory()
}

func configuredSkillLoader(cfg config.Config) (*skills.Loader, error) {
	if !cfg.Tools.EnableSkills {
		return nil, nil
	}
	loader := skills.NewLoader(configuredSkillDirectories(cfg)...)
	if err := loader.Discover(); err != nil {
		return nil, err
	}
	return loader, nil
}

func configuredSystemPrompt(cfg config.Config, memoryStore *store.Store, skillLoader *skills.Loader) (string, error) {
	parts := []string{strings.TrimSpace(defaultSystemPrompt)}
	loadedPrompt, err := loadSystemPromptText(cfg)
	if err != nil {
		return "", err
	}
	if loadedPrompt != "" {
		parts[0] = loadedPrompt
	}
	if memoryStore != nil {
		if memoryPrompt := strings.TrimSpace(memoryStore.BuildSystemPrompt()); memoryPrompt != "" {
			parts = append(parts, memoryPrompt)
		}
	}
	if skillLoader != nil {
		if metadata := strings.TrimSpace(skillLoader.MetadataPrompt()); metadata != "" {
			parts = append(parts, metadata)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

func loadSystemPromptText(cfg config.Config) (string, error) {
	path := strings.TrimSpace(cfg.Agent.SystemPromptPath)
	if path == "" {
		return "", nil
	}
	resolved := config.FindResourceFile(path)
	if resolved == "" {
		return "", nil
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func configuredTurnContext(cfg config.Config, prompt string, memoryStore *store.Store, skillLoader *skills.Loader) []schema.Message {
	contextMessages := []schema.Message{}
	if memoryStore != nil {
		contextMessages = append(contextMessages, memoryStore.BuildTurnContext(prompt, 5)...)
	}
	if skillLoader != nil && cfg.Tools.EnableAutoSkills {
		contextMessages = append(contextMessages, skillLoader.BuildTurnContext(prompt, cfg.Tools.AutoSkillsLimit)...)
	}
	return contextMessages
}

func configuredTools(cfg config.Config, workspace string, memoryStore *store.Store) []agenttools.Tool {
	toolList := make([]agenttools.Tool, 0, 10)
	if cfg.Tools.EnableFileTools {
		toolList = append(toolList,
			agenttools.NewReadTool(workspace),
			agenttools.NewWriteTool(workspace),
			agenttools.NewEditTool(workspace),
		)
	}
	if cfg.Tools.EnableBash {
		toolList = append(toolList,
			agenttools.NewBashTool(workspace),
			agenttools.NewBashOutputTool(),
			agenttools.NewBashKillTool(),
		)
	}
	if cfg.Tools.EnableNote {
		noteFile := defaultSessionNotesPath()
		toolList = append(toolList,
			agenttools.NewSessionNoteTool(noteFile),
			agenttools.NewRecallNoteTool(noteFile),
		)
	}
	if cfg.Tools.EnableMemory {
		memoryTools, _ := agenttools.CreateMemoryTools(memoryStore)
		toolList = append(toolList, memoryTools...)
	}
	return toolList
}

func normalizedMaxSteps(value int) int {
	if value <= 0 {
		return 20
	}
	return value
}

func defaultSessionNotesPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".agent-go", "session_notes.json")
	}
	return filepath.Join(home, ".agent-go", "session_notes.json")
}

func loadDefaultDotEnv() error {
	for _, candidate := range dotEnvCandidates() {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return loadDotEnvFile(candidate)
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func dotEnvCandidates() []string {
	seen := map[string]struct{}{}
	paths := make([]string, 0, 4)
	appendCandidate := func(path string) {
		resolved := config.ExpandPath(path)
		if resolved == "" {
			return
		}
		if _, ok := seen[resolved]; ok {
			return
		}
		seen[resolved] = struct{}{}
		paths = append(paths, resolved)
	}

	appendCandidate(".env")
	appendCandidate(filepath.Join("config", ".env"))
	if executable, err := os.Executable(); err == nil {
		resolved := executable
		if symlinkResolved, err := filepath.EvalSymlinks(executable); err == nil {
			resolved = symlinkResolved
		}
		exeDir := filepath.Dir(resolved)
		appendCandidate(filepath.Join(exeDir, ".env"))
		appendCandidate(filepath.Join(filepath.Dir(exeDir), ".env"))
	}
	return paths
}

func loadDotEnvFile(path string) error {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := parseDotEnvLine(line)
		if !ok {
			continue
		}
		if existing, exists := os.LookupEnv(key); exists && strings.TrimSpace(existing) != "" {
			continue
		}
		_ = os.Setenv(key, value)
	}
	return nil
}

func parseDotEnvLine(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	if strings.HasPrefix(trimmed, "export ") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
	}
	separator := strings.Index(trimmed, "=")
	if separator <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(trimmed[:separator])
	if key == "" {
		return "", "", false
	}
	value := strings.TrimSpace(trimmed[separator+1:])
	if len(value) >= 2 {
		if (value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"') {
			value = value[1 : len(value)-1]
		}
	}
	return key, value, true
}

func runInteractiveMode(stdout, stderr io.Writer, cfg config.Config) error {
	printBanner(stdout)
	printSessionInfo(stdout, cfg)
	_, _ = fmt.Fprintln(stdout, "Interactive runtime is not wired yet in this bootstrap build.")
	reader := bufio.NewReader(os.Stdin)
	for {
		_, _ = fmt.Fprint(stdout, "You › ")
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				_, _ = fmt.Fprintln(stdout)
				return nil
			}
			return err
		}
		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}
		switch {
		case input == "/help":
			printHelp(stdout, "")
		case input == "/clear":
			_, _ = fmt.Fprintln(stdout, "Session cleared (bootstrap placeholder).")
		case input == "/history":
			_, _ = fmt.Fprintln(stdout, "Message history is not available yet.")
		case input == "/stats":
			printStats(stdout, time.Now(), 1, 0, 0, 0, 0)
		case strings.HasPrefix(strings.ToLower(input), "/log"):
			parts := strings.Fields(input)
			if len(parts) == 1 {
				if err := showLogDirectory(stdout, stderr); err != nil {
					return err
				}
			} else {
				if err := readLogFile(stdout, stderr, parts[1]); err != nil {
					return err
				}
			}
		case input == "/exit" || input == "/quit" || input == "/q" || strings.EqualFold(input, "exit") || strings.EqualFold(input, "quit") || strings.EqualFold(input, "q"):
			_, _ = fmt.Fprintln(stdout, "Goodbye.")
			return nil
		default:
			_, _ = fmt.Fprintf(stdout, "Bootstrap build received: %s\n", input)
		}
	}
}

func printBanner(stdout io.Writer) {
	_, _ = fmt.Fprintln(stdout)
	_, _ = fmt.Fprintln(stdout, "╔══════════════════════════════════════════════════════════╗")
	_, _ = fmt.Fprintln(stdout, "║            Agent Go - Multi-turn Interactive Session     ║")
	_, _ = fmt.Fprintln(stdout, "╚══════════════════════════════════════════════════════════╝")
	_, _ = fmt.Fprintln(stdout)
}

func printSessionInfo(stdout io.Writer, cfg config.Config) {
	_, _ = fmt.Fprintln(stdout, "Session Info")
	_, _ = fmt.Fprintln(stdout, strings.Repeat("-", 40))
	_, _ = fmt.Fprintf(stdout, "Model: %s\n", cfg.LLM.Model)
	_, _ = fmt.Fprintf(stdout, "Workspace: %s\n", cfg.Agent.WorkspaceDir)
	_, _ = fmt.Fprintf(stdout, "System Prompt: %s\n", cfg.Agent.SystemPromptPath)
	_, _ = fmt.Fprintf(stdout, "Tools enabled: file=%t bash=%t note=%t memory=%t skills=%t\n", cfg.Tools.EnableFileTools, cfg.Tools.EnableBash, cfg.Tools.EnableNote, cfg.Tools.EnableMemory, cfg.Tools.EnableSkills)
	_, _ = fmt.Fprintln(stdout, strings.Repeat("-", 40))
}

func printStats(stdout io.Writer, start time.Time, totalMessages, userMessages, assistantMessages, toolMessages, apiTokens int) {
	duration := time.Since(start)
	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	seconds := int(duration.Seconds()) % 60
	_, _ = fmt.Fprintln(stdout, "Session Statistics")
	_, _ = fmt.Fprintln(stdout, strings.Repeat("-", 40))
	_, _ = fmt.Fprintf(stdout, "Session Duration: %02d:%02d:%02d\n", hours, minutes, seconds)
	_, _ = fmt.Fprintf(stdout, "Total Messages: %d\n", totalMessages)
	_, _ = fmt.Fprintf(stdout, "User Messages: %d\n", userMessages)
	_, _ = fmt.Fprintf(stdout, "Assistant Replies: %d\n", assistantMessages)
	_, _ = fmt.Fprintf(stdout, "Tool Calls: %d\n", toolMessages)
	if apiTokens > 0 {
		_, _ = fmt.Fprintf(stdout, "API Tokens Used: %d\n", apiTokens)
	}
	_, _ = fmt.Fprintln(stdout, strings.Repeat("-", 40))
}

func defaultLogDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".agent-go/log"
	}
	return filepath.Join(home, ".agent-go", "log")
}

func humanFileSize(size int64) string {
	if size < 1024 {
		return strconv.FormatInt(size, 10) + "B"
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(size)/1024.0)
	}
	if size < 1024*1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(size)/(1024.0*1024.0))
	}
	return fmt.Sprintf("%.1fGB", float64(size)/(1024.0*1024.0*1024.0))
}

func currentWorkspace(cfg config.Config) string {
	if strings.TrimSpace(cfg.Agent.WorkspaceDir) != "" {
		return cfg.Agent.WorkspaceDir
	}
	workspace, err := os.Getwd()
	if err != nil {
		return "."
	}
	return workspace
}

func displayName(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

func textWidth(text string) int {
	return utils.DisplayWidth(text)
}
