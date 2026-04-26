# Troubleshooting Guide

This guide helps diagnose and resolve common issues with 9router-go.

---

## Table of Contents

- [Server Issues](#server-issues)
- [Configuration Issues](#configuration-issues)
- [Provider Issues](#provider-issues)
- [Routing Issues](#routing-issues)
- [Database Issues](#database-issues)
- [Performance Issues](#performance-issues)
- [Streaming Issues](#streaming-issues)
- [Authentication Issues](#authentication-issues)

---

## Server Issues

### Server won't start

**Symptoms:** `./router serve` fails to start or exits immediately

**Solutions:**

1. Check configuration syntax:
   ```bash
   ./router validate --config ./config/config.yaml
   ```

2. Check logs:
   ```bash
   # Systemd
   sudo journalctl -u 9router -n 50

   # Docker
   docker logs 9router

   # Direct binary
   ./router serve --config ./config/config.yaml 2>&1
   ```

3. Verify port availability:
   ```bash
   # Check if port 20128 is in use
   lsof -i :20128
   netstat -tuln | grep 20128
   ```

4. Check file permissions:
   ```bash
   # Config file should be readable
   ls -la config/config.yaml

   # Database directory should be writable
   ls -la data/
   ```

### Server crashes on startup

**Symptoms:** Server starts then immediately exits

**Solutions:**

1. Check database corruption:
   ```bash
   # Try opening database directly
   sqlite3 data/9router.db "SELECT COUNT(*) FROM request_logs;"
   ```

2. Check environment variables:
   ```bash
   # Verify required env vars are set
   env | grep -E "API_KEY|OPENAI|ANTHROPIC"
   ```

3. Check for missing directories:
   ```bash
   # Create data directory if missing
   mkdir -p data
   ```

### Server not responding to requests

**Symptoms:** Health check passes but API requests timeout

**Solutions:**

1. Check timeout settings:
   ```yaml
   server:
     request_timeout_seconds: 120
     read_timeout_seconds: 125
     write_timeout_seconds: 125
   ```

2. Check provider connectivity:
   ```bash
   curl -H "Authorization: Bearer <your-api-key>" \
     http://localhost:20128/api/providers/openai/health
   ```

3. Check for rate limiting:
   ```bash
   ./router logs --db-path ./data/9router.db --status error
   ```

---

## Configuration Issues

### Config validation fails

**Symptoms:** `./router validate` returns errors

**Solutions:**

1. Check YAML syntax:
   ```bash
   # Use YAML linter
   python -c "import yaml; yaml.safe_load(open('config/config.yaml'))"
   ```

2. Check required fields:
   - `server.api_key` must be set
   - Each provider must have `name`, `type`, `base_url`
   - Each account must have `name`, `api_key`

3. Check provider type:
   ```bash
   ./router providers --config ./config/config.yaml
   ```

### Environment variables not expanding

**Symptoms:** `${ENV_VAR}` in config not replaced

**Solutions:**

1. Verify environment variable is set:
   ```bash
   echo $OPENAI_API_KEY
   ```

2. Check syntax (must be `${ENV_VAR}` not `$ENV_VAR`):
   ```yaml
   # Correct
   api_key: ${OPENAI_API_KEY}

   # Incorrect
   api_key: $OPENAI_API_KEY
   ```

3. Export before starting server:
   ```bash
   export OPENAI_API_KEY=sk-xxx
   ./router serve --config ./config/config.yaml
   ```

### CORS errors in browser

**Symptoms:** CORS errors when accessing API from web app

**Solutions:**

1. Configure CORS in config:
   ```yaml
   server:
     cors:
       allowed_origins: ["https://yourdomain.com"]
       allowed_methods: ["GET", "POST", "OPTIONS"]
       allowed_headers: ["Authorization", "Content-Type"]
   ```

2. If using wildcard origin (not recommended for production):
   ```yaml
   server:
     cors:
       allowed_origins: ["*"]
   ```

---

## Provider Issues

### Provider not found

**Symptoms:** Error "provider not found" in logs

**Solutions:**

1. List configured providers:
   ```bash
   ./router providers --config ./config/config.yaml
   ```

2. Check route configuration:
   ```bash
   ./router routes --config ./config/config.yaml
   ```

3. Verify provider name matches:
   ```yaml
   # Provider config
   providers:
     - name: openai  # Must match route target

   # Route config
   routes:
     gpt-4:
       targets:
         - provider: openai  # Must match provider name
   ```

### Authentication failed (401)

**Symptoms:** 401 errors from provider

**Solutions:**

1. Verify API key:
   ```bash
   # Test API key directly
   curl https://api.openai.com/v1/models \
     -H "Authorization: Bearer $OPENAI_API_KEY"
   ```

2. Check environment variable expansion:
   ```bash
   # View config with env vars expanded
   cat config/config.yaml
   ```

3. Check account configuration:
   ```yaml
   accounts:
     - name: account-1
       api_key: ${OPENAI_API_KEY}  # Verify this is set
   ```

4. Check account health:
   ```bash
   curl -H "Authorization: Bearer <your-api-key>" \
     http://localhost:20128/api/providers/openai/accounts/account-1/health
   ```

### Rate limited (429)

**Symptoms:** 429 errors from provider

**Solutions:**

1. Configure multiple accounts:
   ```yaml
   providers:
     - name: openai
       accounts:
         - name: account-1
           api_key: ${OPENAI_API_KEY_1}
         - name: account-2
           api_key: ${OPENAI_API_KEY_2}
   ```

2. Check cooldown settings:
   ```yaml
   retry:
     max_attempts: 3
     initial_backoff_ms: 1000
     max_backoff_ms: 10000
     max_cooldown_ms: 60000
   ```

3. Check error classification:
   ```yaml
   errors:
     openai:
       retryable:
         - 429  # Ensure 429 is marked as retryable
   ```

4. Review logs for rate limit patterns:
   ```bash
   ./router logs --db-path ./data/9router.db --provider openai --status error
   ```

### Connection timeout

**Symptoms:** Provider requests timeout

**Solutions:**

1. Verify provider URL:
   ```yaml
   base_url: https://api.openai.com/v1  # Check this is correct
   ```

2. Test connectivity:
   ```bash
   curl https://api.openai.com/v1/models
   ```

3. Increase timeout:
   ```yaml
   server:
     request_timeout_seconds: 120
     read_timeout_seconds: 125
   ```

4. Check firewall rules:
   ```bash
   # Test from server
   curl -v https://api.openai.com
   ```

---

## Routing Issues

### Model not found

**Symptoms:** Error "model not found" or "no route for model"

**Solutions:**

1. List routes:
   ```bash
   ./router routes --config ./config/config.yaml
   ```

2. Check model alias:
   ```yaml
   model_aliases:
     g4:  # Alias
       provider: openai
       model: gpt-4  # Actual model
   ```

3. Check route target:
   ```yaml
   routes:
     gpt-4:  # Route name
       targets:
         - provider: openai
           model: gpt-4  # Provider model
   ```

### Fallback not working

**Symptoms:** Primary provider fails but fallback not triggered

**Solutions:**

1. Check route strategy:
   ```yaml
   routes:
     gpt-4:
       strategy: tier  # Must be tier for fallback
   ```

2. Check tier configuration:
   ```yaml
   targets:
     - provider: openai
       model: gpt-4
       tier: primary
     - provider: anthropic
       model: claude-3-sonnet-20240229
       tier: secondary  # Must have fallback tier
   ```

3. Check error classification:
   ```yaml
   errors:
     openai:
       retryable:
         - 500
         - 502
         - 503
   ```

4. Check logs for retry attempts:
   ```bash
   ./router logs --db-path ./data/9router.db --status error
   ```

---

## Database Issues

### Database locked

**Symptoms:** "database is locked" error

**Solutions:**

1. Check for multiple instances:
   ```bash
   ps aux | grep router
   ```

2. Stop all instances:
   ```bash
   # Systemd
   sudo systemctl stop 9router

   # Docker
   docker-compose down

   # Direct binary
   pkill -f router
   ```

3. Check WAL mode:
   ```bash
   sqlite3 data/9router.db "PRAGMA journal_mode;"
   # Should return "wal"
   ```

4. If WAL mode not enabled, enable it:
   ```bash
   sqlite3 data/9router.db "PRAGMA journal_mode=WAL;"
   ```

### Database corrupted

**Symptoms:** "database disk image is malformed" error

**Solutions:**

1. Backup and recreate:
   ```bash
   cp data/9router.db data/9router.db.broken
   rm data/9router.db
   # Server will recreate on next start
   ```

2. Try to recover:
   ```bash
   sqlite3 data/9router.db ".recover" | sqlite3 data/9router-recovered.db
   ```

3. Check disk space:
   ```bash
   df -h
   ```

### Logs not appearing

**Symptoms:** `GET /api/logs` returns empty or no logs

**Solutions:**

1. Check async writer is running:
   ```bash
   # Check for errors in logs
   sudo journalctl -u 9router -f | grep async
   ```

2. Check database:
   ```bash
   sqlite3 data/9router.db "SELECT COUNT(*) FROM request_logs;"
   ```

3. Verify requests are being logged:
   ```bash
   # Make a test request
   curl -X POST http://localhost:20128/v1/chat/completions \
     -H "Authorization: Bearer <your-api-key>" \
     -H "Content-Type: application/json" \
     -d '{"model":"gpt-4","messages":[{"role":"user","content":"test"}]}'

   # Check logs
   ./router logs --db-path ./data/9router.db --limit 1
   ```

---

## Performance Issues

### High memory usage

**Symptoms:** Server using excessive memory

**Solutions:**

1. Check SQLite cache size:
   ```bash
   sqlite3 data/9router.db "PRAGMA cache_size;"
   ```

2. Monitor with metrics:
   ```bash
   curl http://localhost:20128/metrics
   ```

3. Check for memory leaks:
   ```bash
   # Use pprof if available
   go tool pprof http://localhost:20128/debug/pprof/heap
   ```

4. Reduce log retention:
   ```bash
   # Delete old logs
   sqlite3 data/9router.db "DELETE FROM request_logs WHERE start_time < strftime('%s', 'now', '-30 days');"
   ```

### Slow response times

**Symptoms:** API requests taking too long

**Solutions:**

1. Check provider latency:
   ```bash
   ./router logs --db-path ./data/9router.db --provider openai | grep duration_ms
   ```

2. Check for cooldown:
   ```bash
   ./router logs --db-path ./data/9router.db --status error
   ```

3. Reduce timeout if provider is slow:
   ```yaml
   server:
     request_timeout_seconds: 60  # Reduce if needed
   ```

4. Check network latency:
   ```bash
   ping api.openai.com
   ```

---

## Streaming Issues

### Streaming not working

**Symptoms:** `"stream": true` returns non-streaming response

**Solutions:**

1. Check client handles SSE:
   ```javascript
   // Must handle Server-Sent Events
   const response = await fetch('/v1/chat/completions', {
     method: 'POST',
     headers: { 'Content-Type': 'application/json' },
     body: JSON.stringify({ model: 'gpt-4', messages: [...], stream: true })
   });

   const reader = response.body.getReader();
   while (true) {
     const { done, value } = await reader.read();
     if (done) break;
     console.log(new TextDecoder().decode(value));
   }
   ```

2. Check proxy configuration (if using):
   ```nginx
   # For Nginx, disable buffering for SSE
   location / {
     proxy_buffering off;
     proxy_cache off;
     chunked_transfer_encoding off;
   }
   ```

3. Check logs for streaming errors:
   ```bash
   ./router logs --db-path ./data/9router.db --status error
   ```

### Streaming disconnects

**Symptoms:** Stream stops mid-response

**Solutions:**

1. Check timeout settings:
   ```yaml
   server:
     read_timeout_seconds: 300  # Increase for long streams
     write_timeout_seconds: 300
   ```

2. Check client timeout:
   ```javascript
   // Increase client timeout
   const controller = new AbortController();
   setTimeout(() => controller.abort(), 300000); // 5 minutes
   ```

3. Check network stability:
   ```bash
   # Monitor connection
   ping -c 100 api.openai.com
   ```

---

## Authentication Issues

### API key rejected

**Symptoms:** 401 Unauthorized from router

**Solutions:**

1. Check configured API key:
   ```yaml
   server:
     api_key: ${ROUTER_API_KEY}  # Verify this is set
   ```

2. Verify request format:
   ```bash
   # Must include Bearer token
   curl -H "Authorization: Bearer <your-api-key>" \
     http://localhost:20128/v1/models
   ```

3. Check for typos:
   ```bash
   # Verify no extra spaces
   echo $ROUTER_API_KEY
   ```

### Admin API returns 401

**Symptoms:** Admin endpoints return 401

**Solutions:**

1. Verify using correct API key (router's, not provider's):
   ```bash
   # Router API key (from config)
   curl -H "Authorization: Bearer <router-api-key>" \
     http://localhost:20128/api/providers
   ```

2. Check endpoint is protected:
   ```bash
   # Public endpoints (no auth needed)
   curl http://localhost:20128/healthz
   curl http://localhost:20128/metrics
   ```

---

## Getting Help

If you're still experiencing issues:

1. Check logs:
   ```bash
   ./router logs --db-path ./data/9router.db --follow
   ```

2. Validate configuration:
   ```bash
   ./router validate --config ./config/config.yaml
   ```

3. Check health:
   ```bash
   curl http://localhost:20128/healthz
   curl http://localhost:20128/readyz
   ```

4. Review documentation:
   - [API Reference](api-reference.md)
   - [Deployment Guide](deployment.md)
   - [Provider Guide](provider-guide)

5. Check for known issues:
   ```bash
   git log --oneline --grep="fix" --grep="bug" -i
   ```

6. Create a minimal reproducible case:
   - Simplify config to one provider
   - Test with curl
   - Capture exact error messages
   - Include logs and configuration
