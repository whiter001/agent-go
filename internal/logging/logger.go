package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/whiter001/agent-go/internal/config"
	"github.com/whiter001/agent-go/internal/schema"
)

type Entry struct {
	Time    time.Time   `json:"time"`
	Type    string      `json:"type"`
	Payload any         `json:"payload"`
}

type Logger struct {
	dir  string
	file *os.File
	path string
	mu   sync.Mutex
}

func New(dir string) (*Logger, error) {
	resolved := config.ExpandPath(dir)
	if resolved == "" {
		resolved = defaultLogDir()
	}
	if err := os.MkdirAll(resolved, 0o755); err != nil {
		return nil, err
	}
	return &Logger{dir: resolved}, nil
}

func (l *Logger) StartRun() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}

	name := fmt.Sprintf("agent_run_%s.log", time.Now().Format("20060102_150405.000000000"))
	path := filepath.Join(l.dir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	l.file = file
	l.path = path
	return path, nil
}

func (l *Logger) Path() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

func (l *Logger) log(kind string, payload any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}
	entry := Entry{Time: time.Now().UTC(), Type: kind, Payload: payload}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = l.file.Write(append(data, '\n'))
	_ = l.file.Sync()
}

func (l *Logger) LogRequest(messages []schema.Message, tools []schema.ToolSpec) {
	l.log("request", map[string]any{
		"messages": messages,
		"tools":    tools,
	})
}

func (l *Logger) LogResponse(response schema.LLMResponse) {
	l.log("response", response)
}

func (l *Logger) LogToolResult(toolName string, arguments map[string]any, success bool, content string, errText string) {
	l.log("tool_result", map[string]any{
		"tool_name": toolName,
		"arguments": arguments,
		"success":   success,
		"content":   content,
		"error":     errText,
	})
}

func (l *Logger) Dir() string {
	return l.dir
}

func ListLogFiles(dir string) ([]os.FileInfo, error) {
	resolved := config.ExpandPath(dir)
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, err
	}
	files := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".log" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, info)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime().After(files[j].ModTime())
	})
	return files, nil
}

func ReadFile(path string) (string, error) {
	data, err := os.ReadFile(config.ExpandPath(path))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func defaultLogDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".agent-go/log"
	}
	return filepath.Join(home, ".agent-go", "log")
}
