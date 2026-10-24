package domain

import "time"

// Account is a Cline account in the local pool.
type Account struct {
	AccountID        string    `json:"accountId"`
	Email            string    `json:"email"`
	RefreshToken     string    `json:"refreshToken"`
	AccessToken      string    `json:"-"`
	ExpiresAt        int64     `json:"-"`
	Status           string    `json:"status"`
	CooldownUntil    time.Time `json:"cooldownUntil,omitempty"`
	LastUsed         time.Time `json:"lastUsed"`
	UsageCount       int64     `json:"usageCount"`
	PromptTokens     int64     `json:"promptTokens"`
	CompletionTokens int64     `json:"completionTokens"`
	TotalTokens      int64     `json:"totalTokens"`
	CachedTokens     int64     `json:"cachedTokens"`
	CreatedAt        time.Time `json:"createdAt"`
	// ModelStats holds per-model usage for free models.
	ModelStats map[string]*ModelStat `json:"modelStats,omitempty"`
	// ModelCooldowns stores model-level cooldown deadlines (429 pauses one model).
	ModelCooldowns map[string]time.Time `json:"modelCooldowns,omitempty"`
}

// Model is a model exposed by the proxy.
type Model struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Cost     string `json:"cost"`
	Status   string `json:"status"`
	Custom   bool   `json:"custom"`
	// Source records the origin: "remote", "zen", or empty for built-in/custom.
	Source string `json:"source,omitempty"`
	// Context and Output are upstream token limits (0 means unknown).
	Context int `json:"context,omitempty"`
	Output  int `json:"output,omitempty"`
	// MetaLocked preserves a user-set Context/Output across Zen syncs.
	MetaLocked bool `json:"metaLocked,omitempty"`
	// Delisted marks a model removed upstream but retained locally.
	Delisted bool `json:"delisted,omitempty"`
	// Disabled hides a model from clients and automatic fallback.
	Disabled bool `json:"disabled,omitempty"`
	// TierSignal records the provenance of a free/paid classification.
	TierSignal string `json:"tierSignal,omitempty"`
	// TierDecidedAt is the classification time in Unix seconds.
	TierDecidedAt int64 `json:"tierDecidedAt,omitempty"`
}

// ModelStat is per-model usage for one account (free models only).
type ModelStat struct {
	ModelID          string `json:"modelId"`
	Cost             string `json:"cost"`
	UsageCount       int64  `json:"usageCount"`
	PromptTokens     int64  `json:"promptTokens"`
	CompletionTokens int64  `json:"completionTokens"`
	TotalTokens      int64  `json:"totalTokens"`
	CachedTokens     int64  `json:"cachedTokens"`
}

// AccountPool is the persisted pool and proxy settings.
type AccountPool struct {
	Accounts     []*Account `json:"accounts"`
	CurrentIdx   int        `json:"currentIdx"`
	Keys         []string   `json:"keys,omitempty"`
	Models       []Model    `json:"models,omitempty"`
	DefaultModel string     `json:"defaultModel,omitempty"`
	ListenHost   string     `json:"listenHost,omitempty"`
	// AdminPasswordHash and AdminPasswordSalt are set by the admin UI.
	AdminPasswordHash string `json:"adminPasswordHash,omitempty"`
	AdminPasswordSalt string `json:"adminPasswordSalt,omitempty"`
}

// LoginMethod identifies how an account was added.
type LoginMethod int

const (
	MethodDeviceOAuth LoginMethod = iota
	MethodRefreshToken
	MethodSSOCookie
)
