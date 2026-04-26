# Deployment Guide

This guide covers deploying 9router-go in production environments.

---

## Prerequisites

- Go 1.24.0 or later
- SQLite (for data persistence)
- Systemd (for Linux service management, optional)
- Docker (optional, for containerized deployment)

---

## Quick Start

### 1. Build

```bash
make build
# or
go build -o bin/router ./cmd/router
```

### 2. Configure

Copy and edit the example configuration:

```bash
cp config/config.example.yaml config/config.yaml
# Edit config/config.yaml with your settings
```

### 3. Run

```bash
./bin/router serve --config ./config/config.yaml
```

The server will start on `http://127.0.0.1:20128` by default.

---

## Configuration

Key configuration options in `config.yaml`:

```yaml
server:
  host: "0.0.0.0"              # Bind address
  port: 20128                  # Port
  api_key: "sk-xxx"           # Required for protected endpoints
  request_timeout_seconds: 120 # Request timeout
  cors:
    allowed_origins: []        # Empty = disabled
    allowed_methods: ["GET", "POST", "OPTIONS"]
    allowed_headers: ["Authorization", "Content-Type"]

storage:
  sqlite_path: ./data/9router.db  # SQLite database path

logging:
  level: info                    # debug, info, warn, error
  json_mode: false               # JSON logging for production
  retention_days: 14            # Log retention (if external rotation used)
```

### Provider Configuration

Add AI providers in `config.yaml`:

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

### Route Configuration

Define model routes:

```yaml
routes:
  gpt-4:
    strategy: tier
    targets:
      - provider: openai
        model: gpt-4
        tier: primary
```

---

## Deployment Methods

### Method 1: Systemd Service (Linux)

#### 1. Create User

```bash
sudo useradd -r -s /bin/false 9router
```

#### 2. Install Binary

```bash
sudo cp bin/router /usr/local/bin/9router
sudo chown 9router:9router /usr/local/bin/9router
sudo chmod +x /usr/local/bin/9router
```

#### 3. Create Directories

```bash
sudo mkdir -p /etc/9router
sudo mkdir -p /var/lib/9router/data
sudo chown -R 9router:9router /etc/9router
sudo chown -R 9router:9router /var/lib/9router
```

#### 4. Copy Configuration

```bash
sudo cp config/config.yaml /etc/9router/config.yaml
sudo chown 9router:9router /etc/9router/config.yaml
sudo chmod 600 /etc/9router/config.yaml
```

#### 5. Create Systemd Unit

Create `/etc/systemd/system/9router.service`:

```ini
[Unit]
Description=9router-go AI Model Router
After=network.target

[Service]
Type=simple
User=9router
Group=9router
ExecStart=/usr/local/bin/9router serve --config /etc/9router/config.yaml
WorkingDirectory=/var/lib/9router
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true

# Security hardening
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/9router/data
CapabilityBoundingSet=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
```

#### 6. Enable and Start

```bash
sudo systemctl daemon-reload
sudo systemctl enable 9router
sudo systemctl start 9router
sudo systemctl status 9router
```

#### 7. View Logs

```bash
sudo journalctl -u 9router -f
```

---

### Method 2: Docker

#### 1. Build Image

```bash
make docker-build
# or
docker build -t 9router-go:latest .
```

#### 2. Run Container

```bash
docker run -d \
  --name 9router \
  -p 20128:20128 \
  -v $(pwd)/config/config.yaml:/app/config/config.yaml:ro \
  -v $(pwd)/data:/app/data \
  9router-go:latest
```

#### 3. Docker Compose

```bash
docker-compose up -d
```

The included `docker-compose.yml` handles:
- Volume mounting for config and data persistence
- Port mapping
- Health checks

---

### Method 3: Direct Binary

#### 1. Build

```bash
make build
```

#### 2. Run

```bash
./bin/router serve --config ./config/config.yaml
```

#### 3. Process Manager (Optional)

Use `supervisord`, `pm2`, or similar for process management.

---

## TLS Termination

9router-go does not handle TLS directly. Use a reverse proxy:

### Nginx Example

```nginx
server {
    listen 443 ssl http2;
    server_name router.example.com;

    ssl_certificate /etc/ssl/certs/router.example.com.crt;
    ssl_certificate_key /etc/ssl/private/router.example.com.key;
    ssl_protocols TLSv1.2 TLSv1.3;

    location / {
        proxy_pass http://127.0.0.1:20128;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # For SSE streaming
        proxy_buffering off;
        proxy_cache off;
        proxy_set_header Connection '';
        proxy_http_version 1.1;
        chunked_transfer_encoding off;
    }
}
```

### Caddy Example

```
router.example.com {
    reverse_proxy 127.0.0.1:20128
}
```

