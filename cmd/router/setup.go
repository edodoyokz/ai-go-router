package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

// runSetup auto-configures supported CLI tools to point to the local router instance.
func runSetup(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	routerBase := fmt.Sprintf("http://%s:%d/v1", cfg.Server.Host, cfg.Server.Port)
	if cfg.Server.Host == "0.0.0.0" || cfg.Server.Host == "" {
		routerBase = fmt.Sprintf("http://127.0.0.1:%d/v1", cfg.Server.Port)
	}
	apiKey := cfg.Server.APIKey
	if apiKey == "" {
		apiKey = "router-local"
	}

	fmt.Println("router auto-configuration")
	fmt.Printf("  Router base URL : %s\n", routerBase)
	fmt.Printf("  API key         : %s\n", maskKey(apiKey))
	fmt.Println()

	results := []setupResult{}

	// --- Cursor ---
	if r := setupCursor(routerBase, apiKey); r != nil {
		results = append(results, *r)
	}

	// --- VS Code (Continue extension) ---
	if r := setupVSCodeContinue(routerBase, apiKey); r != nil {
		results = append(results, *r)
	}

	// --- Claude Code (claude CLI) ---
	if r := setupClaudeCode(routerBase, apiKey); r != nil {
		results = append(results, *r)
	}

	// --- OpenAI CLI ---
	if r := setupOpenAICLI(routerBase, apiKey); r != nil {
		results = append(results, *r)
	}

	// Print summary
	fmt.Println("Results:")
	for _, r := range results {
		status := "✓"
		if r.err != nil {
			status = "✗"
		}
		fmt.Printf("  [%s] %s", status, r.tool)
		if r.err != nil {
			fmt.Printf(" — %v", r.err)
		} else {
			fmt.Printf(" — %s", r.path)
		}
		fmt.Println()
	}
	return nil
}

type setupResult struct {
	tool string
	path string
	err  error
}

// maskKey returns the first 4 chars + "****" for display.
func maskKey(key string) string {
	if len(key) <= 4 {
		return "****"
	}
	return key[:4] + "****"
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return os.Getenv("HOME")
}

// setupCursor writes to ~/.cursor/settings.json (or AppData on Windows).
func setupCursor(base, key string) *setupResult {
	var cfgDir string
	switch runtime.GOOS {
	case "windows":
		cfgDir = filepath.Join(os.Getenv("APPDATA"), "Cursor", "User")
	case "darwin":
		cfgDir = filepath.Join(homeDir(), "Library", "Application Support", "Cursor", "User")
	default:
		cfgDir = filepath.Join(homeDir(), ".config", "Cursor", "User")
	}

	path := filepath.Join(cfgDir, "settings.json")
	return patchJSONSettings(path, "Cursor", map[string]any{
		"cursor.openai.apiKey":  key,
		"cursor.openai.baseUrl": base,
	})
}

// setupVSCodeContinue writes to ~/.continue/config.json.
func setupVSCodeContinue(base, key string) *setupResult {
	path := filepath.Join(homeDir(), ".continue", "config.json")
	_ = os.MkdirAll(filepath.Dir(path), 0755)

	existing := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &existing)
	}

	models, _ := existing["models"].([]any)
	routerModel := map[string]any{
		"title":         "router",
		"provider":      "openai",
		"model":         "gpt-4",
		"apiKey":        key,
		"apiBase":       base,
		"contextLength": 128000,
	}

	// Prepend router entry (remove existing one if present)
	newModels := []any{routerModel}
	for _, m := range models {
		if mm, ok := m.(map[string]any); ok {
			if mm["title"] == "router" {
				continue
			}
		}
		newModels = append(newModels, m)
	}
	existing["models"] = newModels

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return &setupResult{"Continue (VS Code)", path, err}
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return &setupResult{"Continue (VS Code)", path, err}
	}
	return &setupResult{"Continue (VS Code)", path, nil}
}

// setupClaudeCode writes ANTHROPIC_BASE_URL + ANTHROPIC_API_KEY to ~/.claude/.env.
func setupClaudeCode(base, key string) *setupResult {
	dir := filepath.Join(homeDir(), ".claude")
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, ".env")

	// Anthropic base URL is without /v1 suffix for the CLI
	anthropicBase := strings.TrimSuffix(base, "/v1")

	lines := []string{
		"# Auto-configured by router setup",
		"ANTHROPIC_BASE_URL=" + anthropicBase,
		"ANTHROPIC_API_KEY=" + key,
	}

	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		return &setupResult{"Claude Code", path, err}
	}
	return &setupResult{"Claude Code", path, nil}
}

// setupOpenAICLI writes OPENAI_API_KEY + OPENAI_BASE_URL to ~/.openai/.env.
func setupOpenAICLI(base, key string) *setupResult {
	dir := filepath.Join(homeDir(), ".openai")
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, ".env")

	lines := []string{
		"# Auto-configured by router setup",
		"OPENAI_API_KEY=" + key,
		"OPENAI_BASE_URL=" + base,
	}

	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		return &setupResult{"OpenAI CLI", path, err}
	}
	return &setupResult{"OpenAI CLI", path, nil}
}

// patchJSONSettings merges key-value pairs into an existing JSON settings file.
func patchJSONSettings(path, tool string, patches map[string]any) *setupResult {
	_ = os.MkdirAll(filepath.Dir(path), 0755)

	existing := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &existing)
	}

	for k, v := range patches {
		existing[k] = v
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return &setupResult{tool, path, err}
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return &setupResult{tool, path, err}
	}
	return &setupResult{tool, path, nil}
}
