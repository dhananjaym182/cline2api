package adminapi

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"cline-go-proxy/internal/domain"
	"cline-go-proxy/internal/httpx"
)

// DeviceAuthorization is the provider-agnostic device-code grant.
type DeviceAuthorization struct {
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	Interval                int
	ExpiresIn               int
}

// OAuthToken is the WorkOS token pair returned by polling.
type OAuthToken struct {
	AccessToken  string
	RefreshToken string
}

// OAuthAccount is the Cline account registered from a completed OAuth grant.
type OAuthAccount struct {
	Email        string
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
}

type oauthSession struct {
	deviceCode string
	authURL    string
	createdAt  time.Time
	done       bool
	success    bool
	email      string
	errMsg     string
}

// OAuth serves the asynchronous device-code login flow used by the admin UI.
// Session state is in-memory; account persistence goes through AccountStore.
type OAuth struct {
	Store AccountStore
	// StartDevice initiates a device-code grant.
	StartDevice func() (DeviceAuthorization, error)
	// PollToken blocks until the user approves, denies, or the grant expires.
	PollToken func(deviceCode string, interval, expiresIn int) (OAuthToken, error)
	// RegisterAccount exchanges the WorkOS token for a Cline refresh token.
	RegisterAccount func(accessToken, refreshToken string) (OAuthAccount, error)
	Now             func() time.Time
	Translate       func(*http.Request, string) string
	Logf            func(format string, args ...any)

	mu       sync.Mutex
	sessions map[string]*oauthSession
}

func (h *OAuth) msg(r *http.Request, key string) string {
	if h.Translate != nil {
		return h.Translate(r, key)
	}
	return key
}

func (h *OAuth) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *OAuth) session(id string) (*oauthSession, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[id]
	return s, ok
}

// Start handles POST /admin/api/oauth/start.
func (h *OAuth) Start(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: h.msg(r, "method_not_allowed")})
		return
	}
	device, err := h.StartDevice()
	if err != nil {
		httpx.WriteAPI(w, http.StatusInternalServerError, httpx.Response{Error: err.Error()})
		return
	}
	authURL := device.VerificationURIComplete
	if authURL == "" {
		authURL = device.VerificationURI
	}

	sessionID := fmt.Sprintf("oauth_%d", h.now().UnixMilli())
	state := &oauthSession{
		deviceCode: device.DeviceCode,
		authURL:    authURL,
		createdAt:  h.now(),
	}
	h.mu.Lock()
	if h.sessions == nil {
		h.sessions = make(map[string]*oauthSession)
	}
	h.sessions[sessionID] = state
	h.mu.Unlock()

	go h.run(state, device)

	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: map[string]any{
		"sessionId":       sessionID,
		"verificationUri": authURL,
		"userCode":        device.UserCode,
	}})
}

func (h *OAuth) run(state *oauthSession, device DeviceAuthorization) {
	interval := device.Interval
	if interval < 5 {
		interval = 5
	}
	expiresIn := device.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 300
	}

	finish := func(err error, email string) {
		h.mu.Lock()
		state.done = true
		if err != nil {
			state.errMsg = err.Error()
			state.success = false
		} else {
			state.success = true
			state.email = email
		}
		h.mu.Unlock()
	}

	workosTok, err := h.PollToken(device.DeviceCode, interval, expiresIn)
	if err != nil {
		finish(err, "")
		return
	}
	reg, err := h.RegisterAccount(workosTok.AccessToken, workosTok.RefreshToken)
	if err != nil {
		finish(err, "")
		return
	}

	email := reg.Email
	if email == "" {
		email = "unknown"
	}
	now := h.now()
	h.Store.Add(&domain.Account{
		AccountID:    fmt.Sprintf("acc_%d", now.UnixMilli()),
		Email:        email,
		RefreshToken: reg.RefreshToken,
		AccessToken:  "workos:" + reg.AccessToken,
		ExpiresAt:    reg.ExpiresAt - 60000,
		Status:       "active",
		CreatedAt:    now,
	})

	finish(nil, email)
	if h.Logf != nil {
		h.Logf("OAuth account added: %s", email)
	}
}

// Status handles GET /admin/api/oauth/status?sessionId=...
func (h *OAuth) Status(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "session_id_required")})
		return
	}
	state, ok := h.session(sessionID)
	if !ok {
		httpx.WriteAPI(w, http.StatusNotFound, httpx.Response{Error: h.msg(r, "session_not_found")})
		return
	}

	h.mu.Lock()
	resp := map[string]any{"done": state.done, "success": state.success}
	if state.done {
		resp["email"] = state.email
		if !state.success {
			resp["error"] = state.errMsg
		}
	}
	h.mu.Unlock()

	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: resp})
}
