package config

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Error-rule actions. A rule maps an upstream failure signature to routing
// behavior; an empty rule set keeps the built-in classifiers in charge.
const (
	ErrorActionStop                = "stop"
	ErrorActionStopAndCooldown     = "stop_and_cooldown"
	ErrorActionContinue            = "continue"
	ErrorActionContinueAndCooldown = "continue_and_cooldown"
)

// RetryPolicy controls fallback breadth and transient cooldown behavior.
type RetryPolicy struct {
	// MaxRounds is the number of passes over the fallback chain. 0 or 1 keeps
	// the historic single pass.
	MaxRounds int `json:"maxRounds,omitempty"`
	// MaxCredentialsPerRound caps accounts tried per model in one round.
	// 0 means unlimited.
	MaxCredentialsPerRound int `json:"maxCredentialsPerRound,omitempty"`
	// MaxIntervalSeconds bounds the wait between rounds. 0 means no wait.
	MaxIntervalSeconds int `json:"maxIntervalSeconds,omitempty"`
	// TransientCooldownSeconds overrides the cooldown for transient upstream
	// errors (408/5xx). 0 keeps the provider default; -1 disables it.
	TransientCooldownSeconds int `json:"transientCooldownSeconds,omitempty"`
	// MaxFailoverModels caps model attempts in the cross-provider failover
	// loop. 0 keeps the built-in cap of 3.
	MaxFailoverModels int `json:"maxFailoverModels,omitempty"`
}

func (p *RetryPolicy) Normalize() {
	p.MaxRounds = clampInt(p.MaxRounds, 0, 100)
	p.MaxCredentialsPerRound = clampInt(p.MaxCredentialsPerRound, 0, 10000)
	p.MaxIntervalSeconds = clampInt(p.MaxIntervalSeconds, 0, 600)
	p.MaxFailoverModels = clampInt(p.MaxFailoverModels, 0, 100)
	if p.TransientCooldownSeconds < -1 {
		p.TransientCooldownSeconds = -1
	}
	if p.TransientCooldownSeconds > 3600 {
		p.TransientCooldownSeconds = 3600
	}
}

// StreamPolicy controls SSE heartbeats and the pre-content bootstrap window.
type StreamPolicy struct {
	// KeepaliveSeconds is the heartbeat interval. 0 uses the built-in 15s
	// default; -1 disables heartbeats.
	KeepaliveSeconds int `json:"keepaliveSeconds,omitempty"`
	// BootstrapRetries is the number of pre-content retries allowed before the
	// first downstream content frame. 0 keeps the historic single retry;
	// negative values are treated as 0.
	BootstrapRetries int `json:"bootstrapRetries,omitempty"`
	// SuppressKeepaliveBeforeFirstToken holds heartbeat comments back until the
	// first semantic content frame (upstream-style strict bootstrap). Default
	// false keeps the historic behavior: heartbeats flow while the upstream is
	// silent so intermediaries do not idle out the connection.
	SuppressKeepaliveBeforeFirstToken bool `json:"suppressKeepaliveBeforeFirstToken,omitempty"`
	// FirstContentTimeoutSeconds aborts an attempt that has not produced
	// output within the window. 0 disables the timeout.
	FirstContentTimeoutSeconds int `json:"firstContentTimeoutSeconds,omitempty"`
	// BootstrapBufferBytes bounds buffered pre-content frames. 0 uses 1 MiB.
	BootstrapBufferBytes int `json:"bootstrapBufferBytes,omitempty"`
	// BootstrapBufferFrames bounds buffered pre-content writes. 0 uses 64.
	BootstrapBufferFrames int `json:"bootstrapBufferFrames,omitempty"`
}

