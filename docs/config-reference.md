# Configuration Reference

Complete reference for 9router-go configuration file (YAML).

## Configuration File Location

Default: `./config/config.yaml`

Can be specified via command line:
```bash
go run ./cmd/router serve --config /path/to/config.yaml
```

## Configuration Structure

```yaml
server:
  # Server configuration
  host: "127.0.0.1"
  port: 20128
  api_key: "your-api-key"
  read_timeout_seconds: 30
  write_timeout_seconds: 30
  request_timeout_seconds: 120

logging:
  # Logging configuration
  level: "info"
  format: "json"

storage:
  # SQLite database configuration
  path: "./9router.db"

retry:
  # Retry configuration
  max_attempts: 3
  initial_backoff_ms: 1000
  max_backoff_ms: 10000

errors:
  # Error classification rules
  text_rules:
    - text: "rate limit"
      backoff: true
  status_rules:
    - status: 429
      backoff: true

settings:
  # Global settings
  combo_strategy: "fallback"
  outbound_proxy: ""

model_aliases:
  # Model name aliases
  gpt4:
    provider: "openai-primary"
    model: "gpt-4"

routes:
  # Route configurations
  gpt-4:
    strategy: "round-robin"
    targets:
      - provider: "openai-primary"
        model: "gpt-4"
        tier: "primary"

providers:
  # Provider configurations
  - name: "openai-primary"
    type: "openai_compat"
    enabled: true
    base_url: "https://api.openai.com/v1"
    api_key: "sk-..."
    format: "openai"
    tier: "primary"
    headers:
      "X-Custom-Header": "value"
```

## Section Details

### server

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| host | string | "127.0.0.1" | Server bind address |
| port | int | 20128 | Server port |
| api_key | string | "" | API key for authentication (empty = no auth) |
| read_timeout_seconds | int | 30 | HTTP read timeout |
| write_timeout_seconds | int | 30 | HTTP write timeout |
| request_timeout_seconds | int | 120 | Overall request timeout |

### logging

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| level | string | "info" | Log level: debug, info, warn, error |
| format | string | "json" | Log format: json, console |

### storage

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| path | string | "./9router.db" | SQLite database file path |

### retry

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| max_attempts | int | 3 | Maximum retry attempts per target |
| initial_backoff_ms | int | 1000 | Initial backoff in milliseconds |
| max_backoff_ms | int | 10000 | Maximum backoff in milliseconds |

### errors

#### text_rules

Array of text-based error classification rules.

| Field | Type | Description |
|-------|------|-------------|
| text | string | Text pattern to match in error message |
| backoff | boolean | Whether to retry on this error |

#### status_rules

Array of HTTP status code-based error classification rules.

| Field | Type | Description |
|-------|------|-------------|
| status | int | HTTP status code |
| backoff | boolean | Whether to retry on this status |

### settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| combo_strategy | string | "fallback" | Default combo strategy: fallback, round-robin |
| outbound_proxy | string | "" | Outbound proxy URL |

### model_aliases

Map of model alias names to provider/model combinations.

| Field | Type | Description |
|-------|------|-------------|
| provider | string | Provider name |
| model | string | Target model name |

### routes

Map of route names to route configurations.

#### RouteConfig

| Field | Type | Description |
|-------|------|-------------|
| strategy | string | Combo strategy: fallback, round-robin |
| targets | array | Array of target configurations |

#### RouteTarget

| Field | Type | Description |
|-------|------|-------------|
| provider | string | Provider name |
| model | string | Model name |
| tier | string | Tier: primary, secondary, tertiary |

### providers

Array of provider configurations.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| name | string | Yes | Unique provider identifier |
| type | string | Yes | Provider type: openai_compat, anthropic |
| enabled | boolean | Yes | Whether provider is active |
| base_url | string | Yes | Provider API base URL |
| api_key | string | Yes | Provider API key |
| format | string | No | Default format: openai, anthropic |
| tier | string | No | Tier for ordering: primary, secondary, tertiary |
| headers | map | No | Custom HTTP headers |

## Example Configurations

### Minimal Configuration

```yaml
server:
  port: 20128

providers:
  - name: "openai"
    type: "openai_compat"
    enabled: true
    base_url: "https://api.openai.com/v1"
    api_key: "sk-..."

routes:
  gpt-4:
    strategy: "fallback"
    targets:
      - provider: "openai"
        model: "gpt-4"
```

### Multi-Provider with Fallback

```yaml
providers:
  - name: "openai-primary"
    type: "openai_compat"
    enabled: true
    base_url: "https://api.openai.com/v1"
    api_key: "sk-..."
    tier: "primary"
  - name: "openai-backup"
    type: "openai_compat"
    enabled: true
    base_url: "https://api.openai.com/v1"
    api_key: "sk-..."
    tier: "secondary"

routes:
  gpt-4:
    strategy: "fallback"
    targets:
      - provider: "openai-primary"
        model: "gpt-4"
        tier: "primary"
      - provider: "openai-backup"
        model: "gpt-4"
        tier: "secondary"
```

### Round-Robin Load Balancing

```yaml
providers:
  - name: "provider-1"
    type: "openai_compat"
    enabled: true
    base_url: "https://api.provider1.com"
    api_key: "sk-..."
  - name: "provider-2"
    type: "openai_compat"
    enabled: true
    base_url: "https://api.provider2.com"
    api_key: "sk-..."

routes:
  gpt-4:
    strategy: "round-robin"
    targets:
      - provider: "provider-1"
        model: "gpt-4"
      - provider: "provider-2"
        model: "gpt-4"
```

## Validation

The configuration is validated on startup. Common errors:

- **No enabled providers**: At least one provider must be enabled
- **Invalid provider type**: Type must be `openai_compat` or `anthropic`
- **Missing required fields**: Check that all required fields are present
- **Invalid strategy**: Strategy must be `fallback` or `round-robin`
