package main

import "time"

type Account struct {
	AccountID        string    `json:"accountId"`
	Email            string    `json:"email"`
	RefreshToken     string    `json:"refreshToken"`
	AccessToken      string    `json:"-"`
	ExpiresAt        int64     `json:"-"`
	Status           string    `json:"status"` // active, cooldown, expired
	CooldownUntil    time.Time `json:"cooldownUntil,omitempty"`
	LastUsed         time.Time `json:"lastUsed"`
	UsageCount       int64     `json:"usageCount"`
	PromptTokens     int64     `json:"promptTokens"`
	CompletionTokens int64     `json:"completionTokens"`
	TotalTokens      int64     `json:"totalTokens"`
	CachedTokens     int64     `json:"cachedTokens"`
	CreatedAt        time.Time `json:"createdAt"`
	// ModelStats 按模型细分的用量统计（仅记录 free 模型）
	ModelStats map[string]*ModelStat `json:"modelStats,omitempty"`
	// ModelCooldowns 模型级冷却：modelID → 恢复时间（429 时记录，只暂停该模型）
	ModelCooldowns map[string]time.Time `json:"modelCooldowns,omitempty"`
}

type Model struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Cost     string `json:"cost"`   // "free" | "pass"
	Status   string `json:"status"` // "active"
	Custom   bool   `json:"custom"` // true=用户手动添加，可删除
	// Source 标记模型来源："remote"=从 Cline 官方接口同步，"zen"=从 opencode 官方接口同步，空=内置/用户自定义
	Source string `json:"source,omitempty"`
	// Context / Output 上下文与最大输出 token（opencode 模型记录；0=未知）
	Context int `json:"context,omitempty"`
	Output  int `json:"output,omitempty"`
	// MetaLocked 用户在管理页手动设置过 Context/Output：zen 同步保留该值不再覆盖
	MetaLocked bool `json:"metaLocked,omitempty"`
	// Delisted 模型已从上游官方列表消失，但被保留不删 —— 实测官方列表移除后
	// 模型往往仍可继续用（如 z-ai/glm-5.3-flash）。同步只做标记，管理页显示
	// 「已下架」徽标并支持手动移除；上游明确报模型不存在时自动清理。
	Delisted bool `json:"delisted,omitempty"`
	// Disabled 用户在管理页手动停用：不进客户端模型列表（/v1/models）、
	// 直接请求返回 400、默认模型与回退链自动跳过；同步保留该标记。
	Disabled bool `json:"disabled,omitempty"`
}

// ModelStat 是单个模型在某账号下的用量统计（仅统计 free 模型）。
type ModelStat struct {
	ModelID          string `json:"modelId"`
	Cost             string `json:"cost"`
	UsageCount       int64  `json:"usageCount"`
	PromptTokens     int64  `json:"promptTokens"`
	CompletionTokens int64  `json:"completionTokens"`
	TotalTokens      int64  `json:"totalTokens"`
	CachedTokens     int64  `json:"cachedTokens"`
}

type AccountPool struct {
	Accounts     []*Account `json:"accounts"`
	CurrentIdx   int        `json:"currentIdx"`
	Keys         []string   `json:"keys,omitempty"`
	Models       []Model    `json:"models,omitempty"`
	DefaultModel string     `json:"defaultModel,omitempty"`
	// 访问设置：监听地址与管理后台密码（后台 UI 保存）
	ListenHost        string `json:"listenHost,omitempty"`
	AdminPasswordHash string `json:"adminPasswordHash,omitempty"`
	AdminPasswordSalt string `json:"adminPasswordSalt,omitempty"`
}

type LoginMethod int

const (
	MethodDeviceOAuth LoginMethod = iota
	MethodRefreshToken
	MethodSSOCookie
)
