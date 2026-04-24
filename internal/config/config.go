package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type RetryConfig struct {
	Enabled         bool    `json:"enabled"`
	MaxRetries      int     `json:"max_retries"`
	InitialDelay    float64 `json:"initial_delay"`
	MaxDelay        float64 `json:"max_delay"`
	ExponentialBase float64 `json:"exponential_base"`
}

type LLMConfig struct {
	APIKey   string      `json:"api_key"`
	APIBase  string      `json:"api_base"`
	Model    string      `json:"model"`
	Provider string      `json:"provider"`
	Retry    RetryConfig `json:"retry"`
}

type AgentConfig struct {
	MaxSteps         int    `json:"max_steps"`
	WorkspaceDir     string `json:"workspace_dir"`
	SystemPromptPath string `json:"system_prompt_path"`
}

type MCPConfig struct {
	ConnectTimeout float64 `json:"connect_timeout"`
	ExecuteTimeout float64 `json:"execute_timeout"`
	SSEReadTimeout float64 `json:"sse_read_timeout"`
}

type ToolsConfig struct {
	EnableFileTools         bool      `json:"enable_file_tools"`
	EnableBash              bool      `json:"enable_bash"`
	EnableNote              bool      `json:"enable_note"`
	EnableSkills            bool      `json:"enable_skills"`
	EnableAutoSkills        bool      `json:"enable_auto_skills"`
	AutoSkillsLimit         int       `json:"auto_skills_limit"`
	EnableMemory            bool      `json:"enable_memory"`
	EnableAutoSkillCreation bool      `json:"enable_auto_skill_creation"`
	AutoSkillMinToolCalls   int       `json:"auto_skill_min_tool_calls"`
	AutoSkillDir            string    `json:"auto_skill_dir"`
	SkillsExternalDirs      []string  `json:"skills_external_dirs"`
	SkillsDir               string    `json:"skills_dir"`
	EnableMCP               bool      `json:"enable_mcp"`
	MCPConfigPath           string    `json:"mcp_config_path"`
	MCP                     MCPConfig `json:"mcp"`
}

type Config struct {
	LLM   LLMConfig   `json:"llm"`
	Agent AgentConfig `json:"agent"`
	Tools ToolsConfig `json:"tools"`
}

func Default() Config {
	return Config{
		LLM: LLMConfig{
			APIBase:  "https://api.minimaxi.com",
			Model:    "MiniMax-M2.7",
			Provider: "anthropic",
			Retry: RetryConfig{
				Enabled:         true,
				MaxRetries:      3,
				InitialDelay:    1.0,
				MaxDelay:        60.0,
				ExponentialBase: 2.0,
			},
		},
		Agent: AgentConfig{
			MaxSteps:         100,
			WorkspaceDir:     "./workspace",
			SystemPromptPath: "system_prompt.md",
		},
		Tools: ToolsConfig{
			EnableFileTools:         true,
			EnableBash:              true,
			EnableNote:              true,
			EnableSkills:            true,
			EnableAutoSkills:        true,
			AutoSkillsLimit:         2,
			EnableMemory:            true,
			EnableAutoSkillCreation: true,
			AutoSkillMinToolCalls:   5,
			AutoSkillDir:            "~/.agent-go/skills",
			SkillsExternalDirs:      []string{"~/.agent-go/skills"},
			SkillsDir:               "./skills",
			EnableMCP:               false,
			MCPConfigPath:           "mcp.json",
			MCP: MCPConfig{
				ConnectTimeout: 10.0,
				ExecuteTimeout: 60.0,
				SSEReadTimeout: 120.0,
			},
		},
	}
}

func Load() (Config, error) {
	cfg := Default()
	if path := FindConfigFile(); path != "" {
		loaded, err := LoadFromFile(path)
		if err != nil {
			return Config{}, err
		}
		cfg = merge(cfg, loaded)
	}
	cfg.ApplyEnv()
	cfg.Normalize()
	return cfg, nil
}

func LoadFromFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	parsed, err := decodeConfigData(path, data)
	if err != nil {
		return Config{}, err
	}
	return fromMap(parsed), nil
}

