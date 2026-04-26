# Local Run Guide

This guide explains how to run 9router-go locally for development and testing.

## Prerequisites

- Go 1.24 or later
- Configuration file (YAML)

## Installation

```bash
# Clone the repository
git clone <repository-url>
cd 9router-go

# Install dependencies
go mod download
```

## Configuration

Create a configuration file based on the example:

```bash
cp config/config.example.yaml config/config.yaml
```

Edit `config/config.yaml` to add your provider credentials and configure routes.

### Example Configuration

```yaml
server:
  host: "127.0.0.1"
  port: 20128
  api_key: "your-api-key-here"
  read_timeout_seconds: 30
  write_timeout_seconds: 30

providers:
  - name: "openai-primary"
    type: "openai_compat"
    enabled: true
    base_url: "https://api.openai.com/v1"
    api_key: "sk-..."
    format: "openai"
    tier: "primary"
    headers:
      "X-Custom-Header": "value"

routes:
  gpt-4:
    strategy: "round-robin"
    targets:
      - provider: "openai-primary"
        model: "gpt-4"
        tier: "primary"

model_aliases:
  gpt4:
    provider: "openai-primary"
    model: "gpt-4"
```

## Running the Server

```bash
# Start the server
go run ./cmd/router serve --config ./config/config.yaml
```

The server will start on the configured host and port (default: `http://127.0.0.1:20128`).

## Testing Endpoints

### Health Check

```bash
curl http://127.0.0.1:20128/healthz
```

Response:
```json
{"status":"ok"}
```

### List Models

```bash
curl -H "Authorization: Bearer your-api-key-here" \
  http://127.0.0.1:20128/v1/models
```

### Chat Completions (OpenAI Format)

```bash
curl -H "Authorization: Bearer your-api-key-here" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello!"}]
  }' \
  http://127.0.0.1:20128/v1/chat/completions
```

### Messages (Claude Format)

```bash
curl -H "Authorization: Bearer your-api-key-here" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello!"}]
  }' \
  http://127.0.0.1:20128/v1/messages
```

## Development

### Building

```bash
go build -o bin/router ./cmd/router
```

### Running Tests

```bash
go test ./...
```

### Code Structure

```
cmd/router/          - CLI entrypoint
internal/
  api/             - HTTP handlers and middleware
  app/             - Application wiring
  config/          - Configuration loader
  providers/       - Provider adapters
  router/          - Routing engine
  translator/      - Format translation layer
  storage/         - SQLite persistence
```

## Troubleshooting

### Port Already in Use

Change the port in your config file:

```yaml
server:
  port: 20129
```

### Provider Authentication Errors

Verify your API keys in the configuration and ensure the provider's base URL is correct.

### Configuration Validation Errors

Check that your YAML is valid and all required fields are present. The example config at `config/config.example.yaml` shows the expected structure.