func (p *StreamPolicy) Normalize() {
	if p.KeepaliveSeconds < -1 {
		p.KeepaliveSeconds = -1
	}
	if p.KeepaliveSeconds > 3600 {
		p.KeepaliveSeconds = 3600
	}
	p.BootstrapRetries = clampInt(p.BootstrapRetries, 0, 5)
	p.FirstContentTimeoutSeconds = clampInt(p.FirstContentTimeoutSeconds, 0, 600)
	if p.BootstrapBufferBytes <= 0 {
		p.BootstrapBufferBytes = 1 << 20
	}
	if p.BootstrapBufferBytes > 64<<20 {
		p.BootstrapBufferBytes = 64 << 20
	}
	if p.BootstrapBufferFrames <= 0 {
		p.BootstrapBufferFrames = 64
	}
	if p.BootstrapBufferFrames > 10000 {
		p.BootstrapBufferFrames = 10000
	}
}

// SessionAffinityPolicy binds a conversation to one upstream Cline account.
type SessionAffinityPolicy struct {
	Enabled bool `json:"enabled,omitempty"`
	// TTLSeconds bounds the binding lifetime. 0 uses one hour.
	TTLSeconds int `json:"ttlSeconds,omitempty"`
}

func (p *SessionAffinityPolicy) Normalize() {
	if p.TTLSeconds == 0 {
		p.TTLSeconds = 3600
	}
	p.TTLSeconds = clampInt(p.TTLSeconds, 60, 7*24*3600)
}

// TTL returns the configured binding lifetime.
func (p SessionAffinityPolicy) TTL() time.Duration {
	if p.TTLSeconds <= 0 {
		return time.Hour
	}
	return time.Duration(p.TTLSeconds) * time.Second
}

// ModelAlias exposes one client-visible model id backed by another upstream id.
type ModelAlias struct {
	Alias    string `json:"alias"`
	Target   string `json:"target"`
	Provider string `json:"provider,omitempty"`
	// Force rewrites even when the alias collides with a real catalog model.
	Force bool `json:"force,omitempty"`
}

// RequestErrorRule maps an upstream error signature to routing behavior.
type RequestErrorRule struct {
	// StatusCodes limits the rule to these HTTP statuses. Empty matches any.
	StatusCodes []int `json:"statusCodes,omitempty"`
	// Match is a case-insensitive substring, or a regex when Regex is set.
	Match string `json:"match,omitempty"`
	Regex bool   `json:"regex,omitempty"`
	// Action is one of stop, stop_and_cooldown, continue,
	// continue_and_cooldown. Empty means continue.
	Action string `json:"action,omitempty"`
}

// NormalizeRequestErrorRule canonicalizes an action and returns false when the
// rule is unusable (unknown action, invalid status, bad regex).
func (r *RequestErrorRule) NormalizeRequestErrorRule() bool {
	r.Action = strings.ToLower(strings.TrimSpace(r.Action))
	if r.Action == "" {
		r.Action = ErrorActionContinue
	}
	switch r.Action {
	case ErrorActionStop, ErrorActionStopAndCooldown, ErrorActionContinue, ErrorActionContinueAndCooldown:
	default:
		return false
	}
	codes := r.StatusCodes[:0]
	for _, c := range r.StatusCodes {
		if c >= 100 && c <= 599 {
			codes = append(codes, c)
		}
	}
	r.StatusCodes = codes
	r.Match = strings.TrimSpace(r.Match)
	if r.Regex {
		if _, err := regexp.Compile(r.Match); err != nil {
			return false
		}
	}
	return true
}

// NormalizeModelPolicies canonicalizes aliases, exclusions, and error rules.
// Unusable error rules are dropped so a stale file cannot block startup.
func (c *Proxy) NormalizeModelPolicies() {
	aliases := c.ModelAliases[:0]
	seen := map[string]bool{}
	for _, a := range c.ModelAliases {
		a.Alias = strings.TrimSpace(a.Alias)
		a.Target = strings.TrimSpace(a.Target)
		a.Provider = strings.TrimSpace(a.Provider)
		if a.Alias == "" || a.Target == "" || seen[a.Alias] {
			continue
		}
		seen[a.Alias] = true
		aliases = append(aliases, a)
	}
	c.ModelAliases = aliases

	excluded := c.ExcludedModels[:0]
	for _, p := range c.ExcludedModels {
		if p = strings.TrimSpace(p); p != "" {
			excluded = append(excluded, p)
		}
	}
	c.ExcludedModels = excluded

	rules := c.RequestErrorRules[:0]
	for i := range c.RequestErrorRules {
		if c.RequestErrorRules[i].NormalizeRequestErrorRule() {
			rules = append(rules, c.RequestErrorRules[i])
		}
	}
	c.RequestErrorRules = rules
}

