package storage

import (
	"context"
	"os"
	"testing"
	"time"
)

func newTestDB(t *testing.T) (*DB, func()) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	db, err := NewDB(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	return db, func() { db.Close() }
}

func TestLogRequest(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()

	req := &RequestLog{
		RequestID:        "req-1",
		Model:            "gpt-4",
		Provider:         "openai",
		TargetModel:      "gpt-4",
		Status:           "success",
		StartTime:        time.Now().Add(-100 * time.Millisecond),
		EndTime:          time.Now(),
		Duration:         100 * time.Millisecond,
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
	}

	if err := db.LogRequest(context.Background(), req); err != nil {
		t.Fatalf("LogRequest: %v", err)
	}
}

func TestQueryLogs_Empty(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()

	logs, total, err := db.QueryLogs(context.Background(), LogQueryParams{Limit: 10})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if total != 0 {
		t.Errorf("expected total 0, got %d", total)
	}
	if len(logs) != 0 {
		t.Errorf("expected 0 logs, got %d", len(logs))
	}
}

func TestQueryLogs_WithData(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()

	now := time.Now()
	entries := []RequestLog{
		{RequestID: "req-1", Model: "gpt-4", Provider: "openai", TargetModel: "gpt-4", Status: "success", StartTime: now, EndTime: now, Duration: 50 * time.Millisecond},
		{RequestID: "req-2", Model: "claude-3", Provider: "anthropic", TargetModel: "claude-3-sonnet", Status: "success", StartTime: now, EndTime: now, Duration: 80 * time.Millisecond},
		{RequestID: "req-3", Model: "gpt-4", Provider: "openai", TargetModel: "gpt-4", Status: "error", ErrorMessage: "rate limited", StartTime: now, EndTime: now, Duration: 10 * time.Millisecond},
	}

	for _, e := range entries {
		e := e
		if err := db.LogRequest(context.Background(), &e); err != nil {
			t.Fatalf("LogRequest: %v", err)
		}
	}

	t.Run("all logs", func(t *testing.T) {
		logs, total, err := db.QueryLogs(context.Background(), LogQueryParams{Limit: 10})
		if err != nil {
			t.Fatalf("QueryLogs: %v", err)
		}
		if total != 3 {
			t.Errorf("expected total 3, got %d", total)
		}
		if len(logs) != 3 {
			t.Errorf("expected 3 logs, got %d", len(logs))
		}
	})

	t.Run("filter by provider", func(t *testing.T) {
		logs, total, err := db.QueryLogs(context.Background(), LogQueryParams{Provider: "openai", Limit: 10})
		if err != nil {
			t.Fatalf("QueryLogs: %v", err)
		}
		if total != 2 {
			t.Errorf("expected total 2 for openai, got %d", total)
		}
		if len(logs) != 2 {
			t.Errorf("expected 2 logs, got %d", len(logs))
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		logs, total, err := db.QueryLogs(context.Background(), LogQueryParams{Status: "error", Limit: 10})
		if err != nil {
			t.Fatalf("QueryLogs: %v", err)
		}
		if total != 1 {
			t.Errorf("expected total 1 for error status, got %d", total)
		}
		if len(logs) != 1 {
			t.Errorf("expected 1 log, got %d", len(logs))
		}
	})

	t.Run("filter by model", func(t *testing.T) {
		logs, total, err := db.QueryLogs(context.Background(), LogQueryParams{Model: "gpt-4", Limit: 10})
		if err != nil {
			t.Fatalf("QueryLogs: %v", err)
		}
		if total != 2 {
			t.Errorf("expected total 2 for gpt-4, got %d", total)
		}
		if len(logs) != 2 {
			t.Errorf("expected 2 logs, got %d", len(logs))
		}
	})

	t.Run("pagination limit", func(t *testing.T) {
		logs, total, err := db.QueryLogs(context.Background(), LogQueryParams{Limit: 2})
		if err != nil {
			t.Fatalf("QueryLogs: %v", err)
		}
		if total != 3 {
			t.Errorf("expected total 3, got %d", total)
		}
		if len(logs) != 2 {
			t.Errorf("expected 2 logs (limit), got %d", len(logs))
		}
	})

	t.Run("pagination offset", func(t *testing.T) {
		logs, total, err := db.QueryLogs(context.Background(), LogQueryParams{Limit: 10, Offset: 2})
		if err != nil {
			t.Fatalf("QueryLogs: %v", err)
		}
		if total != 3 {
			t.Errorf("expected total 3, got %d", total)
		}
		if len(logs) != 1 {
			t.Errorf("expected 1 log (offset), got %d", len(logs))
		}
	})
}

func TestIncrementUsage(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()

	ctx := context.Background()

	if err := db.IncrementUsage(ctx, "openai", "gpt-4", 100, 200); err != nil {
		t.Fatalf("IncrementUsage: %v", err)
	}

	// Second call should upsert
	if err := db.IncrementUsage(ctx, "openai", "gpt-4", 50, 100); err != nil {
		t.Fatalf("IncrementUsage (second): %v", err)
	}
}