func FindConfigFile() string {
	if explicit := strings.TrimSpace(os.Getenv("AGENT_GO_CONFIG")); explicit != "" {
		if fileExists(explicit) {
			return explicit
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		for _, candidate := range []string{
			filepath.Join(cwd, "config.json"),
			filepath.Join(cwd, "config.yaml"),
			filepath.Join(cwd, "config.yml"),
			filepath.Join(cwd, "agent-go.json"),
			filepath.Join(cwd, "agent-go.yaml"),
			filepath.Join(cwd, "agent-go.yml"),
			filepath.Join(cwd, "config", "config.json"),
			filepath.Join(cwd, "config", "config.yaml"),
			filepath.Join(cwd, "config", "config.yml"),
		} {
			if fileExists(candidate) {
				return candidate
			}
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, candidate := range []string{
		filepath.Join(home, ".agent-go", "config.json"),
		filepath.Join(home, ".agent-go", "config.yaml"),
		filepath.Join(home, ".agent-go", "config.yml"),
	} {
		if fileExists(candidate) {
			return candidate
		}
	}

	return ""
}

func FindResourceFile(filename string) string {
	if fileExists(filename) {
		return filename
	}
	if cwd, err := os.Getwd(); err == nil {
		for _, candidate := range []string{filepath.Join(cwd, filename), filepath.Join(cwd, "config", filename)} {
			if fileExists(candidate) {
				return candidate
			}
		}
	}
	home, err := os.UserHomeDir()
	if err == nil {
		for _, candidate := range []string{filepath.Join(home, ".agent-go", filename), filepath.Join(home, ".agent-go", "config", filename)} {
			if fileExists(candidate) {
				return candidate
			}
		}
	}
	return ""
}

func (c *Config) Normalize() {
	c.LLM.APIBase = strings.TrimRight(strings.TrimSpace(c.LLM.APIBase), "/")
	c.LLM.Provider = strings.ToLower(strings.TrimSpace(c.LLM.Provider))
	c.Agent.WorkspaceDir = ExpandPath(c.Agent.WorkspaceDir)
	c.Agent.SystemPromptPath = ExpandPath(c.Agent.SystemPromptPath)
	c.Tools.AutoSkillDir = ExpandPath(c.Tools.AutoSkillDir)
	c.Tools.SkillsDir = ExpandPath(c.Tools.SkillsDir)
	for i, dir := range c.Tools.SkillsExternalDirs {
		c.Tools.SkillsExternalDirs[i] = ExpandPath(dir)
	}
	c.Tools.MCPConfigPath = ExpandPath(c.Tools.MCPConfigPath)
	if c.LLM.Provider == "" {
		c.LLM.Provider = "anthropic"
	}
}

func (c *Config) ApplyEnv() {
	if value := envAny("AGENT_GO_API_KEY", "MINI_AGENT_API_KEY", "API_KEY"); value != "" {
		c.LLM.APIKey = value
	}
	if value := envAny("AGENT_GO_API_BASE", "MINI_AGENT_API_BASE", "API_BASE"); value != "" {
		c.LLM.APIBase = value
	}
	if value := envAny("AGENT_GO_MODEL", "MINI_AGENT_MODEL", "MODEL"); value != "" {
		c.LLM.Model = value
	}
	if value := envAny("AGENT_GO_PROVIDER", "MINI_AGENT_PROVIDER", "PROVIDER"); value != "" {
		c.LLM.Provider = value
	}
	if value := envAny("AGENT_GO_WORKSPACE_DIR", "MINI_AGENT_WORKSPACE_DIR", "WORKSPACE_DIR"); value != "" {
		c.Agent.WorkspaceDir = value
	}
	if value := envAny("AGENT_GO_MAX_STEPS", "MINI_AGENT_MAX_STEPS", "MAX_STEPS"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.Agent.MaxSteps = parsed
		}
	}
	if value := envAny("AGENT_GO_SYSTEM_PROMPT_PATH", "MINI_AGENT_SYSTEM_PROMPT_PATH", "SYSTEM_PROMPT_PATH"); value != "" {
		c.Agent.SystemPromptPath = value
	}
	if value := envAny("AGENT_GO_ENABLE_FILE_TOOLS", "MINI_AGENT_ENABLE_FILE_TOOLS"); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			c.Tools.EnableFileTools = parsed
		}
	}
	if value := envAny("AGENT_GO_ENABLE_BASH", "MINI_AGENT_ENABLE_BASH"); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			c.Tools.EnableBash = parsed
		}
	}
	if value := envAny("AGENT_GO_ENABLE_NOTE", "MINI_AGENT_ENABLE_NOTE"); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			c.Tools.EnableNote = parsed
		}
	}
	if value := envAny("AGENT_GO_ENABLE_MEMORY", "MINI_AGENT_ENABLE_MEMORY"); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			c.Tools.EnableMemory = parsed
		}
	}
	if value := envAny("AGENT_GO_ENABLE_SKILLS", "MINI_AGENT_ENABLE_SKILLS"); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			c.Tools.EnableSkills = parsed
		}
	}
	if value := envAny("AGENT_GO_SKILLS_DIR", "MINI_AGENT_SKILLS_DIR"); value != "" {
		c.Tools.SkillsDir = value
	}
	if value := envAny("AGENT_GO_AUTO_SKILL_DIR", "MINI_AGENT_AUTO_SKILL_DIR"); value != "" {
		c.Tools.AutoSkillDir = value
	}
}

func ExpandPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			if trimmed == "~" {
				return home
			}
			if strings.HasPrefix(trimmed, "~/") {
				return filepath.Join(home, trimmed[2:])
			}
		}
	}
	return filepath.Clean(trimmed)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func envAny(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func decodeConfigData(path string, data []byte) (map[string]any, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".json" {
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, err
		}
		return parsed, nil
	}
	return parseYAML(string(data))
}

func parseYAML(content string) (map[string]any, error) {
	lines := strings.Split(content, "\n")
	index := 0
	value, err := parseYAMLBlock(lines, &index, 0)
	if err != nil {
		return nil, err
	}
	result, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("yaml root must be a map")
	}
	return result, nil
}

func parseYAMLBlock(lines []string, index *int, indent int) (any, error) {
	var object map[string]any
	var list []any
	mode := ""

	for *index < len(lines) {
		line := lines[*index]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			*index = *index + 1
			continue
		}
		currentIndent := countIndent(line)
		if currentIndent < indent {
			break
		}
		if currentIndent > indent {
			break
		}
		if strings.HasPrefix(trimmed, "- ") {
			if mode == "" {
				mode = "list"
				list = []any{}
			} else if mode != "list" {
				return nil, fmt.Errorf("mixed list and map at indent %d", indent)
			}
			item := strings.TrimSpace(trimmed[2:])
			*index = *index + 1
			if item == "" {
				child, err := parseYAMLBlock(lines, index, indent+2)
				if err != nil {
					return nil, err
				}
				list = append(list, child)
				continue
			}
			if key, value, ok := splitKeyValue(item); ok {
				entry := map[string]any{}
				if value == "" {
					child, err := parseYAMLBlock(lines, index, indent+2)
					if err != nil {
						return nil, err
					}
					entry[key] = child
				} else {
					entry[key] = parseScalar(value)
				}
				list = append(list, entry)
				continue
			}
			list = append(list, parseScalar(item))
			continue
		}

		if mode == "" {
			mode = "map"
			object = map[string]any{}
		} else if mode != "map" {
			break
		}

		key, value, ok := splitKeyValue(trimmed)
		if !ok {
			return nil, fmt.Errorf("invalid yaml line: %q", line)
		}
		*index = *index + 1
		if value == "" {
			child, err := parseYAMLBlock(lines, index, indent+2)
			if err != nil {
				return nil, err
			}
			object[key] = child
		} else {
			object[key] = parseScalar(value)
		}
	}

	if mode == "list" {
		return list, nil
	}
	if object == nil {
		object = map[string]any{}
	}
	return object, nil
}

func countIndent(line string) int {
	count := 0
	for _, r := range line {
		switch r {
		case ' ':
			count++
		case '\t':
			count += 2
		default:
			return count
		}
	}
	return count
}

func splitKeyValue(line string) (string, string, bool) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key := strings.TrimSpace(parts[0])
	if key == "" {
		return "", "", false
	}
	value := strings.TrimSpace(parts[1])
	return key, value, true
}

func parseScalar(value string) any {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.Trim(trimmed, `"'`)
	if trimmed == "" {
		return ""
	}
	if parsed, err := strconv.ParseBool(trimmed); err == nil {
		return parsed
	}
	if strings.ContainsAny(trimmed, ".eE") {
		if parsed, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return parsed
		}
	}
	if parsed, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return int(parsed)
	}
	return trimmed
}

