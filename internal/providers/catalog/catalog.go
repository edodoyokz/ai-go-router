package catalog

import "strings"

type ProviderDefinition struct {
	ID                       string            `json:"id"`
	Alias                    string            `json:"alias,omitempty"`
	Aliases                  []string          `json:"aliases,omitempty"`
	Name                     string            `json:"name"`
	Category                 string            `json:"category"`
	AuthTypes                []string          `json:"auth_types,omitempty"`
	ServiceKinds             []string          `json:"service_kinds,omitempty"`
	DefaultBaseURL           string            `json:"default_base_url,omitempty"`
	Format                   string            `json:"format,omitempty"`
	EndpointStyle            string            `json:"endpoint_style,omitempty"`
	Headers                  map[string]string `json:"headers,omitempty"`
	Website                  string            `json:"website,omitempty"`
	Icon                     string            `json:"icon,omitempty"`
	Color                    string            `json:"color,omitempty"`
	APIKeyURL                string            `json:"api_key_url,omitempty"`
	Notice                   string            `json:"notice,omitempty"`
	KindNotice               map[string]string `json:"kind_notice,omitempty"`
	Hidden                   bool              `json:"hidden,omitempty"`
	HiddenKinds              []string          `json:"hidden_kinds,omitempty"`
	Deprecated               bool              `json:"deprecated,omitempty"`
	DeprecationNotice        string            `json:"deprecation_notice,omitempty"`
	NoAuth                   bool              `json:"no_auth,omitempty"`
	PassthroughModels        bool              `json:"passthrough_models,omitempty"`
	ThinkingMode             string            `json:"thinking_mode,omitempty"`
	ModelFetcher             string            `json:"model_fetcher,omitempty"`
	SupportsResponses        bool              `json:"supports_responses,omitempty"`
	SupportsEmbeddings       bool              `json:"supports_embeddings,omitempty"`
	SupportsAudio            bool              `json:"supports_audio,omitempty"`
	SupportsImages           bool              `json:"supports_images,omitempty"`
	RequiresProjectID        bool              `json:"requires_project_id,omitempty"`
	RuntimeNotes             string            `json:"runtime_notes,omitempty"`
	UnsupportedRuntimeReason string            `json:"unsupported_runtime_reason,omitempty"`
	RuntimeRequirements      []string          `json:"runtime_requirements,omitempty"`
	ExecutionStatus          string            `json:"execution_status"`
	ExecutionKind            string            `json:"execution_kind,omitempty"`
}

