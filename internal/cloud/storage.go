package cloud

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

type Storage interface {
	Load(ctx context.Context) (map[string]map[string]any, error)
	Save(ctx context.Context, machineID string, data map[string]any) error
	Delete(ctx context.Context, machineID string) error
}

type MemoryStorage struct {
	mu    sync.RWMutex
	syncs map[string]map[string]any
}

func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{syncs: map[string]map[string]any{}}
}

func (m *MemoryStorage) Load(context.Context) (map[string]map[string]any, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]map[string]any, len(m.syncs))
	for machineID, data := range m.syncs {
		out[machineID] = copyMap(data)
	}
	return out, nil
}

func (m *MemoryStorage) Save(_ context.Context, machineID string, data map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncs[machineID] = copyMap(data)
	return nil
}

func (m *MemoryStorage) Delete(_ context.Context, machineID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.syncs, machineID)
	return nil
}

type SQLiteStorage struct {
	db *sql.DB
}

func NewSQLiteStorage(path string) (*SQLiteStorage, error) {
	if path == "" {
		path = defaultSQLitePath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("cloud storage mkdir: %w", err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("cloud storage open: %w", err)
	}
	storage := &SQLiteStorage{db: db}
	if err := storage.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return storage, nil
}

func defaultSQLitePath() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "9router", "cloud.db")
	}
	return filepath.Join(os.TempDir(), "9router-cloud.db")
}

func (s *SQLiteStorage) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS cloud_syncs (
		machine_id TEXT PRIMARY KEY,
		data TEXT NOT NULL,
		updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("cloud storage migrate: %w", err)
	}
	return nil
}

func (s *SQLiteStorage) Load(ctx context.Context) (map[string]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT machine_id, data FROM cloud_syncs`)
	if err != nil {
		return nil, fmt.Errorf("cloud storage load: %w", err)
	}
	defer rows.Close()

	out := map[string]map[string]any{}
	for rows.Next() {
		var machineID, raw string
		if err := rows.Scan(&machineID, &raw); err != nil {
			return nil, fmt.Errorf("cloud storage scan: %w", err)
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			return nil, fmt.Errorf("cloud storage decode %s: %w", machineID, err)
		}
		out[machineID] = data
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cloud storage rows: %w", err)
	}
	return out, nil
}

func (s *SQLiteStorage) Save(ctx context.Context, machineID string, data map[string]any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("cloud storage encode: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO cloud_syncs(machine_id, data, updated_at)
		VALUES(?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(machine_id) DO UPDATE SET data=excluded.data, updated_at=CURRENT_TIMESTAMP`, machineID, string(raw))
	if err != nil {
		return fmt.Errorf("cloud storage save: %w", err)
	}
	return nil
}

func (s *SQLiteStorage) Delete(ctx context.Context, machineID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM cloud_syncs WHERE machine_id = ?`, machineID)
	if err != nil {
		return fmt.Errorf("cloud storage delete: %w", err)
	}
	return nil
}
