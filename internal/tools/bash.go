package tools

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/whiter001/agent-go/internal/config"
	"github.com/whiter001/agent-go/internal/schema"
)

type BashTool struct {
	workspaceDir string
	defaultTimeout time.Duration
	maxTimeout    time.Duration
}

func NewBashTool(workspaceDir string) *BashTool {
	return &BashTool{
		workspaceDir:   workspaceDir,
		defaultTimeout: 120 * time.Second,
		maxTimeout:     600 * time.Second,
	}
}

func (t *BashTool) Spec() schema.ToolSpec {
	return Spec("bash", "Execute shell commands in foreground or background. Use for terminal operations like git, npm, docker, etc. Do not use for file operations.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "description": "Command to execute"},
			"timeout": map[string]any{"type": "integer", "description": "Timeout in seconds for foreground commands"},
			"run_in_background": map[string]any{"type": "boolean", "description": "Set true for long-running commands"},
		},
		"required": []string{"command"},
	})
}

func (t *BashTool) Execute(ctx context.Context, args map[string]any) Result {
	command := stringArg(args, "command")
	if command == "" {
		return Result{Error: "command is required"}
	}
	runInBackground := boolArg(args, "run_in_background")
	timeoutSeconds := intArg(args, "timeout")
	if timeoutSeconds <= 0 {
		timeoutSeconds = int(t.defaultTimeout.Seconds())
	}
	if time.Duration(timeoutSeconds)*time.Second > t.maxTimeout {
		timeoutSeconds = int(t.maxTimeout.Seconds())
	}

	workingDir := config.ExpandPath(t.workspaceDir)
	if workingDir == "" {
		workingDir = "."
	}

	if runInBackground {
		bashID, err := bashBackgroundManager.start(ctx, command, workingDir)
		if err != nil {
			return Result{Error: err.Error()}
		}
		return Result{Success: true, Content: fmt.Sprintf("Started background shell: %s", bashID)}
	}

	commandCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	stdout, stderr, exitCode, err := runForeground(commandCtx, command, workingDir)
	content := formatShellResult(stdout, stderr, exitCode, "")
	if err != nil && exitCode == 0 {
		return Result{Error: err.Error(), Content: content}
	}
	if exitCode != 0 {
		return Result{Success: false, Content: content, Error: fmt.Sprintf("command exited with code %d", exitCode)}
	}
	return Result{Success: true, Content: content}
}

type BashOutputTool struct{}

func NewBashOutputTool() *BashOutputTool { return &BashOutputTool{} }

func (t *BashOutputTool) Spec() schema.ToolSpec {
	return Spec("bash_output", "Get output from a background bash process.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"bash_id": map[string]any{"type": "string", "description": "Background shell process ID"},
			"filter_pattern": map[string]any{"type": "string", "description": "Optional regex filter for returned output"},
		},
		"required": []string{"bash_id"},
	})
}

func (t *BashOutputTool) Execute(ctx context.Context, args map[string]any) Result {
	bashID := stringArg(args, "bash_id")
	if bashID == "" {
		return Result{Error: "bash_id is required"}
	}
	filterPattern := stringArg(args, "filter_pattern")
	process := bashBackgroundManager.get(bashID)
	if process == nil {
		return Result{Error: fmt.Sprintf("background shell not found: %s", bashID)}
	}
	output := process.readNewOutput(filterPattern)
	content := process.statusSummary()
	if len(output) > 0 {
		content += "\n" + strings.Join(output, "\n")
	}
	return Result{Success: true, Content: content}
}

type BashKillTool struct{}

func NewBashKillTool() *BashKillTool { return &BashKillTool{} }

func (t *BashKillTool) Spec() schema.ToolSpec {
	return Spec("bash_kill", "Terminate a background bash process.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"bash_id": map[string]any{"type": "string", "description": "Background shell process ID"},
		},
		"required": []string{"bash_id"},
	})
}

func (t *BashKillTool) Execute(ctx context.Context, args map[string]any) Result {
	bashID := stringArg(args, "bash_id")
	if bashID == "" {
		return Result{Error: "bash_id is required"}
	}
	process, err := bashBackgroundManager.terminate(bashID)
	if err != nil {
		return Result{Error: err.Error()}
	}
	return Result{Success: true, Content: process.statusSummary()}
}

type backgroundProcess struct {
	id        string
	command   string
	cmd       *exec.Cmd
	startTime time.Time
	mu        sync.Mutex
	lines     []string
	lastRead  int
	status    string
	exitCode  int
	finished  bool
}

func (p *backgroundProcess) appendLine(prefix, line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if prefix != "" {
		p.lines = append(p.lines, fmt.Sprintf("[%s] %s", prefix, line))
		return
	}
	p.lines = append(p.lines, line)
}

