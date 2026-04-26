// Package sync provides cloud backup and restore of the 9router SQLite database
// and config file to an S3-compatible object store or a plain HTTPS endpoint.
package sync

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Config holds cloud sync settings.
type Config struct {
	Enabled   bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Provider  string `yaml:"provider,omitempty" json:"provider,omitempty"` // "s3", "gcs", "https"
	Endpoint  string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"` // base URL / bucket
	Bucket    string `yaml:"bucket,omitempty" json:"bucket,omitempty"`
	Prefix    string `yaml:"prefix,omitempty" json:"prefix,omitempty"`
	AccessKey string `yaml:"access_key,omitempty" json:"access_key,omitempty"`
	SecretKey string `yaml:"secret_key,omitempty" json:"secret_key,omitempty"`
	// IntervalMinutes controls how often auto-backup runs (0 = disabled)
	IntervalMinutes int `yaml:"interval_minutes,omitempty" json:"interval_minutes,omitempty"`
}

// Manager handles periodic backup and on-demand restore.
type Manager struct {
	cfg        Config
	logger     zerolog.Logger
	client     *http.Client
	mu         sync.RWMutex
	lastBackup time.Time
	nextBackup time.Time
	lastError  string
}

// Status returns the current sync manager state.
type Status struct {
	Enabled         bool      `json:"enabled"`
	Provider        string    `json:"provider,omitempty"`
	IntervalMinutes int       `json:"interval_minutes,omitempty"`
	LastBackup      time.Time `json:"last_backup,omitempty"`
	NextBackup      time.Time `json:"next_backup,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
}

// GetStatus returns a snapshot of the manager's current state.
func (m *Manager) GetStatus() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Status{
		Enabled:         m.cfg.Enabled,
		Provider:        m.cfg.Provider,
		IntervalMinutes: m.cfg.IntervalMinutes,
		LastBackup:      m.lastBackup,
		NextBackup:      m.nextBackup,
		LastError:       m.lastError,
	}
}

// NewManager creates a new cloud sync manager.
func NewManager(cfg Config, logger zerolog.Logger) *Manager {
	return &Manager{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// Start runs the periodic backup loop until ctx is cancelled.
func (m *Manager) Start(ctx context.Context, dbPath, configPath string) {
	if !m.cfg.Enabled || m.cfg.IntervalMinutes <= 0 {
		return
	}

	ticker := time.NewTicker(time.Duration(m.cfg.IntervalMinutes) * time.Minute)
	defer ticker.Stop()

	m.logger.Info().
		Int("interval_minutes", m.cfg.IntervalMinutes).
		Msg("cloud sync: periodic backup started")

	m.mu.Lock()
	m.nextBackup = time.Now().Add(time.Duration(m.cfg.IntervalMinutes) * time.Minute)
	m.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			if err := m.Backup(ctx, dbPath, configPath); err != nil {
				m.logger.Error().Err(err).Msg("cloud sync: backup failed")
				m.mu.Lock()
				m.lastError = err.Error()
				m.mu.Unlock()
			} else {
				m.mu.Lock()
				m.lastBackup = t
				m.lastError = ""
				m.nextBackup = t.Add(time.Duration(m.cfg.IntervalMinutes) * time.Minute)
				m.mu.Unlock()
			}
		}
	}
}

// Backup uploads the database and config file to the configured remote.
func (m *Manager) Backup(ctx context.Context, dbPath, configPath string) error {
	ts := time.Now().UTC().Format("20060102-150405")

	if err := m.uploadFile(ctx, dbPath, m.objectKey("9router-"+ts+".db")); err != nil {
		return fmt.Errorf("cloud sync: backup db: %w", err)
	}
	m.logger.Info().Str("file", dbPath).Msg("cloud sync: db backed up")

	if configPath != "" {
		if err := m.uploadFile(ctx, configPath, m.objectKey("config-"+ts+".yaml")); err != nil {
			return fmt.Errorf("cloud sync: backup config: %w", err)
		}
		m.logger.Info().Str("file", configPath).Msg("cloud sync: config backed up")
	}

	return nil
}

// Restore downloads the most recent backup from the remote and writes it to dst.
func (m *Manager) Restore(ctx context.Context, remoteKey, dstPath string) error {
	data, err := m.downloadFile(ctx, remoteKey)
	if err != nil {
		return fmt.Errorf("cloud sync: restore %s: %w", remoteKey, err)
	}

	if err := os.WriteFile(dstPath, data, 0644); err != nil {
		return fmt.Errorf("cloud sync: write restored file: %w", err)
	}

	m.logger.Info().Str("key", remoteKey).Str("dst", dstPath).Msg("cloud sync: file restored")
	return nil
}

func (m *Manager) objectKey(name string) string {
	prefix := m.cfg.Prefix
	if prefix != "" && prefix[len(prefix)-1] != '/' {
		prefix += "/"
	}
	return prefix + name
}

// uploadFile reads a local file and PUTs it to the remote endpoint.
// Supports "https" provider using Bearer auth; S3/GCS providers use
// the same HTTP PUT but with access-key/secret as Basic auth.
func (m *Manager) uploadFile(ctx context.Context, localPath, key string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	uploadURL := m.buildURL(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	m.setAuth(req)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(data))

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("PUT %s: %w", uploadURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PUT %s: status %d: %s", uploadURL, resp.StatusCode, string(body))
	}

	return nil
}

func (m *Manager) downloadFile(ctx context.Context, key string) ([]byte, error) {
	downloadURL := m.buildURL(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	m.setAuth(req)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", downloadURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", downloadURL, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (m *Manager) buildURL(key string) string {
	base := m.cfg.Endpoint
	if m.cfg.Bucket != "" {
		base += "/" + m.cfg.Bucket
	}
	return base + "/" + key
}

func (m *Manager) setAuth(req *http.Request) {
	if m.cfg.AccessKey != "" {
		req.SetBasicAuth(m.cfg.AccessKey, m.cfg.SecretKey)
	}
}
