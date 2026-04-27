# Provider Setup Guide

This guide explains how to configure AI providers for NusaNexus Router.

## Supported Provider Types

NusaNexus Router supports the following provider types:

- **openai_compat** - OpenAI-compatible APIs (OpenAI, Azure OpenAI, Together, etc.)
- **anthropic** - Anthropic Claude API

## OpenAI-Compatible Providers

### OpenAI

```yaml
providers:
  - name: "openai-primary"
    type: "openai_compat"
    enabled: true
    base_url: "https://api.openai.com/v1"
    api_key: "sk-..."
    format: "openai"
    tier: "primary"
```

### Azure OpenAI

```yaml
providers:
  - name: "azure-openai"
    type: "openai_compat"
    enabled: true
    base_url: "https://your-resource.openai.azure.com"
    api_key: "your-azure-api-key"
    format: "openai"
    tier: "primary"
    headers:
      "api-key": "your-azure-api-key"
```

### Together AI

```yaml
providers:
  - name: "together-ai"
    type: "openai_compat"
    enabled: true
    base_url: "https://api.together.xyz/v1"
    api_key: "your-together-api-key"
    format: "openai"
    tier: "secondary"
```

## Anthropic

```yaml
providers:
  - name: "anthropic-primary"
    type: "anthropic"
    enabled: true
    base_url: "https://api.anthropic.com"
    api_key: "sk-ant-..."
    format: "anthropic"
    tier: "primary"
    headers:
      "anthropic-version": "2023-06-01"
```

## Provider Configuration Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| name | string | Yes | Unique identifier for this provider |
| type | string | Yes | Provider type: `openai_compat` or `anthropic` |
| enabled | boolean | Yes | Whether this provider is active |
| base_url | string | Yes | Base URL for the provider API |
| api_key | string | Yes | API authentication key |
| format | string | No | Default request/response format: `openai` or `anthropic` |
| tier | string | No | Tier for fallback ordering: `primary`, `secondary`, `tertiary` |
| headers | map | No | Custom HTTP headers to include in requests |

## Getting API Keys

### OpenAI

1. Go to [platform.openai.com](https://platform.openai.com)
2. Navigate to API Keys
3. Create a new API key
4. Copy the key (starts with `sk-`)

### Anthropic

1. Go to [console.anthropic.com](https://console.anthropic.com)
2. Navigate to API Keys
3. Create a new API key
4. Copy the key (starts with `sk-ant-`)

### Azure OpenAI

1. Go to Azure Portal
2. Create an OpenAI resource
3. Get the API key from the resource's Keys section
4. Get the base URL from the resource's Endpoint section

## Testing Provider Configuration

After configuring a provider, test it:

```bash
# Start the server
go run ./cmd/router serve --config ./config/config.yaml

# Test the provider directly
curl -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-3.5-turbo","messages":[{"role":"user","content":"test"}]}' \
  http://127.0.0.1:20128/v1/chat/completions
```

## Troubleshooting

### Authentication Errors

- Verify the API key is correct and has sufficient permissions
- Check that the base URL is correct for your provider
- Ensure the provider type matches the API format

### Rate Limiting

- Configure retry settings in the config to handle rate limits
- Set appropriate tier values to prioritize providers
- Consider adding multiple providers for redundancy

### Custom Headers

Some providers require specific headers. Use the `headers` field:

```yaml
providers:
  - name: "custom-provider"
    type: "openai_compat"
    enabled: true
    base_url: "https://api.example.com"
    api_key: "your-key"
    headers:
      "X-Custom-Header": "value"
      "X-API-Version": "v1"
```

## Best Practices

1. **Use environment variables for secrets** - Don't hardcode API keys in config files
2. **Configure multiple providers** - Set up fallback chains for reliability
3. **Use appropriate tiers** - Primary for production, secondary for backup
4. **Monitor usage** - Track token usage and costs per provider
5. **Test configuration** - Validate config before deploying to production
