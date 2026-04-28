package executors

import "time"

// RuntimeAccount represents provider credentials used during request execution.
type RuntimeAccount struct {
	ProviderID           string
	ConnectionID         string
	Name                 string
	AuthType             string
	APIKey               string
	AccessToken          string
	RefreshToken         string
	IDToken              string
	Cookie               string
	ExpiresAt            *time.Time
	BaseURL              string
	Headers              map[string]string
	ProviderSpecificData map[string]any
}
