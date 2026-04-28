# GitHub Copilot API Proxy Guide

This guide explains how to use NusaNexus Router as a GitHub Copilot API proxy, allowing you to route Copilot requests to multiple AI providers with fallback and translation support.

## Overview

GitHub Copilot uses a proprietary API that is compatible with OpenAI's chat completions format. NusaNexus Router can act as a transparent proxy for Copilot requests, enabling:

- Multi-provider routing (OpenAI, Anthropic, etc.)
- Automatic fallback between providers
- Format translation between providers
- Usage tracking and quota management
- Rate limiting per client

## Configuration

### Basic Setup

To use NusaNexus Router as a Copilot proxy, configure a route that maps Copilot's model names to your target providers:

```yaml
routes:
  copilot-gpt-4:
    model: "gpt-4"
    provider: "openai-primary"
    fallback: ["openai-backup"]
    weight: 100
```

### Provider Configuration

Configure your AI providers in the `providers` section:

```yaml
providers:
  - name: openai-primary
    type: openai
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    enabled: true
```

### Server Configuration

Set up the server to listen on the appropriate port:

```yaml
server:
  host: "127.0.0.1"
  port: 1988
  api_key: "your-copilot-api-key"
```

## Usage

### Pointing Copilot to the Proxy

Configure your Copilot client to use the proxy endpoint instead of the default GitHub Copilot API. The exact method depends on your Copilot client:

**For VS Code Copilot:**
1. Install a proxy tool (like mitmproxy) or configure network settings
2. Set the proxy to route `https://api.githubcopilot.com` to `http://127.0.0.1:1988`
3. Configure Copilot to use the proxy

**For command-line tools:**
```bash
export COPILOT_API_BASE=http://127.0.0.1:1988
```

### API Compatibility

The router's `/v1/chat/completions` endpoint is compatible with GitHub Copilot's API format. Copilot requests are automatically:

- Routed to the configured provider
- Translated between formats if needed
- Logged for usage tracking
- Subject to rate limiting and circuit breaking

### Model Mapping

GitHub Copilot uses internal model names. Map these to your provider models in the `model_aliases` section:

```yaml
model_aliases:
  copilot-gpt-4: "gpt-4"
  copilot-gpt-3.5-turbo: "gpt-3.5-turbo"
```

## Features

### Multi-Provider Fallback

Configure fallback providers to ensure high availability:

```yaml
routes:
  copilot-gpt-4:
    model: "gpt-4"
    provider: "openai-primary"
    fallback: ["anthropic-backup", "openai-backup"]
```

### Rate Limiting

Protect your provider quotas with per-client rate limiting:

```yaml
server:
  rate_limit:
    enabled: true
    requests_per_minute: 60
```

### Circuit Breaking

Automatic circuit breaking prevents cascading failures when providers are unhealthy:

```yaml
# Circuit breaker is automatically enabled
# Thresholds: 5 failures to open, 5-minute timeout
```

### Usage Tracking

All Copilot requests are logged with:
- Request ID
- Model used
- Provider selected
- Response time
- Success/failure status

## Advanced Configuration

### Custom Models

Define custom models for Copilot:

```yaml
custom_models:
  copilot-custom:
    provider: "openai-primary"
    model: "gpt-4-turbo-preview"
```

### Error Classification

Customize error handling for Copilot-specific errors:

```yaml
errors:
  status_rules:
    - status: 403
      backoff: true
  text_rules:
    - text: "quota exceeded"
      backoff: true
```

## Monitoring

### Health Checks

Monitor provider health:

```bash
curl http://127.0.0.1:1988/healthz
curl http://127.0.0.1:1988/api/providers/openai-primary/health?deep=true
```

### Metrics

Access request metrics:

```bash
curl http://127.0.0.1:1988/metrics
```

### Logs

View request logs:

```bash
curl http://127.0.0.1:1988/api/logs
```

## Security Considerations

- Use strong API keys for both the router and provider authentication
- Enable TLS in production (configure reverse proxy like nginx)
- Review rate limiting settings based on your usage patterns
- Monitor logs for suspicious activity

## Troubleshooting

### Copilot Cannot Connect

1. Verify the router is running: `curl http://127.0.0.1:1988/healthz`
2. Check Copilot client configuration
3. Review router logs for connection errors

### Provider Errors

1. Check provider health: `curl http://127.0.0.1:1988/api/providers/{name}/health?deep=true`
2. Verify API keys are valid
3. Check provider quota limits

### Rate Limiting

1. Adjust `requests_per_minute` in config
2. Check circuit breaker status
3. Review provider-specific rate limits

## Example Configuration

A complete example configuration for Copilot proxy:

```yaml
server:
  host: "127.0.0.1"
  port: 1988
  api_key: "your-router-api-key"
  admin_api_keys:
    - "admin-key"
  rate_limit:
    enabled: true
    requests_per_minute: 100

providers:
  - name: openai-primary
    type: openai
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    enabled: true
    accounts:
      - name: account1
        api_key: ${OPENAI_ACCOUNT1_KEY}
        weight: 100

routes:
  copilot-gpt-4:
    model: "gpt-4"
    provider: "openai-primary"
    fallback: []
    weight: 100

model_aliases:
  copilot-gpt-4: "gpt-4"

retry:
  max_attempts: 3
  backoff_ms: 1000
  backoff_multiplier: 2
```

## Limitations

- Streaming responses are not currently supported for Copilot
- Some Copilot-specific features may not be translated correctly
- Token counting may vary between providers
- Custom Copilot extensions may not be compatible

## Future Enhancements

Planned improvements for Copilot proxy support:

- Full streaming support
- Copilot-specific format translation
- Token-aware quota management
- Copilot extension API support
- Advanced usage analytics
