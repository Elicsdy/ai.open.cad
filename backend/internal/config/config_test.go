package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	withTempCWD(t)
	t.Setenv("APP_CONFIG_PATH", "")
	t.Setenv("APP_ADDR", "")
	t.Setenv("APP_DB_PATH", "")
	t.Setenv("FRONTEND_DIST", "")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("LLM_MODEL", "")
	t.Setenv("LLM_TIMEOUT", "")

	cfg := Load()
	if cfg.Addr != ":15566" {
		t.Fatalf("unexpected addr: %s", cfg.Addr)
	}
	if cfg.DBPath == "" {
		t.Fatal("expected default db path")
	}
	if cfg.LLM.BaseURL != "https://api.openai.com" {
		t.Fatalf("unexpected base url: %s", cfg.LLM.BaseURL)
	}
	if cfg.LLM.Model == "" {
		t.Fatal("expected default model")
	}
	if cfg.LLM.Timeout != 600*time.Second {
		t.Fatalf("unexpected timeout: %s", cfg.LLM.Timeout)
	}
	if cfg.LLM.ReasoningEffort != "xhigh" {
		t.Fatalf("unexpected reasoning effort: %s", cfg.LLM.ReasoningEffort)
	}
	if !cfg.LLM.EnableWebSearch {
		t.Fatal("expected web search to be enabled by default")
	}
	if cfg.LLM.WebSearchTool != "web_search" {
		t.Fatalf("unexpected web search tool: %s", cfg.LLM.WebSearchTool)
	}
	if !cfg.LLM.RequireWebSearch {
		t.Fatal("expected web search to be required by default")
	}
}

