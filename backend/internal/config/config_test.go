package config

import (
	"os"
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
	if cfg.LLM.Timeout != 120*time.Second {
		t.Fatalf("unexpected timeout: %s", cfg.LLM.Timeout)
	}
	if cfg.LLM.ReasoningEffort != "xhigh" {
		t.Fatalf("unexpected reasoning effort: %s", cfg.LLM.ReasoningEffort)
	}
	if !cfg.LLM.EnableWebSearch {
		t.Fatal("expected web search to be enabled by default")
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
    "enableWebSearch": false
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