func fromMap(data map[string]any) Config {
	cfg := Default()
	mergeTopLevelLLM(&cfg.LLM, data)
	mergeTopLevelAgent(&cfg.Agent, data)
	mergeTopLevelTools(&cfg.Tools, data)
	if nested, ok := asMap(data["llm"]); ok {
		mergeTopLevelLLM(&cfg.LLM, nested)
	}
	if nested, ok := asMap(data["agent"]); ok {
		mergeTopLevelAgent(&cfg.Agent, nested)
	}
	if nested, ok := asMap(data["tools"]); ok {
		mergeTopLevelTools(&cfg.Tools, nested)
	}
	return cfg
}

func merge(base Config, loaded Config) Config {
	if loaded.LLM.APIKey != "" {
		base.LLM.APIKey = loaded.LLM.APIKey
	}
	if loaded.LLM.APIBase != "" {
		base.LLM.APIBase = loaded.LLM.APIBase
	}
	if loaded.LLM.Model != "" {
		base.LLM.Model = loaded.LLM.Model
	}
	if loaded.LLM.Provider != "" {
		base.LLM.Provider = loaded.LLM.Provider
	}
	if loaded.LLM.Retry != (RetryConfig{}) {
		base.LLM.Retry = loaded.LLM.Retry
	}
	if loaded.Agent.MaxSteps != 0 {
		base.Agent.MaxSteps = loaded.Agent.MaxSteps
	}
	if loaded.Agent.WorkspaceDir != "" {
		base.Agent.WorkspaceDir = loaded.Agent.WorkspaceDir
	}
	if loaded.Agent.SystemPromptPath != "" {
		base.Agent.SystemPromptPath = loaded.Agent.SystemPromptPath
	}
	base.Tools = mergeTools(base.Tools, loaded.Tools)
	return base
}

func mergeTools(base ToolsConfig, loaded ToolsConfig) ToolsConfig {
	if loaded.EnableFileTools != base.EnableFileTools {
		base.EnableFileTools = loaded.EnableFileTools
	}
	if loaded.EnableBash != base.EnableBash {
		base.EnableBash = loaded.EnableBash
	}
	if loaded.EnableNote != base.EnableNote {
		base.EnableNote = loaded.EnableNote
	}
	if loaded.EnableSkills != base.EnableSkills {
		base.EnableSkills = loaded.EnableSkills
	}
	if loaded.EnableAutoSkills != base.EnableAutoSkills {
		base.EnableAutoSkills = loaded.EnableAutoSkills
	}
	if loaded.AutoSkillsLimit != 0 {
		base.AutoSkillsLimit = loaded.AutoSkillsLimit
	}
	if loaded.EnableMemory != base.EnableMemory {
		base.EnableMemory = loaded.EnableMemory
	}
	if loaded.EnableAutoSkillCreation != base.EnableAutoSkillCreation {
		base.EnableAutoSkillCreation = loaded.EnableAutoSkillCreation
	}
	if loaded.AutoSkillMinToolCalls != 0 {
		base.AutoSkillMinToolCalls = loaded.AutoSkillMinToolCalls
	}
	if loaded.AutoSkillDir != "" {
		base.AutoSkillDir = loaded.AutoSkillDir
	}
	if len(loaded.SkillsExternalDirs) != 0 {
		base.SkillsExternalDirs = append([]string{}, loaded.SkillsExternalDirs...)
	}
	if loaded.SkillsDir != "" {
		base.SkillsDir = loaded.SkillsDir
	}
	if loaded.EnableMCP != base.EnableMCP {
		base.EnableMCP = loaded.EnableMCP
	}
	if loaded.MCPConfigPath != "" {
		base.MCPConfigPath = loaded.MCPConfigPath
	}
	if loaded.MCP != (MCPConfig{}) {
		base.MCP = loaded.MCP
	}
	return base
}

func mergeTopLevelLLM(target *LLMConfig, data map[string]any) {
	if value, ok := stringValue(data, "api_key"); ok {
		target.APIKey = value
	}
	if value, ok := stringValue(data, "api_base"); ok {
		target.APIBase = value
	}
	if value, ok := stringValue(data, "model"); ok {
		target.Model = value
	}
	if value, ok := stringValue(data, "provider"); ok {
		target.Provider = value
	}
	if nested, ok := asMap(data["retry"]); ok {
		if value, ok := boolValue(nested, "enabled"); ok {
			target.Retry.Enabled = value
		}
		if value, ok := intValue(nested, "max_retries"); ok {
			target.Retry.MaxRetries = value
		}
		if value, ok := floatValue(nested, "initial_delay"); ok {
			target.Retry.InitialDelay = value
		}
		if value, ok := floatValue(nested, "max_delay"); ok {
			target.Retry.MaxDelay = value
		}
		if value, ok := floatValue(nested, "exponential_base"); ok {
			target.Retry.ExponentialBase = value
		}
	}
}

