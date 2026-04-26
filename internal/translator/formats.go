package translator

// Format identifiers for hub-and-spoke translation
const (
	FormatOpenAI       = "openai"
	FormatClaude       = "claude"
	FormatOpenAIResp   = "openai-responses"
	FormatUnknown      = "unknown"
)

// SupportedFormats returns all supported format identifiers
func SupportedFormats() []string {
	return []string{
		FormatOpenAI,
		FormatClaude,
		FormatOpenAIResp,
	}
}
