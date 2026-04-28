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

func TestUsageQueries(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()

	now := time.Now().UTC()
	entries := []RequestLog{
		{
			RequestID:        "usage-1",
			Model:            "gpt-4.1",
			Provider:         "openai",
			TargetModel:      "gpt-4.1",
			Status:           "success",
			StartTime:        now,
			EndTime:          now.Add(100 * time.Millisecond),
			Duration:         100 * time.Millisecond,
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
			TotalCost:        0.0015,
			Currency:         "USD",
		},
		{
			RequestID:        "usage-2",
			Model:            "claude-3-5-sonnet",
			Provider:         "anthropic",
			TargetModel:      "claude-3-5-sonnet",
			Status:           "error",
			ErrorMessage:     "rate limited",
			StartTime:        now,
			EndTime:          now.Add(50 * time.Millisecond),
			Duration:         50 * time.Millisecond,
			PromptTokens:     20,
			CompletionTokens: 0,
			TotalTokens:      20,
			TotalCost:        0.0002,
			Currency:         "USD",
		},
	}
	for _, entry := range entries {
		entry := entry
		if err := db.LogRequest(context.Background(), &entry); err != nil {
			t.Fatalf("LogRequest: %v", err)
		}
	}
	if err := db.LogRequestDetails(context.Background(), LogRequestDetailsParams{RequestID: "usage-1", RequestBody: `{"model":"gpt-4.1"}`, ResponseBody: `{"id":"chatcmpl"}`, StatusCode: 200}); err != nil {
		t.Fatalf("LogRequestDetails: %v", err)
	}

	summary, err := db.UsageSummary(context.Background())
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	if summary.RequestsTotal != 2 || summary.RequestsSuccess != 1 || summary.RequestsError != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.TotalTokens != 170 {
		t.Fatalf("total tokens = %d, want 170", summary.TotalTokens)
	}

	providers, err := db.UsageProviders(context.Background())
	if err != nil {
		t.Fatalf("UsageProviders: %v", err)
	}
	if len(providers) != 2 {
		t.Fatalf("providers len = %d, want 2", len(providers))
	}

	history, err := db.UsageHistory(context.Background(), 7)
	if err != nil {
		t.Fatalf("UsageHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2", len(history))
	}

	details, err := db.GetRequestDetails(context.Background(), "usage-1")
	if err != nil {
		t.Fatalf("GetRequestDetails: %v", err)
	}
	if details == nil || details.StatusCode != 200 {
		t.Fatalf("unexpected details: %+v", details)
	}
}

func TestProviderConnectionsCRUD(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()

	created, err := db.CreateProviderConnection(context.Background(), ProviderConnection{
		Provider: "openai",
		Name:     "primary-openai",
		AuthType: "api_key",
		APIKey:   "sk-secret",
		BaseURL:  "https://api.openai.com/v1",
		Enabled:  true,
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateProviderConnection: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated connection id")
	}

	listed, err := db.ListProviderConnections(context.Background(), ProviderConnectionFilter{})
	if err != nil {
		t.Fatalf("ListProviderConnections: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(listed))
	}

	conn, err := db.GetProviderConnection(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetProviderConnection: %v", err)
	}
	if conn.APIKey != "sk-secret" {
		t.Fatalf("expected api key persisted")
	}

	conn.DefaultModel = "gpt-4.1-mini"
	updated, err := db.UpdateProviderConnection(context.Background(), created.ID, conn)
	if err != nil {
		t.Fatalf("UpdateProviderConnection: %v", err)
	}
	if updated.DefaultModel != "gpt-4.1-mini" {
		t.Fatalf("expected updated model, got %s", updated.DefaultModel)
	}

	if err := db.DeleteProviderConnection(context.Background(), created.ID); err != nil {
		t.Fatalf("DeleteProviderConnection: %v", err)
	}
}

func TestProxyPoolsCRUD(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()

	ctx := context.Background()
	created, err := db.SaveProxyPool(ctx, ProxyPool{
		ID:      "pool1",
		Name:    "Pool 1",
		Proxies: []string{"http://127.0.0.1:8080"},
	})
	if err != nil {
		t.Fatalf("SaveProxyPool: %v", err)
	}
	if len(created.Proxies) != 1 {
		t.Fatalf("proxies len = %d, want 1", len(created.Proxies))
	}

	listed, err := db.ListProxyPools(ctx)
	if err != nil {
		t.Fatalf("ListProxyPools: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed len = %d, want 1", len(listed))
	}

	created.Proxies = append(created.Proxies, "http://127.0.0.1:8081")
	updated, err := db.SaveProxyPool(ctx, created)
	if err != nil {
		t.Fatalf("UpdateProxyPool: %v", err)
	}
	if len(updated.Proxies) != 2 {
		t.Fatalf("updated proxies len = %d, want 2", len(updated.Proxies))
	}

	if err := db.DeleteProxyPool(ctx, "pool1"); err != nil {
		t.Fatalf("DeleteProxyPool: %v", err)
	}
}
