package api

import (
	"net/http"
	"strings"
)

// ToolDetector detects client tools from HTTP requests
type ToolDetector struct{}

// NewToolDetector creates a new tool detector
func NewToolDetector() *ToolDetector {
	return &ToolDetector{}
}

// DetectTool detects the client tool from the request
func (td *ToolDetector) DetectTool(r *http.Request) string {
	// Check User-Agent header
	userAgent := r.Header.Get("User-Agent")
	if userAgent != "" {
		if strings.Contains(userAgent, "Cursor") {
			return "cursor"
		}
		if strings.Contains(userAgent, "Windsurf") {
			return "windsurf"
		}
		if strings.Contains(userAgent, "VSCode") || strings.Contains(userAgent, "vscode") {
			return "vscode"
		}
		if strings.Contains(userAgent, "Copilot") {
			return "copilot"
		}
	}

	// Check X-Client-Name header
	clientName := r.Header.Get("X-Client-Name")
	if clientName != "" {
		return strings.ToLower(clientName)
	}

	// Check custom headers for tool identification
	if r.Header.Get("X-Cursor-Version") != "" {
		return "cursor"
	}
	if r.Header.Get("X-Windsurf-Version") != "" {
		return "windsurf"
	}

	return "unknown"
}

// DetectModelFromTool returns the preferred model for a given tool
func (td *ToolDetector) DetectModelFromTool(tool string, defaultModel string) string {
	// Tool-specific model preferences could be configured here
	// For now, return the default model
	return defaultModel
}
