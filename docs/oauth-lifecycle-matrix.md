# OAuth Lifecycle Matrix

Updated: 2026-04-28. Source of truth: `internal/oauth`, `internal/api/server.go`, and OAuth/API tests.

| Provider | Auth type | Authorize | Device code | Exchange/Poll | Refresh | Delete | Runtime wiring | Status |
|----------|-----------|-----------|-------------|---------------|---------|--------|----------------|--------|
| codex | oauth + PKCE | yes | n/a | yes | yes | yes | Codex Responses executor | supported |
| openai | oauth + PKCE | yes | n/a | yes | yes | yes | OpenAI-compatible/Codex token store path | supported |
| github/copilot | device_code | n/a | yes | yes | Copilot token refresh on demand | yes | GitHub Copilot executor | supported |
| qwen | device_code + PKCE | n/a | yes | yes | token refresh when endpoint permits | yes | deprecated-safe Qwen executor | supported, deprecated |
| kiro | device/import/social | social authorize | yes | yes | yes | yes | Kiro native executor | supported |
| cursor | import | n/a | n/a | import/auto-import | token reuse/import | yes | Cursor native executor | supported |
| gemini-cli | oauth | yes | n/a | yes | yes | yes | Gemini CLI executor | supported, deprecated |
| antigravity | oauth | yes | n/a | yes | yes | yes | Antigravity executor + MITM support | supported, deprecated |
| iflow | oauth/cookie | yes | n/a | yes + cookie import | yes | yes | iFlow native executor | supported |
| qoder | oauth | yes | n/a | yes | yes | yes | Qoder native executor | supported |
| gitlab | PAT | n/a | n/a | PAT import | n/a | yes | stored provider connection metadata | supported import |
| anthropic/claude | api_key/oauth | yes | n/a | yes | yes when refresh token exists | yes | Anthropic-compatible adapter | supported |

## Verification

- `go test ./internal/api -run 'Test.*OAuth|TestCodexAuthorize|TestGitHub|TestQwen|TestKiro|TestCursor|TestIFlow|TestGitLab' -count=1`
- `go test ./internal/oauth ./internal/storage -count=1`
- `go test ./... -count=1`

All OAuth lifecycle tests are mock-server/local-storage based and do not call live third-party services.