// AliasFor returns the alias definition for a client-visible model id.
func (c *Proxy) AliasFor(id string) (ModelAlias, bool) {
	for _, a := range c.ModelAliases {
		if a.Alias == id {
			return a, true
		}
	}
	return ModelAlias{}, false
}

// ruleRegexCache memoizes compiled error-rule patterns (rules are few and
// patterns are stable; compiling per request would be pure overhead).
var ruleRegexCache sync.Map // pattern -> *regexp.Regexp (nil entry = invalid)

func compileRuleRegex(pattern string) *regexp.Regexp {
	if v, ok := ruleRegexCache.Load(pattern); ok {
		re, _ := v.(*regexp.Regexp)
		return re
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		ruleRegexCache.Store(pattern, (*regexp.Regexp)(nil))
		return nil
	}
	ruleRegexCache.Store(pattern, re)
	return re
}

// MatchRequestError evaluates the configured rules in order against an
// upstream failure signature and returns the first matching action.
// A rule matches when every configured predicate it carries (status set,
// match string) accepts the error; a rule with no predicates is a catch-all.
func (c *Proxy) MatchRequestError(status int, message string) (string, bool) {
	for i := range c.RequestErrorRules {
		r := &c.RequestErrorRules[i]
		if len(r.StatusCodes) > 0 {
			ok := false
			for _, s := range r.StatusCodes {
				if s == status {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		if r.Match != "" {
			if r.Regex {
				re := compileRuleRegex(r.Match)
				if re == nil || !re.MatchString(message) {
					continue
				}
			} else if !strings.Contains(strings.ToLower(message), strings.ToLower(r.Match)) {
				continue
			}
		}
		action := r.Action
		if action == "" {
			action = ErrorActionContinue
		}
		return action, true
	}
	return "", false
}

// ExcludesModel reports whether id matches a configured exclusion pattern.
func (c *Proxy) ExcludesModel(id string) bool {
	for _, pattern := range c.ExcludedModels {
		if wildcardMatch(pattern, id) {
			return true
		}
	}
	return false
}

// ValidateModelPolicies returns a user-facing error for an invalid policy set.
func (c *Proxy) ValidateModelPolicies() error {
	seen := map[string]bool{}
	for _, a := range c.ModelAliases {
		if strings.TrimSpace(a.Alias) == "" || strings.TrimSpace(a.Target) == "" {
			return fmt.Errorf("model alias requires alias and target")
		}
		if seen[a.Alias] {
			return fmt.Errorf("duplicate model alias %q", a.Alias)
		}
		seen[a.Alias] = true
	}
	for _, r := range c.RequestErrorRules {
		if !r.NormalizeRequestErrorRule() {
			return fmt.Errorf("invalid request error rule (action=%q match=%q)", r.Action, r.Match)
		}
	}
	return nil
}

// wildcardMatch implements '*' and '?' glob matching without regexp cost.
func wildcardMatch(pattern, s string) bool {
	pi, si := 0, 0
	star, mark := -1, 0
	for si < len(s) {
		if pi < len(pattern) && (pattern[pi] == '?' || pattern[pi] == s[si]) {
			pi++
			si++
			continue
		}
		if pi < len(pattern) && pattern[pi] == '*' {
			star = pi
			mark = si
			pi++
			continue
		}
		if star >= 0 {
			pi = star + 1
			mark++
			si = mark
			continue
		}
		return false
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
