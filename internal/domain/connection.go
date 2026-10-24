package domain

import "time"

// Connection is a provider login connection (OAuth device code or imported
// token) persisted separately from the Cline account pool.
type Connection struct {
	ID          string `json:"id"`
	Provider    string `json:"provider"`
	AuthType    string `json:"authType"`
	Email       string `json:"email,omitempty"`
	AccessToken string `json:"accessToken"`
	// RefreshToken is empty when the upstream does not issue one.
	RefreshToken         string                `json:"refreshToken,omitempty"`
	ExpiresAt            int64                 `json:"expiresAt,omitempty"`
	ProviderSpecificData map[string]string     `json:"providerSpecificData,omitempty"`
	Status               string                `json:"status"`
	CreatedAt            time.Time             `json:"createdAt"`
	UsageCount           int64                 `json:"usageCount,omitempty"`
	PromptTokens         int64                 `json:"promptTokens,omitempty"`
	CompletionTokens     int64                 `json:"completionTokens,omitempty"`
	TotalTokens          int64                 `json:"totalTokens,omitempty"`
	CachedTokens         int64                 `json:"cachedTokens,omitempty"`
	ModelStats           map[string]*ModelStat `json:"modelStats,omitempty"`
}
