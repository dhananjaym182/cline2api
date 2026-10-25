package adminapi

import (
	"fmt"
	"net/http"
	"time"

	"cline-go-proxy/internal/domain"
	"cline-go-proxy/internal/httpx"
)


// DefaultRequestLogLimit is the page size used when the caller does not ask for
// one. It matches the persisted request-log store's default.
const DefaultRequestLogLimit = 50

// Monitoring serves the read-only dashboard endpoints. Data access is injected:
// the composition layer owns the account pool and request-log store.
type Monitoring struct {
	Accounts        func() []*domain.Account
	Strategy        func() string
	ModelChain      func() []string
	Version         func() string
	StartedAt       func() time.Time
	OpencodeToday   func() any
	ListRequestLogs func(limit int, cursor string) (any, error)
	Translate       func(*http.Request, string) string
}

func (m *Monitoring) msg(r *http.Request, key string) string {
	if m.Translate != nil {
		return m.Translate(r, key)
	}
	return key
}

func (m *Monitoring) strategy() string {
	if m.Strategy != nil {
		return m.Strategy()
	}
	return ""
}

func (m *Monitoring) modelChain() []string {
	if m.ModelChain != nil {
		return m.ModelChain()
	}
	return nil
}

func (m *Monitoring) version() string {
	if m.Version != nil {
		return m.Version()
	}
	return ""
}

func (m *Monitoring) startedAt() (time.Time, bool) {
	if m.StartedAt == nil {
		return time.Time{}, false
	}
	t := m.StartedAt()
	if t.IsZero() {
		return time.Time{}, false
	}
	return t, true
}

// Stats handles GET /admin/api/stats.
func (m *Monitoring) Stats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: m.msg(r, "method_not_allowed")})
		return
	}

	var accounts []*domain.Account
	if m.Accounts != nil {
		accounts = m.Accounts()
	}
	active, cooldown, expired := 0, 0, 0
	var usageCount, promptTokens, completionTokens, totalTokens, cachedTokens int64
	for _, a := range accounts {
		usageCount += a.UsageCount
		promptTokens += a.PromptTokens
		completionTokens += a.CompletionTokens
		totalTokens += a.TotalTokens
		cachedTokens += a.CachedTokens
		switch a.Status {
		case "active":
			active++
		case "cooldown":
			cooldown++
		case "expired":
			expired++
		}
	}

	var opencodeToday any
	if m.OpencodeToday != nil {
		opencodeToday = m.OpencodeToday()
	}

	data := map[string]any{
		"total":            len(accounts),
		"active":           active,
		"cooldown":         cooldown,
		"expired":          expired,
		"usageCount":       usageCount,
		"promptTokens":     promptTokens,
		"completionTokens": completionTokens,
		"totalTokens":      totalTokens,
		"cachedTokens":     cachedTokens,
		"strategy":         m.strategy(),
		"modelChain":       m.modelChain(),
		"version":          m.version(),
		"opencodeToday":    opencodeToday,
	}
	if t, ok := m.startedAt(); ok {
		data["startedAt"] = t.UTC().Format(time.RFC3339)
		if secs := int64(time.Since(t).Seconds()); secs > 0 {
			data["uptimeSeconds"] = secs
		}
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{
		Success: true,
		Data:    data,
	})
}

// Logs handles GET /admin/api/request-logs?limit=50&cursor=...
func (m *Monitoring) Logs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: m.msg(r, "method_not_allowed")})
		return
	}

	limit := DefaultRequestLogLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
			httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: m.msg(r, "invalid_limit")})
			return
		}
		limit = n
	}
	cursor := r.URL.Query().Get("cursor")

	if m.ListRequestLogs == nil {
		httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true})
		return
	}
	page, err := m.ListRequestLogs(limit, cursor)
	if err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: err.Error()})
		return
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: page})
}
