# Reference Route Map — Final (Fase 10)

Updated: 2026-04-28. Source of truth: `internal/api/server.go` NewRouter.

## Public Routes

| Method | Path | Handler |
|--------|------|---------|
| GET | `/` | redirect to `/ui/` |
| GET | `/ui`, `/ui/*` | embedded SPA |
| GET | `/dashboard` | redirect to `/ui/` |
| GET | `/healthz` | health check |
| GET | `/api/health` | health check |
| GET | `/readyz` | readiness check |
| GET | `/metrics` | Prometheus metrics |
| GET | `/api/setup/status` | onboarding status |
| POST | `/api/auth/login` | login |
| POST | `/api/auth/logout` | logout |
| GET | `/api/settings/require-login` | login requirement check |

## Inference Compatibility Routes

| Method | Path | Format |
|--------|------|--------|
| GET | `/v1/models` | OpenAI |
| POST | `/v1/chat/completions` | OpenAI Chat |
| POST | `/v1/messages` | Anthropic Messages |
| POST | `/v1/messages/count_tokens` | Anthropic |
| POST | `/v1/responses` | OpenAI Responses |
| POST | `/v1/responses/compact` | OpenAI Responses (compact) |
| POST | `/v1/embeddings` | OpenAI Embeddings |
| POST | `/v1/audio/speech` | OpenAI TTS |
| POST | `/v1/audio/transcriptions` | OpenAI STT |
| POST | `/v1/images/generations` | OpenAI Images |
| POST | `/v1/api/chat` | Ollama Chat |
| POST | `/v1/web/search` | Web Search |
| POST | `/v1/web/fetch` | Web Fetch |
| GET | `/v1beta/models` | Gemini |
| GET | `/v1beta/models/{model}` | Gemini |
| POST | `/v1beta/models/{path:.*}` | Gemini |
| POST | `/codex/v1/responses` | Codex |
| POST | `/codex/{path}` | Codex |

## Admin — Providers

| Method | Path |
|--------|------|
| GET | `/api/providers` |
| POST | `/api/providers` |
| GET | `/api/providers/catalog` |
| GET | `/api/providers/client` |
| GET | `/api/providers/kilo/free-models` |
| POST | `/api/providers/validate` |
| GET | `/api/providers/suggested-models` |
| POST | `/api/providers/test-batch` |
| GET | `/api/providers/{name}` |
| PUT | `/api/providers/{name}` |
| DELETE | `/api/providers/{name}` |
| POST | `/api/providers/{name}/test` |
| POST | `/api/providers/{name}/test-models` |
| GET | `/api/providers/{name}/models` |
| GET | `/api/providers/{name}/health` |
| GET | `/api/providers/{name}/accounts/{account}/health` |

## Admin — Provider Nodes

| Method | Path |
|--------|------|
| GET | `/api/provider-nodes` |
| POST | `/api/provider-nodes` |
| POST | `/api/provider-nodes/validate` |
| GET | `/api/provider-nodes/{id}` |
| PUT | `/api/provider-nodes/{id}` |
| DELETE | `/api/provider-nodes/{id}` |

## Admin — Proxy Pools

| Method | Path |
|--------|------|
| GET | `/api/proxy-pools` |
| POST | `/api/proxy-pools` |
| POST | `/api/proxy-pools/vercel-deploy` |
| GET | `/api/proxy-pools/{id}` |
| PUT | `/api/proxy-pools/{id}` |
| DELETE | `/api/proxy-pools/{id}` |
| POST | `/api/proxy-pools/{id}/test` |

## Admin — Combos / Routing

| Method | Path |
|--------|------|
| GET | `/api/combos` |
| POST | `/api/combos` |
| GET | `/api/combos/{name}` |
| PUT | `/api/combos/{name}` |
| DELETE | `/api/combos/{name}` |

## Admin — API Keys

| Method | Path |
|--------|------|
| GET | `/api/keys` |
| POST | `/api/keys` |
| GET | `/api/keys/{id}` |
| PUT | `/api/keys/{id}` |
| DELETE | `/api/keys/{id}` |

## Admin — Models & Aliases

| Method | Path |
|--------|------|
| GET | `/api/models` |
| PUT | `/api/models` |
| POST | `/api/models/test` |
| GET | `/api/models/availability` |
| POST | `/api/models/availability` |
| GET | `/api/models/alias` |
| POST | `/api/models/alias` |
| PUT | `/api/models/alias` |
| PUT | `/api/models/alias/{name}` |
| DELETE | `/api/models/alias` |
| DELETE | `/api/models/alias/{name}` |
| GET | `/api/models/custom` |
| POST | `/api/models/custom` |
| PUT | `/api/models/custom/{name}` |
| DELETE | `/api/models/custom/{name}` |

## Admin — Config / Settings

| Method | Path |
|--------|------|
| GET | `/api/config` |
| GET | `/api/config/export` |
| POST | `/api/config/import` |
| GET | `/api/settings` |
| PUT | `/api/settings` |
| PATCH | `/api/settings` |
| GET | `/api/settings/database` |
| POST | `/api/settings/database` |
| POST | `/api/settings/proxy-test` |
| POST | `/api/locale` |
| GET | `/api/init` |
| GET | `/api/version` |
| POST | `/api/version/update` |
| POST | `/api/shutdown` |

## Admin — Usage / Logs / Pricing

