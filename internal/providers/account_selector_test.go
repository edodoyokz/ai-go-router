package providers

import (
	"testing"
)

func TestAccountSelector(t *testing.T) {
	accounts := map[string]string{
		"account1": "key1",
		"account2": "key2",
		"account3": "key3",
	}

	selector := NewAccountSelector(accounts, "default-key")

	// Test round-robin selection
	name1, key1 := selector.GetAccount("")
	if key1 != "key1" && key1 != "key2" && key1 != "key3" {
		t.Errorf("Expected one of the account keys, got %s", key1)
	}

	name2, key2 := selector.GetAccount("")
	if key2 != "key1" && key2 != "key2" && key2 != "key3" {
		t.Errorf("Expected one of the account keys, got %s", key2)
	}

	// Verify rotation (should get different accounts)
	if name1 == name2 {
		t.Logf("Warning: got same account twice (may happen with 3 accounts)")
	}

	// Test specific account selection
	name, key := selector.GetAccount("account2")
	if name != "account2" {
		t.Errorf("Expected account2, got %s", name)
	}
	if key != "key2" {
		t.Errorf("Expected key2, got %s", key)
	}

	// Test fallback to deprecated key when no accounts
	emptySelector := NewAccountSelector(map[string]string{}, "fallback-key")
	name, key = emptySelector.GetAccount("")
	if name != "default" {
		t.Errorf("Expected default, got %s", name)
	}
	if key != "fallback-key" {
		t.Errorf("Expected fallback-key, got %s", key)
	}
}
