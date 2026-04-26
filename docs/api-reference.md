# API Reference

9router-go provides OpenAI-compatible, Anthropic-compatible, and OpenAI Responses-compatible endpoints for unified access to multiple AI providers.

---

## Base URL

```
http://localhost:20128
```

Default port: `20128` (configurable via `server.port` in config)

---

## Authentication

All protected endpoints require Bearer token authentication:

```http
Authorization: Bearer <your-api-key>
```

The API key is configured in `config.yaml` under `server.api_key`.

---

## Endpoints

### OpenAI-Compatible Endpoints

#### `POST /v1/chat/completions`

Chat completion endpoint (OpenAI format).

**Request:**

```json
{
  "model": "gpt-4",
  "messages": [
    {"role": "user", "content": "Hello!"}
  ],
  "stream": false,
  "temperature": 0.7,
  "max_tokens": 1000
}
```

**Streaming:** Set `"stream": true` to receive Server-Sent Events (SSE) stream.

**Response:**

```json
{
  "id": "chatcmpl-123",
  "object": "chat.completion",
  "created": 1699000000,
  "model": "gpt-4",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Hello! How can I help you?"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 10,
    "completion_tokens": 20,
    "total_tokens": 30
  }
}
```

#### `POST /v1/responses`

OpenAI Responses / Codex format endpoint. Internally translated to chat completion.

**Request:** Same as `/v1/chat/completions`

**Response:** Same as `/v1/chat/completions`

#### `GET /v1/models`

List available models (routes, aliases, provider combinations).

**Response:**

```json
{
  "object": "list",
  "data": [
    {"id": "gpt-4", "object": "model", "type": "route"},
    {"id": "claude-3", "object": "model", "type": "route"},
    {"id": "openai/*", "object": "model", "type": "provider"}
  ]
}
```

---

## Anthropic-Compatible Endpoints

#### `POST /v1/messages`

Anthropic Messages API format endpoint.

**Request:**

```json
{
  "model": "claude-3-sonnet-20240229",
  "messages": [
    {"role": "user", "content": "Hello!"}
  ],
  "max_tokens": 1024
}
```

**Response:** Translated to Claude format.

---

## Admin API Endpoints

All admin endpoints require authentication.

### Providers

#### `GET /api/providers`

List all configured providers.

**Response:**

```json
{
  "providers": [
    {
      "name": "openai",
      "type": "openai_compat",
      "base_url": "https://api.openai.com/v1",
      "enabled": true
    }
  ],
  "count": 1
}
```

#### `GET /api/providers/{name}/health`

Check provider health status.

**Response:**

```json
{
  "name": "openai",
  "status": "healthy",
  "enabled": true
}
```

#### `GET /api/providers/{name}/accounts/{account}/health`

Check individual account health (for multi-account providers).

**Response:**

```json
{
  "provider": "openai",
  "account": "account-1",
  "status": "healthy"
}
```

### Routes / Combos

#### `GET /api/combos`

List all route configurations (combos).

**Response:**

```json
{
  "combos": [
    {
      "name": "gpt-4",
      "strategy": "tier",
      "targets": [
        {"provider": "openai", "model": "gpt-4", "tier": "primary"}
      ]
    }
  ],
  "count": 1
}
```

### API Keys

#### `GET /api/keys`

List configured API keys.

**Response:**

```json
{
  "keys": [
    {"id": "default", "api_key": "sk-****"}
  ],
  "count": 1
}
```

### Model Aliases

#### `GET /api/models/alias`

List model aliases.

**Response:**

```json
{
  "aliases": [
    {"name": "g4", "provider": "openai", "model": "gpt-4"}
  ],
  "count": 1
}
```

### Settings

#### `GET /api/settings`

Get current settings.

**Response:**

```json
{
  "settings": {
    "request_timeout_seconds": 120
  }
}
```

### Logs

#### `GET /api/logs`

Query request logs with filtering and pagination.

