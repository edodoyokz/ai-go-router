package providers

import (
	"sort"
	"sync"
)

// AccountSelector handles round-robin account selection
type AccountSelector struct {
	accounts      map[string]string // account name -> API key
	deprecatedKey string            // fallback API key for backward compatibility
	accountIdx    int
	accountMu     sync.Mutex
}

// NewAccountSelector creates a new account selector
func NewAccountSelector(accounts map[string]string, deprecatedKey string) *AccountSelector {
	return &AccountSelector{
		accounts:      accounts,
		deprecatedKey: deprecatedKey,
		accountIdx:    0,
	}
}

// GetAccount returns the next account name and API key using round-robin
// If context contains a specific account, it returns that account's key
// Otherwise, it uses round-robin selection
func (s *AccountSelector) GetAccount(accountName string) (string, string) {
	// If specific account requested, look it up
	if accountName != "" {
		if key, exists := s.accounts[accountName]; exists {
			return accountName, key
		}
		// Account not found, fall back to deprecated key
		return "default", s.deprecatedKey
	}

	// Use round-robin selection
	s.accountMu.Lock()
	defer s.accountMu.Unlock()

	// If no accounts configured, use deprecated key
	if len(s.accounts) == 0 {
		return "default", s.deprecatedKey
	}

	// Get sorted account names for deterministic order
	accountNames := make([]string, 0, len(s.accounts))
	for name := range s.accounts {
		accountNames = append(accountNames, name)
	}
	sort.Strings(accountNames)

	// Round-robin selection
	name := accountNames[s.accountIdx%len(accountNames)]
	s.accountIdx++
	apiKey := s.accounts[name]

	return name, apiKey
}