---

## Log Rotation

### Systemd-journald (Default)

Logs are written to journald. Configure retention in `/etc/systemd/journald.conf`:

```
SystemMaxUse=500M
MaxRetentionSec=30day
```

### File Logging with logrotate

If using file logging (not default), create `/etc/logrotate.d/9router`:

```
/var/log/9router/*.log {
    daily
    rotate 14
    compress
    delaycompress
    missingok
    notifempty
    create 0640 9router 9router
    postrotate
        systemctl reload 9router > /dev/null 2>&1 || true
    endscript
}
```

---

## Monitoring

### Health Checks

```bash
# Liveness
curl http://localhost:20128/healthz

# Readiness
curl http://localhost:20128/readyz
```

### Metrics (Prometheus)

```bash
curl http://localhost:20128/metrics
```

### Logs

```bash
# Systemd
sudo journalctl -u 9router -f

# Docker
docker logs -f 9router

# CLI logs command
./router logs --db-path ./data/9router.db --follow
```

---

## Security Best Practices

1. **API Keys**
   - Use environment variables for secrets: `${ENV_VAR}`
   - Never commit API keys to version control
   - Rotate API keys regularly
   - Use separate accounts for different environments

2. **Network**
   - Bind to `127.0.0.1` if only local access needed
   - Use firewall rules to restrict access
   - Enable TLS in production
   - Configure CORS appropriately

3. **File Permissions**
   - Config file: `600` (owner read/write only)
   - Database: `640` (owner read/write, group read)
   - Binary: `755` (owner read/write/execute, others read/execute)

4. **Systemd Hardening**
   - Use the provided systemd unit with security options
   - Run as non-root user
   - Enable `NoNewPrivileges`
   - Use `ProtectSystem` and `ProtectHome`

---

## Troubleshooting

### Server won't start

Check logs:
```bash
sudo journalctl -u 9router -n 50
```

Validate config:
```bash
./router validate --config /etc/9router/config.yaml
```

### Database locked

Ensure only one instance is running:
```bash
sudo systemctl status 9router
```

### Rate limiting errors

Check provider account status:
```bash
curl -H "Authorization: Bearer <your-api-key>" \
  http://localhost:20128/api/providers/openai/accounts/account-1/health
```

### High memory usage

Check SQLite WAL mode is enabled (default). Monitor with:
```bash
sudo journalctl -u 9router -f | grep memory
```

---

## Upgrading

### Binary Upgrade

```bash
# Stop service
sudo systemctl stop 9router

# Backup config and data
sudo cp /etc/9router/config.yaml /etc/9router/config.yaml.bak
sudo cp /var/lib/9router/data/9router.db /var/lib/9router/data/9router.db.bak

# Replace binary
sudo cp bin/router /usr/local/bin/9router

# Start service
sudo systemctl start 9router
```

### Docker Upgrade

```bash
docker-compose pull
docker-compose up -d
```

---

## Performance Tuning

### SQLite Optimization

The default configuration uses WAL mode for better concurrency. Adjust SQLite pragmas if needed:

```yaml
# Not currently exposed in config, can be added if needed
# storage:
#   sqlite_pragmas:
#     - "PRAGMA journal_mode=WAL"
#     - "PRAGMA synchronous=NORMAL"
```

### Connection Pooling

SQLite handles connection pooling internally. For high load, consider:
- Increasing SQLite cache size
- Using a separate database for logs vs metrics

### HTTP Timeouts

Adjust timeouts based on your workload:

```yaml
server:
  request_timeout_seconds: 120
  read_timeout_seconds: 125
  write_timeout_seconds: 125
```

---

## High Availability

### Multiple Instances

Deploy multiple instances behind a load balancer:
- Each instance needs its own SQLite database (not shared)
- Use sticky sessions if using streaming
- Configure health checks on load balancer

### Database Replication

For production, consider:
- SQLite to PostgreSQL migration (not currently supported)
- External log aggregation (ELK, Loki, etc.)

---

## Backup

### Database Backup

```bash
# Backup
sqlite3 /var/lib/9router/data/9router.db ".backup /var/lib/9router/data/9router.db.bak"

# Restore
cp /var/lib/9router/data/9router.db.bak /var/lib/9router/data/9router.db
```

### Automated Backup

Add to cron:

```cron
0 2 * * * sqlite3 /var/lib/9router/data/9router.db ".backup /var/lib/9router/data/9router.db.$(date +\%Y\%m\%d).bak"
```

---

## Support

For issues or questions:
- Check logs: `sudo journalctl -u 9router -f`
- Validate config: `./router validate --config config.yaml`
- Review API reference: `docs/api-reference.md`
