package translator

// Format identifiers for hub-and-spoke translation
const (
	FormatOpenAI      = "openai"
	FormatClaude      = "claude"
	FormatOpenAIResp  = "openai-responses"
	FormatUnknown     = "unknown"
	FormatCursor      = "cursor"
	FormatKiro        = "kiro"
	FormatVertex      = "vertex"
	FormatAntigravity = "antigravity"
	// FormatGemini and FormatOllama are defined in their respective files.
)

// SupportedFormats returns all supported format identifiers
func SupportedFormats() []string {
	return []string{
		FormatOpenAI,
		FormatClaude,
		FormatOpenAIResp,
		FormatGemini,
		FormatOllama,
		FormatCursor,
		FormatKiro,
		FormatVertex,
		FormatAntigravity,
	}
}
