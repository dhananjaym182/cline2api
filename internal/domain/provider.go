package domain

import "time"

// CustomProvider is a user-configured OpenAI-compatible upstream.
type CustomProvider struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	BaseURL    string            `json:"baseURL"`
	APIKey     string            `json:"apiKey"`
	ModelIDs   []string          `json:"modelIds"`
	Headers    map[string]string `json:"headers,omitempty"`
	Enabled    bool              `json:"enabled"`
	Priority   int               `json:"priority"`
	TimeoutSec int               `json:"timeoutSec,omitempty"`
	// Free is a provider-level flag used for statistics and fallback hints.
	// It is not a per-model access entitlement.
	Free bool `json:"free"`
	// ConnectionID binds one logged-in provider connection.
	ConnectionID string `json:"connectionId,omitempty"`
	// ConnectionIDs pools several connections for round-robin and failover.
	ConnectionIDs []string  `json:"connectionIds,omitempty"`
	RpmLimit      int       `json:"rpmLimit,omitempty"`
	DailyLimit    int       `json:"dailyRequestLimit,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}
