# Configuration Reference

Complete reference for 9router-go configuration file (YAML).

## Configuration File Location

Default: `./config/config.example.yaml`

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
  admin_api_keys: []
  read_timeout_seconds: 30
  write_timeout_seconds: 30
  request_timeout_seconds: 120
  idle_timeout_seconds: 60
  read_header_timeout_seconds: 10
  max_header_bytes: 1048576
  cors:
    allowed_origins: []
    allowed_methods: ["GET", "POST", "OPTIONS"]
    allowed_headers: ["Content-Type", "Authorization"]
    allow_credentials: false
    max_age_seconds: 600
  rate_limit:
    enabled: false
    requests_per_minute: 60

logging:
  # Logging configuration
  level: "info"
  debug: false
  json_mode: true

storage:
  # SQLite database configuration
  sqlite_path: "./9router.db"

retry:
  # Retry configuration
  max_attempts: 3
  initial_backoff_ms: 1000
  max_backoff_ms: 10000
  max_cooldown_ms: 300000

errors:
  # Error classification rules
  text_rules:
    - text: "rate limit"
      backoff: true
      cooldown_ms: 60000
  status_rules:
    - status: 429
      backoff: true
      cooldown_ms: 60000

settings:
  # Global settings
  combo_strategy: "fallback"
  outbound_proxy_enabled: false
  outbound_proxy_url: ""
  native_passthrough: false
  thinking:
    enabled: false
    max_tokens: 20000
    include_reasoning: false

model_aliases:
  # Model name aliases
  gpt4:
    provider: "openai-primary"
    model: "gpt-4"

custom_models:
  # Custom model definitions
  custom-gpt4:
    provider: "openai-primary"
    model: "gpt-4-turbo"
    description: "Custom GPT-4 Turbo variant"

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
    accounts:
      - name: "account-1"
        api_key: "sk-..."
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
| admin_api_keys | array | [] | Additional admin API keys |
| read_timeout_seconds | int | 30 | HTTP read timeout |
| write_timeout_seconds | int | 30 | HTTP write timeout |
| request_timeout_seconds | int | 120 | Overall request timeout |
| idle_timeout_seconds | int | 60 | Idle connection timeout |
| read_header_timeout_seconds | int | 10 | HTTP header read timeout |
| max_header_bytes | int | 1048576 | Maximum header size in bytes |
| cors | object | CORS config | CORS configuration (see below) |
| rate_limit | object | Rate limit config | Rate limiting configuration (see below) |

#### cors

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| allowed_origins | array | [] | Allowed CORS origins |
| allowed_methods | array | [] | Allowed HTTP methods |
| allowed_headers | array | [] | Allowed HTTP headers |
| allow_credentials | bool | false | Allow credentials in CORS |
| max_age_seconds | int | 600 | CORS preflight cache duration |

#### rate_limit

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| enabled | bool | false | Enable rate limiting |
| requests_per_minute | int | 60 | Max requests per minute per API key |

### logging

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| level | string | "info" | Log level: debug, info, warn, error |
| debug | bool | false | Enable debug logging |
| json_mode | bool | true | Use JSON format for logs |

### storage

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| sqlite_path | string | "./9router.db" | SQLite database file path |

### retry

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| max_attempts | int | 3 | Maximum retry attempts per target |
| initial_backoff_ms | int | 1000 | Initial backoff in milliseconds |
| max_backoff_ms | int | 10000 | Maximum backoff in milliseconds |
| max_cooldown_ms | int | 300000 | Maximum cooldown duration in milliseconds |

### errors

#### text_rules

Array of text-based error classification rules.

| Field | Type | Description |
|-------|------|-------------|
| text | string | Text pattern to match in error message |
| backoff | boolean | Whether to retry on this error |
| cooldown_ms | int | Cooldown duration in milliseconds |

#### status_rules

Array of HTTP status code-based error classification rules.

| Field | Type | Description |
|-------|------|-------------|
| status | int | HTTP status code |
| backoff | boolean | Whether to retry on this status |
| cooldown_ms | int | Cooldown duration in milliseconds |

### settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| combo_strategy | string | "fallback" | Default combo strategy: fallback, round-robin |
| outbound_proxy_enabled | bool | false | Enable outbound proxy |
| outbound_proxy_url | string | "" | Outbound proxy URL |
| native_passthrough | bool | false | Enable native passthrough mode |
| thinking.enabled | bool | false | Enable thinking tokens |
| thinking.max_tokens | int | 20000 | Maximum thinking tokens |
| thinking.include_reasoning | bool | false | Include reasoning in output |

### model_aliases

Map of model alias names to provider/model combinations.

| Field | Type | Description |
|-------|------|-------------|
| provider | string | Provider name |
| model | string | Target model name |

### custom_models

Map of custom model definitions for user-defined models.

| Field | Type | Description |
|-------|------|-------------|
| provider | string | Provider name |
| model | string | Target model name |
| description | string | Model description |

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
| api_key | string | No | Provider API key (deprecated: use accounts instead) |
| accounts | array | No | Array of account configurations for multi-account support |
| format | string | No | Default format: openai, anthropic |
| tier | string | No | Tier for ordering: primary, secondary, tertiary |
| headers | map | No | Custom HTTP headers |

#### accounts

Array of account configurations for multi-account support.

| Field | Type | Description |
|-------|------|-------------|
| name | string | Account identifier |
| api_key | string | Account API key |

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