| Method | Path |
|--------|------|
| GET | `/api/logs` |
| GET | `/api/usage` |
| GET | `/api/usage/stats` |
| GET | `/api/usage/history` |
| GET | `/api/usage/chart` |
| GET | `/api/usage/providers` |
| GET | `/api/usage/logs` |
| GET | `/api/usage/request-logs` |
| GET | `/api/usage/request-details` |
| GET | `/api/usage/stream` |
| GET | `/api/usage/{connectionId}` |
| GET | `/api/metrics/json` |
| GET | `/api/pricing` |
| PATCH | `/api/pricing` |
| DELETE | `/api/pricing` |
| GET | `/api/tags` |

## Admin — OAuth

| Method | Path |
|--------|------|
| GET | `/api/oauth/tokens` |
| DELETE | `/api/oauth/tokens/{provider}/{account}` |
| GET | `/api/oauth/authorize` |
| GET | `/api/oauth/callback` |
| POST | `/api/oauth/exchange` |
| GET | `/api/oauth/poll/{provider}` |
| GET | `/api/oauth/{provider}/{action}` |
| POST | `/api/oauth/{provider}/{action}` |
| GET | `/api/oauth/cursor/import` |
| POST | `/api/oauth/cursor/import` |
| GET | `/api/oauth/cursor/auto-import` |
| POST | `/api/oauth/gitlab/pat` |
| POST | `/api/oauth/iflow/cookie` |
| GET | `/api/oauth/kiro/auto-import` |
| POST | `/api/oauth/kiro/import` |
| GET | `/api/oauth/kiro/social-authorize` |
| POST | `/api/oauth/kiro/social-exchange` |

## Admin — Translator

| Method | Path |
|--------|------|
| GET | `/api/translator/load` |
| POST | `/api/translator/save` |
| POST | `/api/translator/translate` |
| POST | `/api/translator/send` |
| GET | `/api/translator/console-logs` |
| DELETE | `/api/translator/console-logs` |
| GET | `/api/translator/console-logs/stream` |

## Admin — CLI Tools / MITM

| Method | Path |
|--------|------|
| GET | `/api/cli-tools/antigravity-mitm` |
| POST | `/api/cli-tools/antigravity-mitm` |
| DELETE | `/api/cli-tools/antigravity-mitm` |
| PATCH | `/api/cli-tools/antigravity-mitm` |
| GET | `/api/cli-tools/antigravity-mitm/alias` |
| PUT | `/api/cli-tools/antigravity-mitm/alias` |
| GET | `/api/cli-tools/{tool}-settings` |
| POST | `/api/cli-tools/{tool}-settings` |
| PATCH | `/api/cli-tools/{tool}-settings` |
| DELETE | `/api/cli-tools/{tool}-settings` |

## Admin — Tunnel

| Method | Path |
|--------|------|
| GET | `/api/tunnel/status` |
| POST | `/api/tunnel/enable` |
| POST | `/api/tunnel/disable` |
| GET | `/api/tunnel/tailscale-check` |
| POST | `/api/tunnel/tailscale-enable` |
| POST | `/api/tunnel/tailscale-disable` |
| POST | `/api/tunnel/tailscale-login` |
| POST | `/api/tunnel/tailscale-install` |
| POST | `/api/tunnel/tailscale-start-daemon` |

## Admin — Cloud

| Method | Path |
|--------|------|
| POST | `/api/cloud/auth` |
| PUT | `/api/cloud/credentials/update` |
| POST | `/api/cloud/model/resolve` |
| GET | `/api/cloud/models/alias` |
| PUT | `/api/cloud/models/alias` |
| GET | `/api/sync/status` |
| GET | `/api/nodes` |

## Admin — Media Providers

| Method | Path |
|--------|------|
| GET | `/api/media-providers/tts/voices` |
| GET | `/api/media-providers/tts/elevenlabs/voices` |

## Cloud Server Routes (`cmd/cloud`)

| Method | Path |
|--------|------|
| GET | `/` | landing page |
| GET | `/health` | health check |
| GET | `/api/tags` | Ollama compat model list |
| POST | `/cache/clear` | cache clear |
| GET/POST/DELETE | `/sync/{machineId}` | sync CRUD |
| POST | `/forward` | forward inference |
| POST | `/forward-raw` | raw forward |
| POST | `/testClaude` | Claude test |
| GET | `/v1/verify` | verify machine |
| POST | `/v1/chat/completions` | cloud inference |
| POST | `/v1/messages` | cloud inference |
| POST | `/v1/messages/count_tokens` | cloud count tokens |
| POST | `/v1/embeddings` | cloud inference |
| POST | `/v1/responses` | cloud inference |
| POST | `/v1/responses/compact` | cloud inference |
| POST | `/v1/api/chat` | cloud Ollama chat |
| GET | `/{machineId}/v1/verify` | verify machine by ID |
| POST | `/{machineId}/v1/chat/completions` | cloud inference |
| POST | `/{machineId}/v1/messages` | cloud inference |
| POST | `/{machineId}/v1/messages/count_tokens` | cloud count tokens |
| POST | `/{machineId}/v1/embeddings` | cloud inference |
| POST | `/{machineId}/v1/responses` | cloud inference |
| POST | `/{machineId}/v1/responses/compact` | cloud inference |
| POST | `/{machineId}/v1/api/chat` | cloud Ollama chat |

## Summary

- **Total local API routes**: ~175+
- **Total cloud routes**: ~20
- **Previously missing routes (Phase 0)**: all now implemented
- Audio transcriptions (`/v1/audio/transcriptions`), web search/fetch (`/v1/web/search`, `/v1/web/fetch`) added in later phases
