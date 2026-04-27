# Quick Start Guide

## One-Line Install (Recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/edodoyokz/ai-go-router/main/install.sh | bash
```

Or with explicit shell:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/edodoyokz/ai-go-router/main/install.sh)
```

## What Gets Installed

- **Binary:** `~/.local/bin/router`
- **Config:** `~/.config/router/config.yaml`
- **Data:** `~/.local/share/router/` (SQLite database)
- **Systemd service** (Linux only): `~/.config/systemd/user/router.service`

## First Run

### 1. Set Provider Keys

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
export OPENAI_API_KEY="sk-..."
```

Or edit `~/.config/router/config.yaml` directly.

### 2. Validate Config

```bash
router validate --config ~/.config/router/config.yaml
```

### 3. Start the Server

**Option A: Direct run**

```bash
router serve --config ~/.config/router/config.yaml
```

**Option B: Systemd (Linux)**

```bash
systemctl --user enable router
systemctl --user start router
systemctl --user status router
```

### 4. Test the Endpoint

```bash
curl http://127.0.0.1:20128/healthz
```

Expected response:

```json
{
  "status": "ok",
  "uptime_seconds": 5,
  "providers": 1,
  "enabled_providers": 1
}
```

### 5. Auto-Configure Tools (Optional)

Automatically configure Cursor, VS Code Continue, Claude Code, and OpenAI CLI:

```bash
router setup --config ~/.config/router/config.yaml
```

## Test a Request

```bash
curl http://127.0.0.1:20128/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk_router_local_dev" \
  -d '{
    "model": "fast",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## Next Steps

- **Read the docs:** https://github.com/edodoyokz/ai-go-router/blob/main/docs/deployment.md
- **Configure providers:** https://github.com/edodoyokz/ai-go-router/blob/main/docs/provider-guide.md
- **Set up routes:** https://github.com/edodoyokz/ai-go-router/blob/main/docs/config-reference.md
- **Join discussions:** https://github.com/edodoyokz/ai-go-router/discussions

## Troubleshooting

### "Config validation failed: at least one provider must be enabled"

**Problem:** No provider is enabled in `config.yaml`.

**Solution:**

1. Edit `~/.config/router/config.yaml`
2. Set `enabled: true` for at least one provider
3. Set the corresponding API key environment variable or directly in config
4. Run `router validate --config ~/.config/router/config.yaml` again

### "provider[0].api_key or accounts is required when enabled"

**Problem:** Provider is enabled but no API key is set.

**Solution:**

```bash
# Option 1: Set environment variable
export ANTHROPIC_API_KEY="your-key-here"

# Option 2: Edit config.yaml directly
providers:
  - name: anthropic
    type: anthropic
    base_url: https://api.anthropic.com
    api_key: sk-ant-your-actual-key-here  # ⚠️ Not recommended for production
    enabled: true
```

### "Connection refused" on http://127.0.0.1:20128

**Problem:** Router is not running.

**Solution:**

```bash
# Check if service is running
systemctl --user status router

# Or start it manually
router serve --config ~/.config/router/config.yaml
```

### "Port 20128 already in use"

**Problem:** Another process is using port 20128.

**Solution:**

```bash
# Find what's using the port
lsof -i :20128

# Or change the port in config.yaml
server:
  port: 20129  # Use a different port
```

### "Binary not found" after install

**Problem:** `~/.local/bin` is not in your PATH.

**Solution:**

```bash
# Add to your shell profile (~/.bashrc, ~/.zshrc, etc.)
export PATH="$HOME/.local/bin:$PATH"

# Then reload
source ~/.bashrc  # or ~/.zshrc
```

## Uninstall

```bash
# Stop the service
systemctl --user stop router

# Remove files
rm ~/.local/bin/router
rm -rf ~/.config/router
rm -rf ~/.local/share/router
rm ~/.config/systemd/user/router.service

# Reload systemd
systemctl --user daemon-reload
```

## Getting Help

- **Issues:** https://github.com/edodoyokz/ai-go-router/issues
- **Discussions:** https://github.com/edodoyokz/ai-go-router/discussions
- **Documentation:** https://github.com/edodoyokz/ai-go-router/tree/main/docs
