# Provider Guide

This guide explains how to add and configure AI providers in NusaNexus Router.

---

## Overview

NusaNexus Router supports multiple AI providers through a unified interface. Providers are configured in `config.yaml` and can be of different types:

- `openai_compat`: OpenAI-compatible APIs (OpenAI, DeepSeek, Groq, Mistral, xAI, etc.)
- `anthropic`: Native Anthropic API
- `anthropic_compat`: Anthropic-compatible APIs
- `openrouter`: OpenRouter (OpenAI-compatible with custom headers)

---

## Provider Configuration

### Basic Structure

```yaml
providers:
  - name: my-provider
    type: openai_compat
    base_url: https://api.example.com/v1
    enabled: true
    accounts:
      - name: account-1
        api_key: ${API_KEY_ENV_VAR}
```

### Configuration Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique provider identifier |
| `type` | string | Yes | Provider type (see below) |
| `base_url` | string | Yes | API base URL |
| `enabled` | bool | No | Enable/disable provider (default: true) |
| `api_key` | string | No | Deprecated: single API key (use `accounts` instead) |
| `accounts` | array | No | Multi-account configuration (recommended) |

### Account Configuration

```yaml
accounts:
  - name: account-1
    api_key: ${API_KEY_ENV_VAR}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Account identifier |
| `api_key` | string | Yes | API key (supports `${ENV_VAR}` syntax) |

---

## Provider Types

### OpenAI-Compatible (`openai_compat`)

For providers that follow OpenAI's API format.

#### Supported Providers

- OpenAI
- DeepSeek
- Groq
- xAI (Grok)
- Mistral
- Together AI
- Any OpenAI-compatible API

#### Example: OpenAI

```yaml
providers:
  - name: openai
    type: openai_compat
    base_url: https://api.openai.com/v1
    enabled: true
    accounts:
      - name: account-1
        api_key: ${OPENAI_API_KEY_1}
      - name: account-2
        api_key: ${OPENAI_API_KEY_2}
```

#### Example: DeepSeek

```yaml
providers:
  - name: deepseek
    type: openai_compat
    base_url: https://api.deepseek.com
    enabled: true
    accounts:
      - name: account-1
        api_key: ${DEEPSEEK_API_KEY}
```

#### Example: Groq

```yaml
providers:
  - name: groq
    type: openai_compat
    base_url: https://api.groq.com/openai/v1
    enabled: true
    accounts:
      - name: account-1
        api_key: ${GROQ_API_KEY}
```

#### Example: Mistral

```yaml
providers:
  - name: mistral
    type: openai_compat
    base_url: https://api.mistral.ai/v1
    enabled: true
    accounts:
      - name: account-1
        api_key: ${MISTRAL_API_KEY}
```

---

### Anthropic (`anthropic`)

Native Anthropic API.

#### Example

```yaml
providers:
  - name: anthropic
    type: anthropic
    base_url: https://api.anthropic.com
    enabled: true
    accounts:
      - name: account-1
        api_key: ${ANTHROPIC_API_KEY}
```

---

### Anthropic-Compatible (`anthropic_compat`)

For providers that follow Anthropic's API format.

#### Example

```yaml
providers:
  - name: anthropic-compat-provider
    type: anthropic_compat
    base_url: https://api.example.com/v1
    enabled: true
    accounts:
      - name: account-1
        api_key: ${API_KEY}
```

---

### OpenRouter (`openrouter`)

OpenRouter is OpenAI-compatible but requires custom headers.

#### Example

```yaml
providers:
  - name: openrouter
    type: openrouter
    base_url: https://openrouter.ai/api/v1
    enabled: true
    accounts:
      - name: account-1
        api_key: ${OPENROUTER_API_KEY}
```

OpenRouter automatically adds the `HTTP-Referer` and `X-Title` headers.

---

## Environment Variables

API keys support environment variable expansion using `${ENV_VAR}` syntax:

```yaml
providers:
  - name: openai
    type: openai_compat
    base_url: https://api.openai.com/v1
    accounts:
      - name: account-1
        api_key: ${OPENAI_API_KEY}
```

Set environment variables before starting the server:

```bash
export OPENAI_API_KEY=sk-xxx
./router serve --config ./config/config.yaml
```

Or use a `.env` file:

```bash
source .env
./router serve --config ./config/config.yaml
```

---

## Multi-Account Configuration

For providers that support multiple API keys, configure multiple accounts:

```yaml
providers:
  - name: openai
    type: openai_compat
    base_url: https://api.openai.com/v1
    accounts:
      - name: account-1
        api_key: ${OPENAI_API_KEY_1}
      - name: account-2
        api_key: ${OPENAI_API_KEY_2}
      - name: account-3
        api_key: ${OPENAI_API_KEY_3}