func withTempCWD(t *testing.T) {
	t.Helper()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	t.Cleanup(func() {
		_ = os.Chdir(oldWd)
	})
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReadsJSONConfig(t *testing.T) {
	withTempCWD(t)
	if err := os.WriteFile("config.json", []byte(`{
  "addr": ":9090",
  "dbPath": "./custom.db",
  "frontendDist": "./web",
  "llm": {
    "baseUrl": "https://api.example.com",
    "apiKey": "test-key",
    "model": "test-model",
    "timeout": "45s",
    "reasoningEffort": "high",
    "enableWebSearch": false,
    "webSearchTool": "web_search_preview",
    "requireWebSearch": false
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("APP_CONFIG_PATH", "")
	t.Setenv("APP_ADDR", "")
	t.Setenv("APP_DB_PATH", "")
	t.Setenv("FRONTEND_DIST", "")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("LLM_MODEL", "")
	t.Setenv("LLM_TIMEOUT", "")

	cfg := Load()
	if cfg.Addr != ":9090" {
		t.Fatalf("unexpected addr: %s", cfg.Addr)
	}
	if cfg.DBPath != "./custom.db" {
		t.Fatalf("unexpected db path: %s", cfg.DBPath)
	}
	if cfg.FrontendDist != "./web" {
		t.Fatalf("unexpected frontend dist: %s", cfg.FrontendDist)
	}
	if cfg.LLM.BaseURL != "https://api.example.com" {
		t.Fatalf("unexpected base url: %s", cfg.LLM.BaseURL)
	}
	if cfg.LLM.APIKey != "test-key" {
		t.Fatalf("unexpected api key: %s", cfg.LLM.APIKey)
	}
	if cfg.LLM.Model != "test-model" {
		t.Fatalf("unexpected model: %s", cfg.LLM.Model)
	}
	if cfg.LLM.Timeout != 45*time.Second {
		t.Fatalf("unexpected timeout: %s", cfg.LLM.Timeout)
	}
	if cfg.LLM.ReasoningEffort != "high" {
		t.Fatalf("unexpected reasoning effort: %s", cfg.LLM.ReasoningEffort)
	}
	if cfg.LLM.EnableWebSearch {
		t.Fatal("expected web search to be disabled")
	}
	if cfg.LLM.WebSearchTool != "web_search_preview" {
		t.Fatalf("unexpected web search tool: %s", cfg.LLM.WebSearchTool)
	}
	if cfg.LLM.RequireWebSearch {
		t.Fatal("expected web search not to be required")
	}
}

func TestLoadReadsJSONConfigWithUTF8BOM(t *testing.T) {
	withTempCWD(t)
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"addr":":17777","llm":{"model":"bom-model"}}`)...)
	if err := os.WriteFile("config.json", data, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("APP_CONFIG_PATH", "")
	t.Setenv("APP_ADDR", "")
	t.Setenv("LLM_MODEL", "")

	cfg := Load()
	if cfg.Addr != ":17777" {
		t.Fatalf("unexpected addr: %s", cfg.Addr)
	}
	if cfg.LLM.Model != "bom-model" {
		t.Fatalf("unexpected model: %s", cfg.LLM.Model)
	}
	if cfg.ConfigPath == "" {
		t.Fatal("expected config path")
	}
}

func TestLoadFindsProjectRootConfigFromNestedDevCWD(t *testing.T) {
	withTempCWD(t)
	if err := os.MkdirAll(filepath.Join("backend", "cmd", "server"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join("backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("backend", "go.mod"), []byte("module test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("config.example.json", []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("config.json", []byte(`{"addr":":16666","llm":{"model":"nested-dev-model"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Join("backend", "cmd", "server")); err != nil {
		t.Fatal(err)
	}

	t.Setenv("APP_CONFIG_PATH", "")
	t.Setenv("APP_ADDR", "")
	t.Setenv("LLM_MODEL", "")

	cfg := Load()
	if cfg.Addr != ":16666" {
		t.Fatalf("unexpected addr: %s", cfg.Addr)
	}
	if cfg.LLM.Model != "nested-dev-model" {
		t.Fatalf("unexpected model: %s", cfg.LLM.Model)
	}
	if cfg.ConfigPath == "" {
		t.Fatal("expected config path")
	}
}

func TestEnvOverridesJSONConfig(t *testing.T) {
	withTempCWD(t)
	if err := os.WriteFile("config.json", []byte(`{"llm":{"baseUrl":"https://file.example.com","apiKey":"file-key"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("APP_CONFIG_PATH", "")
	t.Setenv("LLM_BASE_URL", "https://env.example.com")
	t.Setenv("LLM_API_KEY", "env-key")
	t.Setenv("LLM_REASONING_EFFORT", "medium")
	t.Setenv("LLM_ENABLE_WEB_SEARCH", "false")
	t.Setenv("LLM_WEB_SEARCH_TOOL", "web_search_preview")
	t.Setenv("LLM_REQUIRE_WEB_SEARCH", "false")

	cfg := Load()
	if cfg.LLM.BaseURL != "https://env.example.com" {
		t.Fatalf("unexpected base url: %s", cfg.LLM.BaseURL)
	}
	if cfg.LLM.APIKey != "env-key" {
		t.Fatalf("unexpected api key: %s", cfg.LLM.APIKey)
	}
	if cfg.LLM.ReasoningEffort != "medium" {
		t.Fatalf("unexpected reasoning effort: %s", cfg.LLM.ReasoningEffort)
	}
	if cfg.LLM.EnableWebSearch {
		t.Fatal("expected env to disable web search")
	}
	if cfg.LLM.WebSearchTool != "web_search_preview" {
		t.Fatalf("unexpected web search tool: %s", cfg.LLM.WebSearchTool)
	}
	if cfg.LLM.RequireWebSearch {
		t.Fatal("expected env to make web search optional")
	}
}

func TestJSONConfigOverridesDotEnvCompatibility(t *testing.T) {
	withTempCWD(t)
	if err := os.WriteFile(".env", []byte("APP_ADDR=:7070\nLLM_MODEL=dotenv-model\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("config.json", []byte(`{"addr":":15566","llm":{"model":"json-model"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("APP_CONFIG_PATH", "")
	t.Setenv("APP_ADDR", "")
	t.Setenv("LLM_MODEL", "")

	cfg := Load()
	if cfg.Addr != ":15566" {
		t.Fatalf("unexpected addr: %s", cfg.Addr)
	}
	if cfg.LLM.Model != "json-model" {
		t.Fatalf("unexpected model: %s", cfg.LLM.Model)
	}
}

func TestDotEnvCompatibility(t *testing.T) {
	withTempCWD(t)
	if err := os.WriteFile(".env", []byte("APP_ADDR=:7777\nLLM_MODEL=dotenv-model\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("APP_CONFIG_PATH", "")
	t.Setenv("APP_ADDR", "")
	t.Setenv("LLM_MODEL", "")

	cfg := Load()
	if cfg.Addr != ":7777" {
		t.Fatalf("unexpected addr: %s", cfg.Addr)
	}
	if cfg.LLM.Model != "dotenv-model" {
		t.Fatalf("unexpected model: %s", cfg.LLM.Model)
	}
}
