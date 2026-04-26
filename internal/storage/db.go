package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
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
	}

	for _, migration := range migrations {
		if _, err := db.conn.Exec(migration); err != nil {
			return fmt.Errorf("execute migration: %w", err)
		}
	}

	return nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) LogRequest(ctx context.Context, req *RequestLog) error {
	query := `
		INSERT INTO request_logs (
			request_id, model, provider, target_model, status, error_message,
			start_time, end_time, duration_ms, prompt_tokens, completion_tokens, total_tokens
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.conn.ExecContext(ctx, query,
		req.RequestID, req.Model, req.Provider, req.TargetModel, req.Status, req.ErrorMessage,
		req.StartTime.Unix(), req.EndTime.Unix(), req.Duration.Milliseconds(),
		req.PromptTokens, req.CompletionTokens, req.TotalTokens,
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

func (db *DB) LogRequestDetails(ctx context.Context, requestID string, requestBody, responseBody string, statusCode int) error {
	query := `
		INSERT INTO request_details (request_id, request_body, response_body, status_code, created_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := db.conn.ExecContext(ctx, query, requestID, requestBody, responseBody, statusCode, time.Now().Unix())
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
}