**Query Parameters:**

- `limit` (int): Number of logs to return (default: 50, max: 1000)
- `offset` (int): Pagination offset (default: 0)
- `provider` (string): Filter by provider name
- `model` (string): Filter by model
- `status` (string): Filter by status (success/error)
- `start_time` (int64): Filter by start time (Unix timestamp)
- `end_time` (int64): Filter by end time (Unix timestamp)

**Response:**

```json
{
  "logs": [
    {
      "request_id": "req-123",
      "model": "gpt-4",
      "provider": "openai",
      "target_model": "gpt-4",
      "status": "success",
      "start_time": 1699000000,
      "end_time": 1699000010,
      "duration_ms": 100,
      "prompt_tokens": 10,
      "completion_tokens": 20,
      "total_tokens": 30
    }
  ],
  "total": 100,
  "limit": 50,
  "offset": 0
}
```

### Usage

#### `GET /api/usage`

Get usage summary (in-memory metrics).

**Response:**

```json
{
  "usage": {
    "total_requests": 1000,
    "successful_requests": 950,
    "failed_requests": 50,
    "provider_usage": {
      "openai": 800,
      "anthropic": 200
    }
  }
}
```

---

## System Endpoints

### Health Checks

#### `GET /healthz`

Liveness probe - checks if server is running.

**Response:**

```json
{
  "status": "ok"
}
```

#### `GET /readyz`

Readiness probe - checks if server is ready to accept requests.

**Response:**

```json
{
  "status": "ready"
}
```

### Metrics

#### `GET /metrics`

Prometheus-formatted metrics endpoint.

**Response (text/plain):**

```
# HELP router_requests_total Total number of requests
# TYPE router_requests_total counter
router_requests_total 1000
```

---

## Error Responses

All errors follow OpenAI error format:

```json
{
  "error": {
    "message": "Error description",
    "type": "error_type",
    "code": "error_code"
  }
}
```

**Common Error Types:**

- `invalid_request_error`: Invalid request parameters
- `authentication_error`: Invalid API key
- `rate_limit_error`: Rate limited by provider
- `api_error`: Provider API error
- `internal_error`: Internal server error

---

## Streaming

When `"stream": true` in the request, responses are sent via Server-Sent Events (SSE):

```
data: {"id":"...","choices":[{"delta":{"content":"Hello"}}]}

data: {"id":"...","choices":[{"delta":{"content":" world"}}]}

data: {"id":"...","choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

Each SSE event contains a JSON chunk with the same structure as non-streaming responses.

---

## CORS

CORS is configurable via `server.cors` in config:

```yaml
server:
  cors:
    allowed_origins: ["https://example.com"]
    allowed_methods: ["GET", "POST", "OPTIONS"]
    allowed_headers: ["Authorization", "Content-Type"]
    allow_credentials: false
    max_age_seconds: 86400
```

If `allowed_origins` is empty, CORS is disabled (no CORS headers set).

---

## CLI Commands

### `./router serve`

Start the server.

```bash
./router serve --config ./config/config.yaml
```

### `./router validate`

Validate configuration file.

```bash
./router validate --config ./config/config.yaml
```

### `./router providers`

List configured providers.

```bash
./router providers --config ./config/config.yaml
```

### `./router routes`

List routes and model aliases.

```bash
./router routes --config ./config/config.yaml
```

### `./router logs`

Tail request logs from SQLite database.

```bash
./router logs --db-path ./data/9router.db --limit 50 --provider openai
```

Options:
- `--db-path`: Path to SQLite database
- `--limit`: Number of logs to show (default: 50)
- `--provider`: Filter by provider
- `--model`: Filter by model
- `--status`: Filter by status
- `--follow`: Follow mode (poll for new logs)

---

## Version

Get version information:

```bash
./router version
```

Output:
```
9router-go version 0.1.0
build time: 2026-04-26T10:00:00Z
git commit: abc123def456
```
