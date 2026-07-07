package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr         string
	DBPath       string
	FrontendDist string
	LLM          LLMConfig
	ConfigPath   string
}

type LLMConfig struct {
	BaseURL          string
	APIKey           string
	Model            string
	Timeout          time.Duration
	ReasoningEffort  string
	EnableWebSearch  bool
	WebSearchTool    string
	RequireWebSearch bool
}

type fileConfig struct {
	Addr         string        `json:"addr"`
	DBPath       string        `json:"dbPath"`
	FrontendDist string        `json:"frontendDist"`
	LLM          fileLLMConfig `json:"llm"`
}

type fileLLMConfig struct {
	BaseURL          string `json:"baseUrl"`
	APIKey           string `json:"apiKey"`
	Model            string `json:"model"`
	Timeout          string `json:"timeout"`
	ReasoningEffort  string `json:"reasoningEffort"`
	EnableWebSearch  *bool  `json:"enableWebSearch"`
	WebSearchTool    string `json:"webSearchTool"`
	RequireWebSearch *bool  `json:"requireWebSearch"`
}

func Load() Config {
	dotEnv := loadDotEnv()
	fileCfg, configPath := loadJSONConfig(dotEnv)
	addr := firstNonEmpty(os.Getenv("APP_ADDR"), fileCfg.Addr, dotEnv["APP_ADDR"], ":15566")
	dbPath := firstNonEmpty(os.Getenv("APP_DB_PATH"), fileCfg.DBPath, dotEnv["APP_DB_PATH"], "./data/app.db")
	frontendDist := firstNonEmpty(os.Getenv("FRONTEND_DIST"), fileCfg.FrontendDist, dotEnv["FRONTEND_DIST"], "../dist")
	llmBaseURL := strings.TrimRight(firstNonEmpty(os.Getenv("LLM_BASE_URL"), fileCfg.LLM.BaseURL, dotEnv["LLM_BASE_URL"], "https://api.openai.com"), "/")
	llmAPIKey := firstNonEmpty(os.Getenv("LLM_API_KEY"), fileCfg.LLM.APIKey, dotEnv["LLM_API_KEY"])
	llmModel := firstNonEmpty(os.Getenv("LLM_MODEL"), fileCfg.LLM.Model, dotEnv["LLM_MODEL"], "gpt-5.5")
	llmTimeout := durationValue(firstNonEmpty(os.Getenv("LLM_TIMEOUT"), fileCfg.LLM.Timeout, dotEnv["LLM_TIMEOUT"]), 600*time.Second)
	reasoningEffort := reasoningEffortValue(firstNonEmpty(os.Getenv("LLM_REASONING_EFFORT"), fileCfg.LLM.ReasoningEffort, dotEnv["LLM_REASONING_EFFORT"]), "xhigh")
	enableWebSearch := boolValue(firstNonEmpty(os.Getenv("LLM_ENABLE_WEB_SEARCH"), boolPtrString(fileCfg.LLM.EnableWebSearch), dotEnv["LLM_ENABLE_WEB_SEARCH"]), true)
	webSearchTool := webSearchToolValue(firstNonEmpty(os.Getenv("LLM_WEB_SEARCH_TOOL"), fileCfg.LLM.WebSearchTool, dotEnv["LLM_WEB_SEARCH_TOOL"]), "web_search")
	requireWebSearch := enableWebSearch && boolValue(firstNonEmpty(os.Getenv("LLM_REQUIRE_WEB_SEARCH"), boolPtrString(fileCfg.LLM.RequireWebSearch), dotEnv["LLM_REQUIRE_WEB_SEARCH"]), true)

	return Config{
		Addr:         addr,
		DBPath:       dbPath,
		FrontendDist: frontendDist,
		ConfigPath:   configPath,
		LLM: LLMConfig{
			BaseURL:          llmBaseURL,
			APIKey:           llmAPIKey,
			Model:            llmModel,
			Timeout:          llmTimeout,
			ReasoningEffort:  reasoningEffort,
			EnableWebSearch:  enableWebSearch,
			WebSearchTool:    webSearchTool,
			RequireWebSearch: requireWebSearch,
		},
	}
}

func loadJSONConfig(dotEnv map[string]string) (fileConfig, string) {
	for _, path := range candidateJSONConfigPaths(dotEnv) {
		if strings.TrimSpace(path) == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg fileConfig
		if err := json.Unmarshal(trimUTF8BOM(data), &cfg); err == nil {
			return cfg, path
		}
	}
	return fileConfig{}, ""
}

func trimUTF8BOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
}

func candidateJSONConfigPaths(dotEnv map[string]string) []string {
	paths := []string{
		firstNonEmpty(os.Getenv("APP_CONFIG_PATH"), dotEnv["APP_CONFIG_PATH"]),
		"config.json",
		filepath.Join("backend", "config.json"),
		filepath.Join("..", "config.json"),
		filepath.Join("..", "..", "config.json"),
		filepath.Join("..", "..", "..", "config.json"),
	}
	paths = append(paths, executableRelativeConfigPaths()...)
	paths = append(paths, projectRootConfigPaths()...)
	return uniqueNonEmptyPaths(paths)
}

func executableRelativeConfigPaths() []string {
	executable, err := os.Executable()
	if err != nil {
		return nil
	}
	dir := filepath.Dir(executable)
	return []string{
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "..", "config.json"),
		filepath.Join(dir, "..", "..", "config.json"),
		filepath.Join(dir, "..", "..", "..", "config.json"),
	}
}

func projectRootConfigPaths() []string {
	wd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var paths []string
	dir := wd
	for {
		if looksLikeProjectRoot(dir) {
			paths = append(paths, filepath.Join(dir, "config.json"))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return paths
}

func looksLikeProjectRoot(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "backend", "go.mod")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "config.example.json")); err == nil {
		return true
	}
	return false
}

func uniqueNonEmptyPaths(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		clean := filepath.Clean(path)
		key := strings.ToLower(clean)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, clean)
	}
	return out
}

func loadDotEnv() map[string]string {
	values := map[string]string{}
	for _, path := range candidateDotEnvPaths() {
		loadDotEnvFile(path, values)
	}
	return values
}

func candidateDotEnvPaths() []string {
	return []string{
		".env",
		filepath.Join("backend", ".env"),
		filepath.Join("..", ".env"),
		filepath.Join("..", "..", ".env"),
		filepath.Join("..", "..", "..", ".env"),
	}
}

func loadDotEnvFile(path string, values map[string]string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := values[key]; exists {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		values[key] = value
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func durationValue(value string, fallback time.Duration) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func reasoningEffortValue(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "none", "minimal", "low", "medium", "high", "xhigh":
		return value
	case "":
		return fallback
	default:
		return fallback
	}
}

func webSearchToolValue(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "web_search", "web_search_preview":
		return value
	case "":
		return fallback
	default:
		return fallback
	}
}

func boolValue(value string, fallback bool) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func boolPtrString(value *bool) string {
	if value == nil {
		return ""
	}
	if *value {
		return "true"
	}
	return "false"
}
