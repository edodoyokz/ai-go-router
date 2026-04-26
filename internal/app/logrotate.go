package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

// RotatingFileWriter is an io.Writer that writes to a file and rotates it when
// it exceeds MaxSizeMB. Old rotated files are named <path>.YYYYMMDD-HHMMSS.
// It keeps at most MaxBackups rotated files and deletes those older than MaxAgeDays.
type RotatingFileWriter struct {
	mu         sync.Mutex
	cfg        config.LogRotationConfig
	file       *os.File
	currentSz  int64
	maxBytes   int64
}

// NewRotatingFileWriter opens (or creates) the log file and returns a writer.
func NewRotatingFileWriter(cfg config.LogRotationConfig) (*RotatingFileWriter, error) {
	if cfg.FilePath == "" {
		return nil, fmt.Errorf("log rotation: file_path must be set")
	}

	if err := os.MkdirAll(filepath.Dir(cfg.FilePath), 0755); err != nil {
		return nil, fmt.Errorf("log rotation: create log dir: %w", err)
	}

	f, err := os.OpenFile(cfg.FilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("log rotation: open log file: %w", err)
	}

	fi, _ := f.Stat()
	sz := int64(0)
	if fi != nil {
		sz = fi.Size()
	}

	maxBytes := int64(100) * 1024 * 1024 // default 100 MB
	if cfg.MaxSizeMB > 0 {
		maxBytes = int64(cfg.MaxSizeMB) * 1024 * 1024
	}

	return &RotatingFileWriter{
		cfg:       cfg,
		file:      f,
		currentSz: sz,
		maxBytes:  maxBytes,
	}, nil
}

func (w *RotatingFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.currentSz+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			// Best-effort: log to stderr and continue writing to current file
			fmt.Fprintf(os.Stderr, "log rotation failed: %v\n", err)
		}
	}

	n, err := w.file.Write(p)
	w.currentSz += int64(n)
	return n, err
}

func (w *RotatingFileWriter) rotate() error {
	// Close current file
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("close current log: %w", err)
	}

	// Rename to timestamped backup
	ts := time.Now().Format("20060102-150405")
	backupPath := w.cfg.FilePath + "." + ts
	if err := os.Rename(w.cfg.FilePath, backupPath); err != nil {
		return fmt.Errorf("rename log file: %w", err)
	}

	// Open new file
	f, err := os.OpenFile(w.cfg.FilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open new log file: %w", err)
	}
	w.file = f
	w.currentSz = 0

	// Clean up old backups
	w.pruneBackups()
	return nil
}

func (w *RotatingFileWriter) pruneBackups() {
	dir := filepath.Dir(w.cfg.FilePath)
	base := filepath.Base(w.cfg.FilePath)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	var backups []os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Match files like "<base>.YYYYMMDD-HHMMSS"
		if len(name) > len(base)+1 && name[:len(base)+1] == base+"." {
			backups = append(backups, e)
		}
	}

	// Delete by age first
	if w.cfg.MaxAgeDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -w.cfg.MaxAgeDays)
		for _, e := range backups {
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
		// Refresh list
		backups = backups[:0]
		if entries2, err := os.ReadDir(dir); err == nil {
			for _, e := range entries2 {
				if !e.IsDir() && len(e.Name()) > len(base)+1 && e.Name()[:len(base)+1] == base+"." {
					backups = append(backups, e)
				}
			}
		}
	}

	// Delete oldest if over MaxBackups
	if w.cfg.MaxBackups > 0 && len(backups) > w.cfg.MaxBackups {
		// entries are sorted alphabetically; oldest timestamps sort first
		for i := 0; i < len(backups)-w.cfg.MaxBackups; i++ {
			_ = os.Remove(filepath.Join(dir, backups[i].Name()))
		}
	}
}

// Close flushes and closes the underlying file.
func (w *RotatingFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}
