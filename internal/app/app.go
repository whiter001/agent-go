package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/whiter001/agent-go/internal/config"
	"github.com/whiter001/agent-go/internal/utils"
)

const Version = "0.1.0"

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
	_, _ = fmt.Fprintln(stdout, "Prompt mode is not wired yet in this bootstrap build.")
	_, _ = fmt.Fprintf(stdout, "Prompt: %s\n", prompt)
	_, _ = fmt.Fprintf(stdout, "Workspace: %s\n", cfg.Agent.WorkspaceDir)
	if strings.TrimSpace(cfg.LLM.APIKey) == "" {
		_, _ = fmt.Fprintln(stdout, "Missing API key. Set AGENT_GO_API_KEY or a config file before running the agent.")
	}
	return nil
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