```

The router uses round-robin selection across accounts automatically.

### Account Health Check

Check individual account health:

```bash
curl -H "Authorization: Bearer <your-api-key>" \
  http://localhost:1988/api/providers/openai/accounts/account-1/health
```

Response:
```json
{
  "provider": "openai",
  "account": "account-1",
  "status": "healthy"
}
```

---

## Route Configuration

Once providers are configured, define routes to map model names to providers:

```yaml
routes:
  gpt-4:
    strategy: tier
    targets:
      - provider: openai
        model: gpt-4
        tier: primary

  claude-3:
    strategy: tier
    targets:
      - provider: anthropic
        model: claude-3-sonnet-20240229
        tier: primary
```

### Route Strategies

- `tier`: Use targets in order by tier (primary → secondary → tertiary)
- `round-robin`: Rotate through targets
- `priority`: Use first available target

---

## Model Aliases

Create short aliases for models:

```yaml
model_aliases:
  g4:
    provider: openai
    model: gpt-4
  c3:
    provider: anthropic
    model: claude-3-sonnet-20240229
```

Use aliases in requests:
```bash
curl -X POST http://localhost:1988/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"g4","messages":[{"role":"user","content":"Hello"}]}'
```

---

## Error Classification

Configure error classification rules per provider:

```yaml
errors:
  openai:
    retryable:
      - 429
      - 500
      - 502
      - 503
      - 504
    non_retryable:
      - 400
      - 401
      - 403
      - 404
```

---

## Testing Provider Configuration

Validate configuration:

```bash
./router validate --config ./config/config.yaml
```

List providers:

```bash
./router providers --config ./config/config.yaml
```

List routes:

```bash
./router routes --config ./config/config.yaml
```

---

## Adding a New Provider Type

If you need to add a new provider type not currently supported:

1. Create a new adapter in `internal/providers/` (e.g., `myprovider.go`)
2. Implement the `Adapter` interface from `internal/providers/providers.go`
3. Add the provider type to the validation in `internal/config/config.go`
4. Register the provider in `internal/app/app.go`
5. Add tests in `internal/providers/providers_test.go`

### Adapter Interface

```go
type Adapter interface {
    Name() string
    ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error)
    StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error)
    ClassifyError(err error) ErrorClassification
    FetchUsage(ctx context.Context, model string) (*Usage, error)
}
```

---

## Troubleshooting

### Provider Not Found

Error: `provider not found`

- Check provider name in config matches route target
- Verify provider is enabled (`enabled: true`)
- Use `./router providers` to list configured providers

### Authentication Failed

Error: `401 Unauthorized`

- Verify API key is correct
- Check environment variable is set
- Ensure `${ENV_VAR}` syntax is correct

### Rate Limited

Error: `429 Too Many Requests`

- Configure multiple accounts for round-robin
- Check cooldown settings in `retry` config
- Review error classification rules

### Connection Timeout

Error: `connection timeout`

- Verify `base_url` is correct
- Check network connectivity
- Increase timeout in `server.request_timeout_seconds`

---

## Best Practices

1. **Use Environment Variables**: Never hardcode API keys
2. **Multiple Accounts**: Configure multiple accounts for high-volume usage
3. **Model Aliases**: Use short aliases for frequently used models
4. **Error Classification**: Customize error rules for your providers
5. **Health Checks**: Monitor account health with the API
6. **Log Analysis**: Use `./router logs` to troubleshoot provider issues

---

## Examples

### Complete Configuration Example

```yaml
server:
  host: "0.0.0.0"
  port: 1988
  api_key: ${ROUTER_API_KEY}

providers:
  - name: openai
    type: openai_compat
    base_url: https://api.openai.com/v1
    enabled: true
    accounts:
      - name: account-1
        api_key: ${OPENAI_API_KEY_1}
      - name: account-2
        api_key: ${OPENAI_API_KEY_2}

  - name: anthropic
    type: anthropic
    base_url: https://api.anthropic.com
    enabled: true
    accounts:
      - name: account-1
        api_key: ${ANTHROPIC_API_KEY}

  - name: groq
    type: openai_compat
    base_url: https://api.groq.com/openai/v1
    enabled: true
    accounts:
      - name: account-1
        api_key: ${GROQ_API_KEY}

routes:
  gpt-4:
    strategy: tier
    targets:
      - provider: openai
        model: gpt-4
        tier: primary

  claude-3:
    strategy: tier
    targets:
      - provider: anthropic
        model: claude-3-sonnet-20240229
        tier: primary

  llama3:
    strategy: tier
    targets:
      - provider: groq
        model: llama3-70b-8192
        tier: primary

model_aliases:
  g4:
    provider: openai
    model: gpt-4
  c3:
    provider: anthropic
    model: claude-3-sonnet-20240229
```

---

## Support

For issues with specific providers:
- Check the provider's API documentation
- Review logs: `./router logs --db-path ./data/router.db --provider <provider-name>`
- Validate config: `./router validate --config ./config/config.yaml`
