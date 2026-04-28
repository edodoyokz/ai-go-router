package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	conn *sql.DB
}

func NewDB(path string) (*DB, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.init(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("initialize database: %w", err)
	}

	return db, nil
}

func (db *DB) init() error {
	// Enable WAL mode for better concurrency
	if _, err := db.conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("enable WAL mode: %w", err)
	}

	// Run migrations
	if err := db.migrate(); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}

func (db *DB) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS request_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id TEXT NOT NULL,
			model TEXT NOT NULL,
			provider TEXT NOT NULL,
			target_model TEXT NOT NULL,
			status TEXT NOT NULL,
			error_message TEXT,
			start_time INTEGER NOT NULL,
			end_time INTEGER NOT NULL,
			duration_ms INTEGER NOT NULL,
			prompt_tokens INTEGER,
			completion_tokens INTEGER,
			total_tokens INTEGER,
			input_cost REAL,
			output_cost REAL,
			total_cost REAL,
			currency TEXT,
			UNIQUE(request_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_request_id ON request_logs(request_id)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_model ON request_logs(model)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_provider ON request_logs(provider)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_start_time ON request_logs(start_time)`,
		`CREATE TABLE IF NOT EXISTS usage_counters (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			date TEXT NOT NULL,
			request_count INTEGER NOT NULL DEFAULT 0,
			total_prompt_tokens INTEGER NOT NULL DEFAULT 0,
			total_completion_tokens INTEGER NOT NULL DEFAULT 0,
			UNIQUE(provider, model, date)
		)`,
		`CREATE TABLE IF NOT EXISTS quota_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider TEXT NOT NULL,
			account TEXT NOT NULL,
			snapshot_date TEXT NOT NULL,
			prompt_tokens INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			cost_usd REAL NOT NULL DEFAULT 0,
			UNIQUE(provider, account, snapshot_date)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_quota_snapshots_provider ON quota_snapshots(provider)`,
		`CREATE INDEX IF NOT EXISTS idx_quota_snapshots_date ON quota_snapshots(snapshot_date)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_counters_date ON usage_counters(date)`,
		`CREATE TABLE IF NOT EXISTS request_details (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id TEXT NOT NULL,
			request_body TEXT,
			response_body TEXT,
			status_code INTEGER,
			created_at INTEGER NOT NULL,
			FOREIGN KEY(request_id) REFERENCES request_logs(request_id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_request_details_request_id ON request_details(request_id)`,
		`CREATE INDEX IF NOT EXISTS idx_request_details_created_at ON request_details(created_at)`,
		`CREATE TABLE IF NOT EXISTS account_cooldowns (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider TEXT NOT NULL,
			account TEXT NOT NULL,
			rate_limited_until INTEGER NOT NULL,
			backoff_level INTEGER NOT NULL DEFAULT 0,
			UNIQUE(provider, account)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_account_cooldowns_provider_account ON account_cooldowns(provider, account)`,
		`CREATE TABLE IF NOT EXISTS model_locks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider TEXT NOT NULL,
			account TEXT NOT NULL,
			model TEXT NOT NULL,
			locked_until INTEGER NOT NULL,
			UNIQUE(provider, account, model)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_model_locks_provider_account_model ON model_locks(provider, account, model)`,
		`CREATE TABLE IF NOT EXISTS provider_connections (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			auth_type TEXT,
			name TEXT,
			display_name TEXT,
			email TEXT,
			api_key TEXT,
			access_token TEXT,
			refresh_token TEXT,
			id_token TEXT,
			expires_at INTEGER,
			provider_specific_data TEXT,
			priority INTEGER NOT NULL DEFAULT 0,
			global_priority INTEGER NOT NULL DEFAULT 0,
			default_model TEXT,
			is_active INTEGER NOT NULL DEFAULT 1,
			test_status TEXT,
			last_error TEXT,
			last_error_at INTEGER,
			last_tested_at INTEGER,
			error_code TEXT,
			backoff_level INTEGER NOT NULL DEFAULT 0,
			last_used_at INTEGER,
			consecutive_use_count INTEGER NOT NULL DEFAULT 0,
			model_locks TEXT,
			provider_type TEXT,
			format TEXT,
			base_url TEXT,
			headers_json TEXT,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_provider_connections_provider ON provider_connections(provider)`,
		`CREATE INDEX IF NOT EXISTS idx_provider_connections_active ON provider_connections(is_active)`,
		`CREATE TABLE IF NOT EXISTS provider_nodes (
			id TEXT PRIMARY KEY,
			prefix TEXT NOT NULL,
			name TEXT NOT NULL,
			api_type TEXT NOT NULL,
			base_url TEXT NOT NULL,
			headers_json TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS proxy_pools (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			proxies_json TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
	}

	for _, migration := range migrations {
		if _, err := db.conn.Exec(migration); err != nil {
			return fmt.Errorf("execute migration: %w", err)
		}
	}

	if _, err := db.conn.Exec(`ALTER TABLE provider_connections ADD COLUMN last_tested_at INTEGER`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return fmt.Errorf("add last_tested_at column: %w", err)
		}
	}

	extendedCols := []struct {
		col string
		def string
	}{
		{"translated_body", "TEXT"},
		{"upstream_status", "INTEGER"},
		{"upstream_body", "TEXT"},
		{"selected_provider", "TEXT"},
		{"selected_account", "TEXT"},
		{"selected_model", "TEXT"},
		{"fallback_attempts", "INTEGER DEFAULT 0"},
		{"usage_json", "TEXT"},
		{"error_category", "TEXT"},
	}
	for _, c := range extendedCols {
		if _, err := db.conn.Exec(`ALTER TABLE request_details ADD COLUMN ` + c.col + ` ` + c.def); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				return fmt.Errorf("add request_details.%s column: %w", c.col, err)
			}
		}
	}

	return nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) Ping() error {
	return db.conn.Ping()
}

func (db *DB) LogRequest(ctx context.Context, req *RequestLog) error {
	query := `
		INSERT INTO request_logs (
			request_id, model, provider, target_model, status, error_message,
			start_time, end_time, duration_ms, prompt_tokens, completion_tokens, total_tokens,
			input_cost, output_cost, total_cost, currency
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.conn.ExecContext(ctx, query,
		req.RequestID, req.Model, req.Provider, req.TargetModel, req.Status, req.ErrorMessage,
		req.StartTime.Unix(), req.EndTime.Unix(), req.Duration.Milliseconds(),
		req.PromptTokens, req.CompletionTokens, req.TotalTokens,
		req.InputCost, req.OutputCost, req.TotalCost, req.Currency,
	)
	return err
}

func (db *DB) IncrementUsage(ctx context.Context, provider, model string, promptTokens, completionTokens int) error {
	date := time.Now().Format("2006-01-02")
	query := `
		INSERT INTO usage_counters (provider, model, date, request_count, total_prompt_tokens, total_completion_tokens)
		VALUES (?, ?, ?, 1, ?, ?)
		ON CONFLICT(provider, model, date) DO UPDATE SET
			request_count = request_count + 1,
			total_prompt_tokens = total_prompt_tokens + ?,
			total_completion_tokens = total_completion_tokens + ?
	`
	_, err := db.conn.ExecContext(ctx, query, provider, model, date, promptTokens, completionTokens, promptTokens, completionTokens)
	return err
}

func (db *DB) LogRequestDetails(ctx context.Context, p LogRequestDetailsParams) error {
	query := `
		INSERT INTO request_details (
			request_id, request_body, translated_body, response_body, status_code,
			upstream_status, upstream_body, selected_provider, selected_account, selected_model,
			fallback_attempts, usage_json, error_category, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.conn.ExecContext(ctx, query,
		p.RequestID, p.RequestBody, p.TranslatedBody, p.ResponseBody, p.StatusCode,
		p.UpstreamStatus, p.UpstreamBody, p.SelectedProvider, p.SelectedAccount, p.SelectedModel,
		p.FallbackAttempts, p.UsageJSON, p.ErrorCategory, time.Now().Unix(),
	)
	return err
}

// LoadAccountCooldowns loads all account cooldown states from the database
func (db *DB) LoadAccountCooldowns(ctx context.Context) (map[string]DBCooldownState, error) {
	query := `
		SELECT provider, account, rate_limited_until, backoff_level
		FROM account_cooldowns
		WHERE rate_limited_until > ?
	`
	rows, err := db.conn.QueryContext(ctx, query, time.Now().Unix())
	if err != nil {
		return nil, fmt.Errorf("query account cooldowns: %w", err)
	}
	defer rows.Close()

	states := make(map[string]DBCooldownState)
	for rows.Next() {
		var provider, account string
		var rateLimitedUntil, backoffLevel int64
		if err := rows.Scan(&provider, &account, &rateLimitedUntil, &backoffLevel); err != nil {
			return nil, fmt.Errorf("scan account cooldown: %w", err)
		}
		key := provider + "/" + account
		states[key] = DBCooldownState{
			RateLimitedUntil: time.Unix(rateLimitedUntil, 0),
			BackoffLevel:     int(backoffLevel),
			ModelLocks:       make(map[string]time.Time),
		}
	}
	return states, nil
}

// SaveAccountCooldown saves or updates an account cooldown state
func (db *DB) SaveAccountCooldown(ctx context.Context, provider, account string, rateLimitedUntil time.Time, backoffLevel int) error {
	query := `
		INSERT INTO account_cooldowns (provider, account, rate_limited_until, backoff_level)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(provider, account) DO UPDATE SET
			rate_limited_until = excluded.rate_limited_until,
			backoff_level = excluded.backoff_level
	`
	_, err := db.conn.ExecContext(ctx, query, provider, account, rateLimitedUntil.Unix(), backoffLevel)
	return err
}

// ClearAccountCooldown removes an account cooldown state
func (db *DB) ClearAccountCooldown(ctx context.Context, provider, account string) error {
	query := `DELETE FROM account_cooldowns WHERE provider = ? AND account = ?`
	_, err := db.conn.ExecContext(ctx, query, provider, account)
	return err
}

// LoadModelLocks loads all model lock states from the database
func (db *DB) LoadModelLocks(ctx context.Context) (map[string]map[string]time.Time, error) {
	query := `
		SELECT provider, account, model, locked_until
		FROM model_locks
		WHERE locked_until > ?
	`
	rows, err := db.conn.QueryContext(ctx, query, time.Now().Unix())
	if err != nil {
		return nil, fmt.Errorf("query model locks: %w", err)
	}
	defer rows.Close()

	locks := make(map[string]map[string]time.Time)
	for rows.Next() {
		var provider, account, model string
		var lockedUntil int64
		if err := rows.Scan(&provider, &account, &model, &lockedUntil); err != nil {
			return nil, fmt.Errorf("scan model lock: %w", err)
		}
		key := provider + "/" + account
		if locks[key] == nil {
			locks[key] = make(map[string]time.Time)
		}
		locks[key][model] = time.Unix(lockedUntil, 0)
	}
	return locks, nil
}

// SaveModelLock saves or updates a model lock state
func (db *DB) SaveModelLock(ctx context.Context, provider, account, model string, lockedUntil time.Time) error {
	query := `
		INSERT INTO model_locks (provider, account, model, locked_until)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(provider, account, model) DO UPDATE SET
			locked_until = excluded.locked_until
	`
	_, err := db.conn.ExecContext(ctx, query, provider, account, model, lockedUntil.Unix())
	return err
}

// ClearModelLock removes a model lock state
func (db *DB) ClearModelLock(ctx context.Context, provider, account, model string) error {
	query := `DELETE FROM model_locks WHERE provider = ? AND account = ? AND model = ?`
	_, err := db.conn.ExecContext(ctx, query, provider, account, model)
	return err
}

// DBCooldownState represents cooldown state loaded from database
type DBCooldownState struct {
	RateLimitedUntil time.Time
	BackoffLevel     int
	ModelLocks       map[string]time.Time
}

type ProviderConnection struct {
	ID                   string            `json:"id"`
	Provider             string            `json:"provider"`
	AuthType             string            `json:"auth_type,omitempty"`
	Name                 string            `json:"name,omitempty"`
	DisplayName          string            `json:"display_name,omitempty"`
	Email                string            `json:"email,omitempty"`
	APIKey               string            `json:"api_key,omitempty"`
	AccessToken          string            `json:"access_token,omitempty"`
	RefreshToken         string            `json:"refresh_token,omitempty"`
	IDToken              string            `json:"id_token,omitempty"`
	ExpiresAt            *time.Time        `json:"expires_at,omitempty"`
	ProviderSpecificData map[string]any    `json:"provider_specific_data,omitempty"`
	Priority             int               `json:"priority,omitempty"`
	GlobalPriority       int               `json:"global_priority,omitempty"`
	DefaultModel         string            `json:"default_model,omitempty"`
	IsActive             bool              `json:"is_active"`
	TestStatus           string            `json:"test_status,omitempty"`
	LastError            string            `json:"last_error,omitempty"`
	LastErrorAt          *time.Time        `json:"last_error_at,omitempty"`
	LastTestedAt         *time.Time        `json:"last_tested_at,omitempty"`
	ErrorCode            string            `json:"error_code,omitempty"`
	BackoffLevel         int               `json:"backoff_level,omitempty"`
	LastUsedAt           *time.Time        `json:"last_used_at,omitempty"`
	ConsecutiveUseCount  int               `json:"consecutive_use_count,omitempty"`
	ModelLocks           []string          `json:"model_locks,omitempty"`
	ProviderType         string            `json:"provider_type,omitempty"`
	Format               string            `json:"format,omitempty"`
	BaseURL              string            `json:"base_url,omitempty"`
	Headers              map[string]string `json:"headers,omitempty"`
	Enabled              bool              `json:"enabled"`
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
}

type ProviderConnectionFilter struct {
	Provider string
	Active   *bool
}

type ProxyPool struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Proxies   []string  `json:"proxies"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (db *DB) ListProviderConnections(ctx context.Context, filter ProviderConnectionFilter) ([]ProviderConnection, error) {
	query := `SELECT id, provider, auth_type, name, display_name, email, api_key, access_token, refresh_token, id_token,
		expires_at, provider_specific_data, priority, global_priority, default_model, is_active, test_status, last_error,
		last_error_at, last_tested_at, error_code, backoff_level, last_used_at, consecutive_use_count, model_locks, provider_type,
		format, base_url, headers_json, enabled, created_at, updated_at
		FROM provider_connections WHERE 1=1`
	args := []any{}
	if strings.TrimSpace(filter.Provider) != "" {
		query += " AND provider = ?"
		args = append(args, strings.TrimSpace(filter.Provider))
	}
	if filter.Active != nil {
		query += " AND is_active = ?"
		if *filter.Active {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	query += " ORDER BY created_at DESC"

	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list provider connections: %w", err)
	}
	defer rows.Close()

	out := []ProviderConnection{}
	for rows.Next() {
		pc, err := scanProviderConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pc)
	}
	return out, nil
}

func (db *DB) GetProviderConnection(ctx context.Context, id string) (ProviderConnection, error) {
	query := `SELECT id, provider, auth_type, name, display_name, email, api_key, access_token, refresh_token, id_token,
		expires_at, provider_specific_data, priority, global_priority, default_model, is_active, test_status, last_error,
		last_error_at, last_tested_at, error_code, backoff_level, last_used_at, consecutive_use_count, model_locks, provider_type,
		format, base_url, headers_json, enabled, created_at, updated_at
		FROM provider_connections WHERE id = ?`
	row := db.conn.QueryRowContext(ctx, query, id)
	pc, err := scanProviderConnection(row)
	if err != nil {
		return ProviderConnection{}, err
	}
	return pc, nil
}

func (db *DB) CreateProviderConnection(ctx context.Context, c ProviderConnection) (ProviderConnection, error) {
	now := time.Now().UTC()
	if strings.TrimSpace(c.ID) == "" {
		c.ID = fmt.Sprintf("pc_%d", now.UnixNano())
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now

	providerData, _ := json.Marshal(c.ProviderSpecificData)
	modelLocks, _ := json.Marshal(c.ModelLocks)
	headers, _ := json.Marshal(c.Headers)

	var expiresAt, lastErrorAt, lastTestedAt, lastUsedAt any
	if c.ExpiresAt != nil {
		expiresAt = c.ExpiresAt.Unix()
	}
	if c.LastErrorAt != nil {
		lastErrorAt = c.LastErrorAt.Unix()
	}
	if c.LastTestedAt != nil {
		lastTestedAt = c.LastTestedAt.Unix()
	}
	if c.LastUsedAt != nil {
		lastUsedAt = c.LastUsedAt.Unix()
	}

	query := `INSERT INTO provider_connections (
		id, provider, auth_type, name, display_name, email, api_key, access_token, refresh_token, id_token,
		expires_at, provider_specific_data, priority, global_priority, default_model, is_active, test_status, last_error,
		last_error_at, last_tested_at, error_code, backoff_level, last_used_at, consecutive_use_count, model_locks, provider_type,
		format, base_url, headers_json, enabled, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := db.conn.ExecContext(ctx, query,
		c.ID, c.Provider, c.AuthType, c.Name, c.DisplayName, c.Email, c.APIKey, c.AccessToken, c.RefreshToken, c.IDToken,
		expiresAt, string(providerData), c.Priority, c.GlobalPriority, c.DefaultModel, boolToInt(c.IsActive), c.TestStatus, c.LastError,
		lastErrorAt, lastTestedAt, c.ErrorCode, c.BackoffLevel, lastUsedAt, c.ConsecutiveUseCount, string(modelLocks), c.ProviderType,
		c.Format, c.BaseURL, string(headers), boolToInt(c.Enabled), c.CreatedAt.Unix(), c.UpdatedAt.Unix(),
	)
	if err != nil {
		return ProviderConnection{}, fmt.Errorf("create provider connection: %w", err)
	}
	return db.GetProviderConnection(ctx, c.ID)
}

func (db *DB) UpdateProviderConnection(ctx context.Context, id string, c ProviderConnection) (ProviderConnection, error) {
	c.UpdatedAt = time.Now().UTC()
	providerData, _ := json.Marshal(c.ProviderSpecificData)
	modelLocks, _ := json.Marshal(c.ModelLocks)
	headers, _ := json.Marshal(c.Headers)

	var expiresAt, lastErrorAt, lastTestedAt, lastUsedAt any
	if c.ExpiresAt != nil {
		expiresAt = c.ExpiresAt.Unix()
	}
	if c.LastErrorAt != nil {
		lastErrorAt = c.LastErrorAt.Unix()
	}
	if c.LastTestedAt != nil {
		lastTestedAt = c.LastTestedAt.Unix()
	}
	if c.LastUsedAt != nil {
		lastUsedAt = c.LastUsedAt.Unix()
	}

	query := `UPDATE provider_connections SET
		provider=?, auth_type=?, name=?, display_name=?, email=?, api_key=?, access_token=?, refresh_token=?, id_token=?,
		expires_at=?, provider_specific_data=?, priority=?, global_priority=?, default_model=?, is_active=?, test_status=?, last_error=?,
		last_error_at=?, last_tested_at=?, error_code=?, backoff_level=?, last_used_at=?, consecutive_use_count=?, model_locks=?, provider_type=?,
		format=?, base_url=?, headers_json=?, enabled=?, updated_at=?
		WHERE id=?`
	res, err := db.conn.ExecContext(ctx, query,
		c.Provider, c.AuthType, c.Name, c.DisplayName, c.Email, c.APIKey, c.AccessToken, c.RefreshToken, c.IDToken,
		expiresAt, string(providerData), c.Priority, c.GlobalPriority, c.DefaultModel, boolToInt(c.IsActive), c.TestStatus, c.LastError,
		lastErrorAt, lastTestedAt, c.ErrorCode, c.BackoffLevel, lastUsedAt, c.ConsecutiveUseCount, string(modelLocks), c.ProviderType,
		c.Format, c.BaseURL, string(headers), boolToInt(c.Enabled), c.UpdatedAt.Unix(), id,
	)
	if err != nil {
		return ProviderConnection{}, fmt.Errorf("update provider connection: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ProviderConnection{}, sql.ErrNoRows
	}
	return db.GetProviderConnection(ctx, id)
}

func (db *DB) DeleteProviderConnection(ctx context.Context, id string) error {
	res, err := db.conn.ExecContext(ctx, `DELETE FROM provider_connections WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete provider connection: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProviderConnection(row scanner) (ProviderConnection, error) {
	var c ProviderConnection
	var expiresAt, lastErrorAt, lastTestedAt, lastUsedAt sql.NullInt64
	var providerData, modelLocks, headersJSON sql.NullString
	var isActive, enabled int
	var createdAt, updatedAt int64
	if err := row.Scan(
		&c.ID, &c.Provider, &c.AuthType, &c.Name, &c.DisplayName, &c.Email, &c.APIKey, &c.AccessToken, &c.RefreshToken, &c.IDToken,
		&expiresAt, &providerData, &c.Priority, &c.GlobalPriority, &c.DefaultModel, &isActive, &c.TestStatus, &c.LastError,
		&lastErrorAt, &lastTestedAt, &c.ErrorCode, &c.BackoffLevel, &lastUsedAt, &c.ConsecutiveUseCount, &modelLocks, &c.ProviderType,
		&c.Format, &c.BaseURL, &headersJSON, &enabled, &createdAt, &updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return ProviderConnection{}, err
		}
		return ProviderConnection{}, fmt.Errorf("scan provider connection: %w", err)
	}
	c.IsActive = isActive == 1
	c.Enabled = enabled == 1
	c.CreatedAt = time.Unix(createdAt, 0).UTC()
	c.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	if expiresAt.Valid {
		t := time.Unix(expiresAt.Int64, 0).UTC()
		c.ExpiresAt = &t
	}
	if lastErrorAt.Valid {
		t := time.Unix(lastErrorAt.Int64, 0).UTC()
		c.LastErrorAt = &t
	}
	if lastTestedAt.Valid {
		t := time.Unix(lastTestedAt.Int64, 0).UTC()
		c.LastTestedAt = &t
	}
	if lastUsedAt.Valid {
		t := time.Unix(lastUsedAt.Int64, 0).UTC()
		c.LastUsedAt = &t
	}
	if providerData.Valid && strings.TrimSpace(providerData.String) != "" {
		_ = json.Unmarshal([]byte(providerData.String), &c.ProviderSpecificData)
	}
	if modelLocks.Valid && strings.TrimSpace(modelLocks.String) != "" {
		_ = json.Unmarshal([]byte(modelLocks.String), &c.ModelLocks)
	}
	if headersJSON.Valid && strings.TrimSpace(headersJSON.String) != "" {
		_ = json.Unmarshal([]byte(headersJSON.String), &c.Headers)
	}
	return c, nil
}

func (db *DB) ListProxyPools(ctx context.Context) ([]ProxyPool, error) {
	rows, err := db.conn.QueryContext(ctx, `SELECT id, name, proxies_json, created_at, updated_at FROM proxy_pools ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list proxy pools: %w", err)
	}
	defer rows.Close()

	pools := []ProxyPool{}
	for rows.Next() {
		pool, err := scanProxyPool(rows)
		if err != nil {
			return nil, err
		}
		pools = append(pools, pool)
	}
	return pools, rows.Err()
}

func (db *DB) GetProxyPool(ctx context.Context, id string) (ProxyPool, error) {
	row := db.conn.QueryRowContext(ctx, `SELECT id, name, proxies_json, created_at, updated_at FROM proxy_pools WHERE id = ?`, id)
	return scanProxyPool(row)
}

func (db *DB) SaveProxyPool(ctx context.Context, pool ProxyPool) (ProxyPool, error) {
	now := time.Now().UTC()
	if pool.ID == "" {
		pool.ID = strings.ToLower(strings.ReplaceAll(pool.Name, " ", "-"))
	}
	if pool.CreatedAt.IsZero() {
		pool.CreatedAt = now
	}
	pool.UpdatedAt = now
	proxiesJSON, err := json.Marshal(pool.Proxies)
	if err != nil {
		return ProxyPool{}, fmt.Errorf("marshal proxy pool proxies: %w", err)
	}
	_, err = db.conn.ExecContext(ctx, `
		INSERT INTO proxy_pools (id, name, proxies_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			proxies_json = excluded.proxies_json,
			updated_at = excluded.updated_at
	`, pool.ID, pool.Name, string(proxiesJSON), pool.CreatedAt.Unix(), pool.UpdatedAt.Unix())
	if err != nil {
		return ProxyPool{}, fmt.Errorf("save proxy pool: %w", err)
	}
	return db.GetProxyPool(ctx, pool.ID)
}

func (db *DB) DeleteProxyPool(ctx context.Context, id string) error {
	res, err := db.conn.ExecContext(ctx, `DELETE FROM proxy_pools WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete proxy pool: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanProxyPool(row scanner) (ProxyPool, error) {
	var pool ProxyPool
	var proxiesJSON string
	var createdAt, updatedAt int64
	if err := row.Scan(&pool.ID, &pool.Name, &proxiesJSON, &createdAt, &updatedAt); err != nil {
		return ProxyPool{}, err
	}
	if strings.TrimSpace(proxiesJSON) != "" {
		if err := json.Unmarshal([]byte(proxiesJSON), &pool.Proxies); err != nil {
			return ProxyPool{}, fmt.Errorf("unmarshal proxy pool proxies: %w", err)
		}
	}
	pool.CreatedAt = time.Unix(createdAt, 0)
	pool.UpdatedAt = time.Unix(updatedAt, 0)
	return pool, nil
}

type ProviderNode struct {
	ID        string            `json:"id"`
	Prefix    string            `json:"prefix"`
	Name      string            `json:"name"`
	APIType   string            `json:"apiType"`
	BaseURL   string            `json:"baseUrl"`
	Headers   map[string]string `json:"headers,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

func (db *DB) ListProviderNodes(ctx context.Context) ([]ProviderNode, error) {
	rows, err := db.conn.QueryContext(ctx, `SELECT id, prefix, name, api_type, base_url, headers_json, created_at, updated_at FROM provider_nodes ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list provider nodes: %w", err)
	}
	defer rows.Close()
	out := []ProviderNode{}
	for rows.Next() {
		n, err := scanProviderNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func (db *DB) GetProviderNode(ctx context.Context, id string) (ProviderNode, error) {
	row := db.conn.QueryRowContext(ctx, `SELECT id, prefix, name, api_type, base_url, headers_json, created_at, updated_at FROM provider_nodes WHERE id = ?`, id)
	return scanProviderNode(row)
}

func (db *DB) CreateProviderNode(ctx context.Context, n ProviderNode) (ProviderNode, error) {
	now := time.Now().UTC()
	if strings.TrimSpace(n.ID) == "" {
		n.ID = fmt.Sprintf("pn_%d", now.UnixNano())
	}
	n.CreatedAt = now
	n.UpdatedAt = now
	headers, _ := json.Marshal(n.Headers)
	_, err := db.conn.ExecContext(ctx, `INSERT INTO provider_nodes (id, prefix, name, api_type, base_url, headers_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.Prefix, n.Name, n.APIType, n.BaseURL, string(headers), n.CreatedAt.Unix(), n.UpdatedAt.Unix())
	if err != nil {
		return ProviderNode{}, fmt.Errorf("create provider node: %w", err)
	}
	return db.GetProviderNode(ctx, n.ID)
}

func (db *DB) UpdateProviderNode(ctx context.Context, id string, n ProviderNode) (ProviderNode, error) {
	n.UpdatedAt = time.Now().UTC()
	headers, _ := json.Marshal(n.Headers)
	res, err := db.conn.ExecContext(ctx, `UPDATE provider_nodes SET prefix=?, name=?, api_type=?, base_url=?, headers_json=?, updated_at=? WHERE id=?`,
		n.Prefix, n.Name, n.APIType, n.BaseURL, string(headers), n.UpdatedAt.Unix(), id)
	if err != nil {
		return ProviderNode{}, fmt.Errorf("update provider node: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ProviderNode{}, sql.ErrNoRows
	}
	return db.GetProviderNode(ctx, id)
}

func (db *DB) DeleteProviderNode(ctx context.Context, id string) error {
	res, err := db.conn.ExecContext(ctx, `DELETE FROM provider_nodes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete provider node: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanProviderNode(row scanner) (ProviderNode, error) {
	var n ProviderNode
	var headersJSON sql.NullString
	var createdAt, updatedAt int64
	if err := row.Scan(&n.ID, &n.Prefix, &n.Name, &n.APIType, &n.BaseURL, &headersJSON, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return ProviderNode{}, err
		}
		return ProviderNode{}, fmt.Errorf("scan provider node: %w", err)
	}
	n.CreatedAt = time.Unix(createdAt, 0).UTC()
	n.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	if headersJSON.Valid && strings.TrimSpace(headersJSON.String) != "" {
		_ = json.Unmarshal([]byte(headersJSON.String), &n.Headers)
	}
	return n, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

type RequestLog struct {
	RequestID        string
	Model            string
	Provider         string
	TargetModel      string
	Status           string
	ErrorMessage     string
	StartTime        time.Time
	EndTime          time.Time
	Duration         time.Duration
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	InputCost        float64
	OutputCost       float64
	TotalCost        float64
	Currency         string
}

type UsageSummary struct {
	RequestsTotal    int
	RequestsSuccess  int
	RequestsError    int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	TotalCost        float64
}

type UsageProviderSummary struct {
	Provider         string
	RequestCount     int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	TotalCost        float64
	LastRequestAt    time.Time
}

type UsageHistoryRow struct {
	Date             string
	Provider         string
	Model            string
	RequestCount     int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	TotalCost        float64
}

type RequestDetailsRecord struct {
	RequestID        string
	RequestBody      string
	TranslatedBody   string
	ResponseBody     string
	StatusCode       int
	UpstreamStatus   int
	UpstreamBody     string
	SelectedProvider string
	SelectedAccount  string
	SelectedModel    string
	FallbackAttempts int
	UsageJSON        string
	ErrorCategory    string
	CreatedAt        time.Time
}

type LogRequestDetailsParams struct {
	RequestID        string
	RequestBody      string
	TranslatedBody   string
	ResponseBody     string
	StatusCode       int
	UpstreamStatus   int
	UpstreamBody     string
	SelectedProvider string
	SelectedAccount  string
	SelectedModel    string
	FallbackAttempts int
	UsageJSON        string
	ErrorCategory    string
}

// LogQueryParams represents query parameters for logs
type LogQueryParams struct {
	Limit     int
	Offset    int
	Provider  string
	Model     string
	Status    string
	StartTime int64
	EndTime   int64
}

// QueryLogs retrieves logs from the database with optional filters and pagination
func (db *DB) QueryLogs(ctx context.Context, params LogQueryParams) ([]RequestLog, int, error) {
	// Build query with filters
	query := `SELECT id, request_id, model, provider, target_model, status, error_message,
			start_time, end_time, duration_ms, prompt_tokens, completion_tokens, total_tokens,
			input_cost, output_cost, total_cost, currency
			FROM request_logs WHERE 1=1`
	args := []interface{}{}
	argCount := 0

	if params.Provider != "" {
		argCount++
		query += fmt.Sprintf(" AND provider = $%d", argCount)
		args = append(args, params.Provider)
	}
	if params.Model != "" {
		argCount++
		query += fmt.Sprintf(" AND model = $%d", argCount)
		args = append(args, params.Model)
	}
	if params.Status != "" {
		argCount++
		query += fmt.Sprintf(" AND status = $%d", argCount)
		args = append(args, params.Status)
	}
	if params.StartTime > 0 {
		argCount++
		query += fmt.Sprintf(" AND start_time >= $%d", argCount)
		args = append(args, params.StartTime)
	}
	if params.EndTime > 0 {
		argCount++
		query += fmt.Sprintf(" AND start_time <= $%d", argCount)
		args = append(args, params.EndTime)
	}

	// Get total count
	countQuery := "SELECT COUNT(*) FROM (" + query + ") AS count_query"
	var total int
	if err := db.conn.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count logs: %w", err)
	}

	// Add pagination
	query += " ORDER BY start_time DESC"
	if params.Limit > 0 {
		argCount++
		query += fmt.Sprintf(" LIMIT $%d", argCount)
		args = append(args, params.Limit)
	}
	if params.Offset > 0 {
		argCount++
		query += fmt.Sprintf(" OFFSET $%d", argCount)
		args = append(args, params.Offset)
	}

	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query logs: %w", err)
	}
	defer rows.Close()

	var logs []RequestLog
	for rows.Next() {
		var log RequestLog
		var id int
		var startTimeUnix, endTimeUnix int64
		var durationMs int64
		err := rows.Scan(
			&id,
			&log.RequestID,
			&log.Model,
			&log.Provider,
			&log.TargetModel,
			&log.Status,
			&log.ErrorMessage,
			&startTimeUnix,
			&endTimeUnix,
			&durationMs,
			&log.PromptTokens,
			&log.CompletionTokens,
			&log.TotalTokens,
			&log.InputCost,
			&log.OutputCost,
			&log.TotalCost,
			&log.Currency,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan log: %w", err)
		}
		log.StartTime = time.Unix(startTimeUnix, 0)
		log.EndTime = time.Unix(endTimeUnix, 0)
		log.Duration = time.Duration(durationMs) * time.Millisecond
		logs = append(logs, log)
	}

	return logs, total, nil
}

func (db *DB) UsageSummary(ctx context.Context) (UsageSummary, error) {
	query := `SELECT
		COUNT(*),
		COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status != 'success' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(prompt_tokens), 0),
		COALESCE(SUM(completion_tokens), 0),
		COALESCE(SUM(total_tokens), 0),
		COALESCE(SUM(total_cost), 0)
		FROM request_logs`
	var out UsageSummary
	if err := db.conn.QueryRowContext(ctx, query).Scan(
		&out.RequestsTotal,
		&out.RequestsSuccess,
		&out.RequestsError,
		&out.PromptTokens,
		&out.CompletionTokens,
		&out.TotalTokens,
		&out.TotalCost,
	); err != nil {
		return UsageSummary{}, fmt.Errorf("query usage summary: %w", err)
	}
	return out, nil
}

func (db *DB) UsageProviders(ctx context.Context) ([]UsageProviderSummary, error) {
	query := `SELECT provider,
		COUNT(*),
		COALESCE(SUM(prompt_tokens), 0),
		COALESCE(SUM(completion_tokens), 0),
		COALESCE(SUM(total_tokens), 0),
		COALESCE(SUM(total_cost), 0),
		COALESCE(MAX(start_time), 0)
		FROM request_logs
		GROUP BY provider
		ORDER BY COUNT(*) DESC, provider ASC`
	rows, err := db.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query usage providers: %w", err)
	}
	defer rows.Close()

	var out []UsageProviderSummary
	for rows.Next() {
		var row UsageProviderSummary
		var lastRequestAt int64
		if err := rows.Scan(
			&row.Provider,
			&row.RequestCount,
			&row.PromptTokens,
			&row.CompletionTokens,
			&row.TotalTokens,
			&row.TotalCost,
			&lastRequestAt,
		); err != nil {
			return nil, fmt.Errorf("scan usage provider: %w", err)
		}
		if lastRequestAt > 0 {
			row.LastRequestAt = time.Unix(lastRequestAt, 0).UTC()
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate usage providers: %w", err)
	}
	return out, nil
}

func (db *DB) UsageHistory(ctx context.Context, days int) ([]UsageHistoryRow, error) {
	if days <= 0 || days > 365 {
		days = 30
	}
	start := time.Now().UTC().AddDate(0, 0, -days+1).Unix()
	query := `SELECT date(start_time, 'unixepoch') AS day, provider, model,
		COUNT(*),
		COALESCE(SUM(prompt_tokens), 0),
		COALESCE(SUM(completion_tokens), 0),
		COALESCE(SUM(total_tokens), 0),
		COALESCE(SUM(total_cost), 0)
		FROM request_logs
		WHERE start_time >= ?
		GROUP BY day, provider, model
		ORDER BY day ASC, provider ASC, model ASC`
	rows, err := db.conn.QueryContext(ctx, query, start)
	if err != nil {
		return nil, fmt.Errorf("query usage history: %w", err)
	}
	defer rows.Close()

	var out []UsageHistoryRow
	for rows.Next() {
		var row UsageHistoryRow
		if err := rows.Scan(
			&row.Date,
			&row.Provider,
			&row.Model,
			&row.RequestCount,
			&row.PromptTokens,
			&row.CompletionTokens,
			&row.TotalTokens,
			&row.TotalCost,
		); err != nil {
			return nil, fmt.Errorf("scan usage history: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate usage history: %w", err)
	}
	return out, nil
}

func (db *DB) GetRequestDetails(ctx context.Context, requestID string) (*RequestDetailsRecord, error) {
	query := `SELECT
		request_id, request_body,
		COALESCE(translated_body,''), response_body, status_code,
		COALESCE(upstream_status,0), COALESCE(upstream_body,''),
		COALESCE(selected_provider,''), COALESCE(selected_account,''), COALESCE(selected_model,''),
		COALESCE(fallback_attempts,0), COALESCE(usage_json,''), COALESCE(error_category,''),
		created_at
		FROM request_details
		WHERE request_id = ?
		ORDER BY created_at DESC
		LIMIT 1`
	var out RequestDetailsRecord
	var createdAt int64
	if err := db.conn.QueryRowContext(ctx, query, requestID).Scan(
		&out.RequestID,
		&out.RequestBody,
		&out.TranslatedBody,
		&out.ResponseBody,
		&out.StatusCode,
		&out.UpstreamStatus,
		&out.UpstreamBody,
		&out.SelectedProvider,
		&out.SelectedAccount,
		&out.SelectedModel,
		&out.FallbackAttempts,
		&out.UsageJSON,
		&out.ErrorCategory,
		&createdAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get request details: %w", err)
	}
	out.CreatedAt = time.Unix(createdAt, 0).UTC()
	return &out, nil
}

// QuotaSnapshot represents a quota usage snapshot
type QuotaSnapshot struct {
	ID               int
	Provider         string
	Account          string
	SnapshotDate     string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CostUSD          float64
}

// SaveQuotaSnapshot saves a quota snapshot to the database
func (db *DB) SaveQuotaSnapshot(ctx context.Context, snapshot QuotaSnapshot) error {
	query := `INSERT INTO quota_snapshots (provider, account, snapshot_date, prompt_tokens, completion_tokens, total_tokens, cost_usd)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(provider, account, snapshot_date) DO UPDATE SET
			prompt_tokens = excluded.prompt_tokens,
			completion_tokens = excluded.completion_tokens,
			total_tokens = excluded.total_tokens,
			cost_usd = excluded.cost_usd`

	_, err := db.conn.ExecContext(ctx, query,
		snapshot.Provider, snapshot.Account, snapshot.SnapshotDate,
		snapshot.PromptTokens, snapshot.CompletionTokens, snapshot.TotalTokens, snapshot.CostUSD)
	if err != nil {
		return fmt.Errorf("save quota snapshot: %w", err)
	}
	return nil
}

// GetQuotaSnapshots retrieves quota snapshots for a provider
func (db *DB) GetQuotaSnapshots(ctx context.Context, provider string, limit int) ([]QuotaSnapshot, error) {
	query := `SELECT id, provider, account, snapshot_date, prompt_tokens, completion_tokens, total_tokens, cost_usd
			FROM quota_snapshots WHERE provider = ?
			ORDER BY snapshot_date DESC`
	if limit > 0 {
		query += " LIMIT ?"
	}

	rows, err := db.conn.QueryContext(ctx, query, provider, limit)
	if err != nil {
		return nil, fmt.Errorf("get quota snapshots: %w", err)
	}
	defer rows.Close()

	var snapshots []QuotaSnapshot
	for rows.Next() {
		var snap QuotaSnapshot
		err := rows.Scan(&snap.ID, &snap.Provider, &snap.Account, &snap.SnapshotDate,
			&snap.PromptTokens, &snap.CompletionTokens, &snap.TotalTokens, &snap.CostUSD)
		if err != nil {
			return nil, fmt.Errorf("scan quota snapshot: %w", err)
		}
		snapshots = append(snapshots, snap)
	}

	return snapshots, nil
}