func (p *backgroundProcess) readNewOutput(filterPattern string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	newLines := append([]string(nil), p.lines[p.lastRead:]...)
	p.lastRead = len(p.lines)
	if filterPattern == "" {
		return newLines
	}
	pattern, err := regexp.Compile(filterPattern)
	if err != nil {
		return newLines
	}
	filtered := make([]string, 0, len(newLines))
	for _, line := range newLines {
		if pattern.MatchString(line) {
			filtered = append(filtered, line)
		}
	}
	return filtered
}

func (p *backgroundProcess) markFinished(exitCode int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.finished = true
	p.exitCode = exitCode
	if exitCode == 0 {
		p.status = "completed"
	} else {
		p.status = "failed"
	}
}

func (p *backgroundProcess) terminate() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return errors.New("background process not started")
	}
	if err := p.cmd.Process.Kill(); err != nil {
		return err
	}
	p.markFinished(-1)
	p.mu.Lock()
	p.status = "terminated"
	p.mu.Unlock()
	return nil
}

func (p *backgroundProcess) statusSummary() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	content := []string{
		fmt.Sprintf("bash_id: %s", p.id),
		fmt.Sprintf("command: %s", p.command),
		fmt.Sprintf("status: %s", p.status),
	}
	if p.finished {
		content = append(content, fmt.Sprintf("exit_code: %d", p.exitCode))
	}
	return strings.Join(content, "\n")
}

type backgroundManager struct {
	mu       sync.Mutex
	processes map[string]*backgroundProcess
	counter  int64
}

var bashBackgroundManager = &backgroundManager{processes: map[string]*backgroundProcess{}}

func (m *backgroundManager) start(ctx context.Context, command string, workingDir string) (string, error) {
	cmd := shellCommand(ctx, command)
	cmd.Dir = workingDir

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	m.counter++
	id := fmt.Sprintf("bash-%d-%d", time.Now().UnixNano(), m.counter)
	process := &backgroundProcess{id: id, command: command, cmd: cmd, startTime: time.Now(), status: "running"}
	m.processes[id] = process
	m.mu.Unlock()

	if err := cmd.Start(); err != nil {
		m.mu.Lock()
		delete(m.processes, id)
		m.mu.Unlock()
		return "", err
	}

	go streamPipe(process, stdoutPipe, "")
	go streamPipe(process, stderrPipe, "stderr")
	go func() {
		err := cmd.Wait()
		exitCode := exitCodeFromError(err, cmd)
		process.markFinished(exitCode)
	}()

	return id, nil
}

func (m *backgroundManager) get(id string) *backgroundProcess {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.processes[id]
}

func (m *backgroundManager) terminate(id string) (*backgroundProcess, error) {
	m.mu.Lock()
	process := m.processes[id]
	m.mu.Unlock()
	if process == nil {
		return nil, fmt.Errorf("background shell not found: %s", id)
	}
	if err := process.terminate(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	delete(m.processes, id)
	m.mu.Unlock()
	return process, nil
}

func streamPipe(process *backgroundProcess, reader io.ReadCloser, prefix string) {
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	for scanner.Scan() {
		process.appendLine(prefix, scanner.Text())
	}
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command)
	}
	if pathExists("/bin/bash") {
		return exec.CommandContext(ctx, "/bin/bash", "-lc", command)
	}
	return exec.CommandContext(ctx, "/bin/sh", "-lc", command)
}

func runForeground(ctx context.Context, command string, workingDir string) (string, string, int, error) {
	cmd := shellCommand(ctx, command)
	cmd.Dir = workingDir
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	exitCode := exitCodeFromError(err, cmd)
	return stdoutBuf.String(), stderrBuf.String(), exitCode, err
}

func exitCodeFromError(err error, cmd *exec.Cmd) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	if cmd != nil && cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	return -1
}

func formatShellResult(stdoutText, stderrText string, exitCode int, bashID string) string {
	parts := []string{}
	if stdoutText != "" {
		parts = append(parts, stdoutText)
	}
	if stderrText != "" {
		parts = append(parts, "[stderr]:\n"+stderrText)
	}
	if bashID != "" {
		parts = append(parts, "[bash_id]:\n"+bashID)
	}
	parts = append(parts, fmt.Sprintf("[exit_code]:\n%d", exitCode))
	if len(parts) == 0 {
		return "(no output)"
	}
	return strings.Join(parts, "\n")
}

func boolArg(args map[string]any, key string) bool {
	value, ok := args[key]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(typed)
		return parsed
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	default:
		return false
	}
}

func pathExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