func mergeTopLevelAgent(target *AgentConfig, data map[string]any) {
	if value, ok := intValue(data, "max_steps"); ok {
		target.MaxSteps = value
	}
	if value, ok := stringValue(data, "workspace_dir"); ok {
		target.WorkspaceDir = value
	}
	if value, ok := stringValue(data, "system_prompt_path"); ok {
		target.SystemPromptPath = value
	}
}

func mergeTopLevelTools(target *ToolsConfig, data map[string]any) {
	if value, ok := boolValue(data, "enable_file_tools"); ok {
		target.EnableFileTools = value
	}
	if value, ok := boolValue(data, "enable_bash"); ok {
		target.EnableBash = value
	}
	if value, ok := boolValue(data, "enable_note"); ok {
		target.EnableNote = value
	}
	if value, ok := boolValue(data, "enable_skills"); ok {
		target.EnableSkills = value
	}
	if value, ok := boolValue(data, "enable_auto_skills"); ok {
		target.EnableAutoSkills = value
	}
	if value, ok := intValue(data, "auto_skills_limit"); ok {
		target.AutoSkillsLimit = value
	}
	if value, ok := boolValue(data, "enable_memory"); ok {
		target.EnableMemory = value
	}
	if value, ok := boolValue(data, "enable_auto_skill_creation"); ok {
		target.EnableAutoSkillCreation = value
	}
	if value, ok := intValue(data, "auto_skill_min_tool_calls"); ok {
		target.AutoSkillMinToolCalls = value
	}
	if value, ok := stringValue(data, "auto_skill_dir"); ok {
		target.AutoSkillDir = value
	}
	if value, ok := stringSliceValue(data, "skills_external_dirs"); ok {
		target.SkillsExternalDirs = value
	}
	if value, ok := stringValue(data, "skills_dir"); ok {
		target.SkillsDir = value
	}
	if value, ok := boolValue(data, "enable_mcp"); ok {
		target.EnableMCP = value
	}
	if value, ok := stringValue(data, "mcp_config_path"); ok {
		target.MCPConfigPath = value
	}
	if nested, ok := asMap(data["mcp"]); ok {
		if value, ok := floatValue(nested, "connect_timeout"); ok {
			target.MCP.ConnectTimeout = value
		}
		if value, ok := floatValue(nested, "execute_timeout"); ok {
			target.MCP.ExecuteTimeout = value
		}
		if value, ok := floatValue(nested, "sse_read_timeout"); ok {
			target.MCP.SSEReadTimeout = value
		}
	}
}

func asMap(value any) (map[string]any, bool) {
	if value == nil {
		return nil, false
	}
	result, ok := value.(map[string]any)
	return result, ok
}

func stringValue(data map[string]any, key string) (string, bool) {
	value, ok := data[key]
	if !ok {
		return "", false
	}
	switch typed := value.(type) {
	case string:
		return typed, true
	case fmt.Stringer:
		return typed.String(), true
	default:
		return fmt.Sprint(typed), true
	}
}

func boolValue(data map[string]any, key string) (bool, bool) {
	value, ok := data[key]
	if !ok {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		parsed, err := strconv.ParseBool(typed)
		return parsed, err == nil
	case int:
		return typed != 0, true
	case int64:
		return typed != 0, true
	case float64:
		return typed != 0, true
	default:
		return false, false
	}
}

func intValue(data map[string]any, key string) (int, bool) {
	value, ok := data[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(typed)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func floatValue(data map[string]any, key string) (float64, bool) {
	value, ok := data[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func stringSliceValue(data map[string]any, key string) ([]string, bool) {
	value, ok := data[key]
	if !ok {
		return nil, false
	}
	switch typed := value.(type) {
	case []string:
		return append([]string{}, typed...), true
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			items = append(items, fmt.Sprint(item))
		}
		return items, true
	default:
		return []string{fmt.Sprint(typed)}, true
	}
}