var definitions = []ProviderDefinition{
	{ID: "openai", Alias: "openai", Aliases: []string{"oai", "openai_compat"}, Name: "OpenAI", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"llm", "embedding", "tts", "image", "imageToText", "webSearch"}, DefaultBaseURL: "https://api.openai.com/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible", ThinkingMode: "effort"},
	{ID: "openrouter", Alias: "openrouter", Aliases: []string{"or"}, Name: "OpenRouter", Category: "free_tier", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"llm", "embedding", "tts", "imageToText"}, DefaultBaseURL: "https://openrouter.ai/api/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible", PassthroughModels: true},
	{ID: "anthropic", Alias: "anthropic", Aliases: []string{"cc", "anthropic_compat"}, Name: "Anthropic", Category: "api_key", AuthTypes: []string{"api_key", "oauth"}, ServiceKinds: []string{"llm", "imageToText"}, DefaultBaseURL: "https://api.anthropic.com", Format: "claude", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "anthropic_compatible"},
	{ID: "codex", Alias: "cx", Aliases: []string{"codex"}, Name: "OpenAI Codex", Category: "oauth", AuthTypes: []string{"oauth"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://chatgpt.com/backend-api/codex/responses", Format: "openai-responses", EndpointStyle: "full_responses_endpoint", SupportsResponses: true, ExecutionStatus: "supported", ExecutionKind: "native", RuntimeNotes: "Native Responses executor with compact routing, OAuth authorize/exchange/refresh, and SSE response streaming support."},
	{ID: "github", Alias: "gh", Aliases: []string{"copilot"}, Name: "GitHub Copilot", Category: "oauth", AuthTypes: []string{"device_code"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://api.githubcopilot.com", Format: "openai", EndpointStyle: "api_root", SupportsResponses: true, ExecutionStatus: "supported", ExecutionKind: "native"},
	{ID: "deepseek", Alias: "ds", Aliases: []string{"deepseek"}, Name: "DeepSeek", Category: "api_key", AuthTypes: []string{"api_key"}, DefaultBaseURL: "https://api.deepseek.com/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "kiro", Alias: "kr", Name: "Kiro AI", Category: "free", AuthTypes: []string{"device_code", "import"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse", Format: "kiro", EndpointStyle: "full_responses_endpoint", ExecutionStatus: "supported", ExecutionKind: "native", RuntimeNotes: "Native Kiro executor with AWS EventStream parsing, import/social auth, auto-import, and refresh helper."},
	{ID: "qwen", Alias: "qw", Name: "Qwen Code", Category: "free", AuthTypes: []string{"device_code"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://portal.qwen.ai/v1", Format: "openai", EndpointStyle: "api_root", Deprecated: true, DeprecationNotice: "Qwen OAuth free tier was discontinued by Alibaba on 2026-04-15; runtime only works with an existing valid token.", ExecutionStatus: "supported", ExecutionKind: "qwen", RuntimeNotes: "Deprecated-safe runtime: not recommended by default; requires an existing valid OAuth token and returns explicit auth/free-tier errors on 401/403.", RuntimeRequirements: []string{"existing valid Qwen OAuth access token"}},
	{ID: "opencode", Alias: "oc", Name: "OpenCode", Category: "free", AuthTypes: []string{"no_auth"}, DefaultBaseURL: "https://opencode.ai/zen/v1", Format: "openai", EndpointStyle: "api_root", NoAuth: true, PassthroughModels: true, ExecutionStatus: "supported", ExecutionKind: "openai_compatible", ModelFetcher: "opencode-free"},
	{ID: "groq", Alias: "groq", Aliases: []string{"gx"}, Name: "Groq", Category: "api_key", AuthTypes: []string{"api_key"}, DefaultBaseURL: "https://api.groq.com/openai/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "mistral", Alias: "mistral", Aliases: []string{"ms"}, Name: "Mistral", Category: "api_key", AuthTypes: []string{"api_key"}, DefaultBaseURL: "https://api.mistral.ai/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "cohere", Alias: "cohere", Name: "Cohere", Category: "api_key", AuthTypes: []string{"api_key"}, DefaultBaseURL: "https://api.cohere.com/compatibility/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "google", Alias: "google", Aliases: []string{"gemini", "gg"}, Name: "Google", Category: "api_key", AuthTypes: []string{"api_key", "oauth"}, DefaultBaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "xai", Alias: "xai", Name: "xAI", Category: "api_key", AuthTypes: []string{"api_key"}, DefaultBaseURL: "https://api.x.ai/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "perplexity", Alias: "pplx", Aliases: []string{"perplexity"}, Name: "Perplexity", Category: "api_key", AuthTypes: []string{"api_key"}, DefaultBaseURL: "https://api.perplexity.ai", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "together", Alias: "together", Name: "Together", Category: "api_key", AuthTypes: []string{"api_key"}, DefaultBaseURL: "https://api.together.xyz/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "fireworks", Alias: "fireworks", Name: "Fireworks", Category: "api_key", AuthTypes: []string{"api_key"}, DefaultBaseURL: "https://api.fireworks.ai/inference/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "cerebras", Alias: "cerebras", Name: "Cerebras", Category: "api_key", AuthTypes: []string{"api_key"}, DefaultBaseURL: "https://api.cerebras.ai/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "nebius", Alias: "nebius", Name: "Nebius", Category: "api_key", AuthTypes: []string{"api_key"}, DefaultBaseURL: "https://api.studio.nebius.ai/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "siliconflow", Alias: "sf", Aliases: []string{"siliconflow"}, Name: "SiliconFlow", Category: "api_key", AuthTypes: []string{"api_key"}, DefaultBaseURL: "https://api.siliconflow.cn/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "hyperbolic", Alias: "hyperbolic", Name: "Hyperbolic", Category: "api_key", AuthTypes: []string{"api_key"}, DefaultBaseURL: "https://api.hyperbolic.xyz/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "chutes", Alias: "chutes", Name: "Chutes", Category: "api_key", AuthTypes: []string{"api_key"}, DefaultBaseURL: "https://llm.chutes.ai/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "nvidia", Alias: "nvidia", Name: "NVIDIA NIM", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://integrate.api.nvidia.com/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "byteplus", Alias: "byteplus", Name: "BytePlus", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://ark.cn-beijing.volces.com/api/v3", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "glm", Alias: "glm", Name: "GLM", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://open.bigmodel.cn/api/paas/v4", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "glm-cn", Alias: "glm-cn", Aliases: []string{"zhipu"}, Name: "GLM CN", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://open.bigmodel.cn/api/paas/v4", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "kimi", Alias: "kimi", Name: "Kimi", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://api.moonshot.ai/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "minimax", Alias: "minimax", Name: "MiniMax", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://api.minimax.io/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "minimax-cn", Alias: "minimax-cn", Name: "MiniMax CN", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://api.minimaxi.com/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "alicode", Alias: "alicode", Name: "Alicode", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "alicode-intl", Alias: "alicode-intl", Name: "Alicode Intl", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "volcengine-ark", Alias: "volcengine-ark", Aliases: []string{"ark"}, Name: "Volcengine Ark", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://ark.cn-beijing.volces.com/api/v3", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "blackbox", Alias: "blackbox", Name: "Blackbox", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://api.blackbox.ai/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "openai_compatible"},
	{ID: "cursor", Alias: "cu", Name: "Cursor", Category: "oauth", AuthTypes: []string{"import"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://api2.cursor.sh", Format: "cursor", EndpointStyle: "full_responses_endpoint", ExecutionStatus: "supported", ExecutionKind: "native", RuntimeNotes: "Native Cursor executor with local auto-import, checksum headers, protobuf chat request framing, and ConnectRPC stream decoding."},
	{ID: "antigravity", Alias: "ag", Name: "Antigravity", Category: "oauth", AuthTypes: []string{"oauth"}, ServiceKinds: []string{"llm"}, Deprecated: true, DeprecationNotice: "AG is designed exclusively for Antigravity IDE. Using it with other tools may result in account restrictions or bans.", DefaultBaseURL: "https://daily-cloudcode-pa.googleapis.com", ExecutionStatus: "supported", ExecutionKind: "native", RuntimeRequirements: []string{"oauth access_token"}},
	{ID: "grok-web", Alias: "gw", Name: "Grok Web", Category: "cookie", AuthTypes: []string{"cookie"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://grok.com/rest/app-chat/conversations/new", Format: "grok-web", EndpointStyle: "full_responses_endpoint", PassthroughModels: true, ExecutionStatus: "supported", ExecutionKind: "web", RuntimeNotes: "Cookie-session runtime with browser-compatible headers, NDJSON parsing, OpenAI response normalization, and explicit expired-cookie/rate-limit errors. Session replay remains fragile and should not be default-recommended.", RuntimeRequirements: []string{"valid grok.com sso cookie"}},
	{ID: "perplexity-web", Alias: "pw", Name: "Perplexity Web", Category: "cookie", AuthTypes: []string{"cookie"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://www.perplexity.ai/rest/sse/perplexity_ask", Format: "perplexity-web", EndpointStyle: "full_responses_endpoint", ExecutionStatus: "supported", ExecutionKind: "web", RuntimeNotes: "Cookie-session runtime with browser-compatible headers, SSE parsing, OpenAI response normalization, and explicit expired-cookie/rate-limit errors. Session replay remains fragile and should not be default-recommended.", RuntimeRequirements: []string{"valid perplexity.ai __Secure-next-auth.session-token cookie"}},
	{ID: "ollama", Alias: "ollama", Name: "Ollama", Category: "free", AuthTypes: []string{"no_auth"}, DefaultBaseURL: "http://127.0.0.1:11434", Format: "ollama", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "ollama"},
	{ID: "vertex", Alias: "vx", Name: "Vertex", Category: "api_key", AuthTypes: []string{"service_account"}, ServiceKinds: []string{"llm"}, RequiresProjectID: true, ExecutionStatus: "supported", ExecutionKind: "native", RuntimeRequirements: []string{"service_account_json or api_key", "project_id"}},
	{ID: "gemini-cli", Alias: "gc", Name: "Gemini CLI", Category: "free", AuthTypes: []string{"oauth"}, ServiceKinds: []string{"llm"}, Deprecated: true, DeprecationNotice: "Gemini CLI is designed exclusively for Gemini CLI. Using it with other tools may result in account restrictions or bans.", DefaultBaseURL: "https://cloudcode-pa.googleapis.com/v1internal", RequiresProjectID: true, ExecutionStatus: "supported", ExecutionKind: "native", RuntimeRequirements: []string{"oauth access_token", "project_id"}},
	{ID: "iflow", Alias: "if", Name: "iFlow AI", Category: "free", AuthTypes: []string{"oauth"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://apis.iflow.cn/v1/chat/completions", ExecutionStatus: "supported", ExecutionKind: "native", RuntimeRequirements: []string{"oauth/api_key token"}},
	{ID: "qoder", Alias: "qd", Name: "Qoder AI", Category: "free", AuthTypes: []string{"oauth"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://api.qoder.com/v1/chat/completions", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "native", RuntimeRequirements: []string{"oauth/api_key token"}},
	{ID: "opencode-go", Alias: "ocg", Name: "OpenCode Go", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"llm"}, DefaultBaseURL: "https://opencode.ai/zen/go/v1", Format: "openai", EndpointStyle: "api_root", ExecutionStatus: "supported", ExecutionKind: "native", RuntimeRequirements: []string{"api_key"}},
	{ID: "azure", Alias: "azure", Name: "Azure OpenAI", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"llm", "embedding", "image"}, DefaultBaseURL: "https://api.openai.com", Format: "openai", EndpointStyle: "azure_deployment", ExecutionStatus: "supported", ExecutionKind: "native", RuntimeRequirements: []string{"base_url (azure endpoint)", "api_key"}},
	{ID: "vertex-partner", Alias: "vxp", Name: "Vertex Partner", Category: "api_key", AuthTypes: []string{"service_account"}, ServiceKinds: []string{"llm"}, RequiresProjectID: true, ExecutionStatus: "supported", ExecutionKind: "native", RuntimeRequirements: []string{"service_account_json or api_key", "project_id"}},
	{ID: "deepgram", Alias: "dg", Name: "Deepgram", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"stt"}, DefaultBaseURL: "https://api.deepgram.com", ExecutionStatus: "supported", ExecutionKind: "media", RuntimeRequirements: []string{"api_key"}},
	{ID: "assemblyai", Alias: "aai", Name: "AssemblyAI", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"stt"}, DefaultBaseURL: "https://api.assemblyai.com", ExecutionStatus: "supported", ExecutionKind: "media", RuntimeRequirements: []string{"api_key"}},
	{ID: "nanobanana", Alias: "nb", Name: "NanoBanana", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"image"}, DefaultBaseURL: "https://api.nanobananaapi.ai", ExecutionStatus: "supported", ExecutionKind: "media", RuntimeRequirements: []string{"api_key"}},
	{ID: "elevenlabs", Alias: "el", Name: "ElevenLabs", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"tts"}, DefaultBaseURL: "https://api.elevenlabs.io", ExecutionStatus: "supported", ExecutionKind: "media", RuntimeRequirements: []string{"api_key"}},
	{ID: "cartesia", Alias: "cartesia", Name: "Cartesia", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"tts"}, Hidden: true, DefaultBaseURL: "https://api.cartesia.ai", ExecutionStatus: "supported", ExecutionKind: "media", RuntimeRequirements: []string{"api_key"}, RuntimeNotes: "TTS bytes endpoint runtime with Cartesia-Version header support."},
	{ID: "playht", Alias: "playht", Name: "PlayHT", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"tts"}, Hidden: true, DefaultBaseURL: "https://api.play.ht", ExecutionStatus: "supported", ExecutionKind: "media", RuntimeRequirements: []string{"api_key", "X-USER-ID header or provider_specific_data.userId"}, RuntimeNotes: "PlayHT v2 stream TTS runtime."},
	{ID: "local-device", Alias: "local-device", Name: "Local Device", Category: "free", AuthTypes: []string{"no_auth"}, ServiceKinds: []string{"tts"}, DefaultBaseURL: "local-device", NoAuth: true, ExecutionStatus: "supported", ExecutionKind: "media", RuntimeRequirements: []string{"OS TTS command", "ffmpeg"}, RuntimeNotes: "Local OS TTS bridge; returns explicit dependency errors when local binaries are unavailable."},
	{ID: "google-tts", Alias: "google-tts", Name: "Google TTS", Category: "free", AuthTypes: []string{"no_auth"}, ServiceKinds: []string{"tts"}, DefaultBaseURL: "https://translate.google.com", NoAuth: true, ExecutionStatus: "supported", ExecutionKind: "media", RuntimeNotes: "Google Translate web TTS runtime with cached token parsing."},
	{ID: "edge-tts", Alias: "edge-tts", Name: "Edge TTS", Category: "free", AuthTypes: []string{"no_auth"}, ServiceKinds: []string{"tts"}, DefaultBaseURL: "https://www.bing.com", NoAuth: true, ExecutionStatus: "supported", ExecutionKind: "media", RuntimeNotes: "Bing/Edge Translator TTS runtime with cached token parsing and one refresh retry on 403/429."},
	{ID: "sdwebui", Alias: "sdwebui", Name: "SD WebUI", Category: "api_key", AuthTypes: []string{"api_key", "no_auth"}, ServiceKinds: []string{"image"}, DefaultBaseURL: "http://127.0.0.1:7860", ExecutionStatus: "supported", ExecutionKind: "media"},
	{ID: "comfyui", Alias: "comfyui", Name: "ComfyUI", Category: "api_key", AuthTypes: []string{"api_key", "no_auth"}, ServiceKinds: []string{"image"}, DefaultBaseURL: "http://127.0.0.1:8188", ExecutionStatus: "supported", ExecutionKind: "media"},
	{ID: "huggingface", Alias: "hf", Name: "HuggingFace", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"image"}, DefaultBaseURL: "https://api-inference.huggingface.co/models", ExecutionStatus: "supported", ExecutionKind: "media", RuntimeRequirements: []string{"api_key"}},
	{ID: "ollama-local", Alias: "ollama-local", Name: "Ollama Local", Category: "free", AuthTypes: []string{"no_auth"}, ServiceKinds: []string{"llm"}, NoAuth: true, ExecutionStatus: "planned", ExecutionKind: "ollama", UnsupportedRuntimeReason: "requires local/cloud catalog split before runtime enablement"},
	{ID: "tavily", Alias: "tavily", Name: "Tavily", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"webSearch"}, DefaultBaseURL: "https://api.tavily.com", ExecutionStatus: "supported", ExecutionKind: "search", RuntimeRequirements: []string{"api_key"}},
	{ID: "brave-search", Alias: "brave", Name: "Brave Search", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"webSearch"}, DefaultBaseURL: "https://api.search.brave.com", ExecutionStatus: "supported", ExecutionKind: "search", RuntimeRequirements: []string{"api_key"}},
	{ID: "serper", Alias: "serper", Name: "Serper", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"webSearch"}, DefaultBaseURL: "https://google.serper.dev", ExecutionStatus: "supported", ExecutionKind: "search", RuntimeRequirements: []string{"api_key"}},
	{ID: "exa", Alias: "exa", Name: "Exa", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"webSearch"}, DefaultBaseURL: "https://api.exa.ai", ExecutionStatus: "supported", ExecutionKind: "search", RuntimeRequirements: []string{"api_key"}},
	{ID: "searxng", Alias: "searxng", Name: "SearXNG", Category: "free", AuthTypes: []string{"no_auth"}, ServiceKinds: []string{"webSearch"}, DefaultBaseURL: "http://127.0.0.1:8080", NoAuth: true, ExecutionStatus: "supported", ExecutionKind: "search"},
	{ID: "firecrawl", Alias: "firecrawl", Name: "Firecrawl", Category: "api_key", AuthTypes: []string{"api_key"}, ServiceKinds: []string{"webFetch"}, DefaultBaseURL: "https://api.firecrawl.dev", ExecutionStatus: "supported", ExecutionKind: "search", RuntimeRequirements: []string{"api_key"}},
}

func List() []ProviderDefinition {
	out := make([]ProviderDefinition, len(definitions))
	copy(out, definitions)
	return out
}

func Get(id string) (ProviderDefinition, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, def := range definitions {
		if def.ID == id {
			return def, true
		}
	}
	return ProviderDefinition{}, false
}

func ResolveAlias(name string) (ProviderDefinition, bool) {
	needle := strings.ToLower(strings.TrimSpace(name))
	for _, def := range definitions {
		if def.ID == needle {
			return def, true
		}
		if strings.EqualFold(def.Alias, needle) {
			return def, true
		}
		for _, alias := range def.Aliases {
			if strings.EqualFold(alias, needle) {
				return def, true
			}
		}
	}
	return ProviderDefinition{}, false
}

func InferProviderID(providerType, providerName string) string {
	if def, ok := ResolveAlias(providerType); ok {
		return def.ID
	}
	if def, ok := ResolveAlias(providerName); ok {
		return def.ID
	}
	return strings.ToLower(strings.TrimSpace(providerType))
}
