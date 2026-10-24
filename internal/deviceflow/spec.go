// Package deviceflow implements provider device-code login strategies.
//
// It is deliberately free of project persistence: callers initiate a flow,
// poll it, and persist the returned Result through their own store. The package
// only performs HTTP against upstream authentication endpoints.
package deviceflow

// Spec describes the endpoints and field differences for one provider's
// device-code login. The data-driven shape keeps new providers to a new entry
// rather than a new control flow.
type Spec struct {
	InitiateURL   string // kilo: POST JSON empty body -> code / verificationUrl / expiresIn
	PollURLBase   string // kilo: GET {base}/{code}: 202 pending / 403 denied / 410 expired / 200+approved -> token
	ProfileURL    string // optional: GET bearer, fills in account info
	EmailPath     string // JSON path to email in ProfileURL response, e.g. "email"
	OrgIDPath     string // JSON path to organization id, e.g. "organizations.0.id"
	OrgHeader     string // when set, injects OrgIDPath as a dynamic request header (Kilo: X-Kilocode-OrganizationID)
	ModelsTestURL string // connectivity/probe endpoint (bearer + OrgHeader)
	IntervalSec   int    // poll interval (default 3)

	// rfc8628 mode (GitHub / Qwen Portal / Kimi): POST form authorization + poll, error field carries state.
	Mode          string // "" | "kilo" | "rfc8628" | "state-poll" | "qoder"
	TokenURL      string // rfc8628/state-poll/qoder poll endpoint
	ClientID      string
	Scope         string
	PKCE          bool   // authorization request carries code_challenge(S256); poll carries code_verifier
	UserCodeField string // user-code field name (default "user_code"; "code" for kilo)

	// state-poll mode (CodeBuddy family): POST {StateURL}?platform= -> {code:0,data:{state,authUrl}};
	// GET {TokenURL}?state= poll, code 11217=pending / code 0 + data.accessToken=success.
	StateURL  string
	Platform  string
	UserAgent string

	// qoder mode: locally generated PKCE, nonce and machine_id; the browser opens
	// {LoginURL}?challenge=&challenge_method=S256&machine_id=&nonce=; polling is
	// GET {TokenURL}?nonce=&verifier=&challenge_method=S256 (202/404 pending).
	LoginURL string

	// Optional second exchange: login token -> short-lived job token (GitHub: OAuth token -> copilot job token).
	ExchangeURL       string
	ExchangeTokenPath string
	ExchangeExpPath   string
}

// Specs returns the built-in provider flow specs. Callers own the returned map
// and may register additional flows (tests do this); built-in entries are
// copied so mutations cannot affect another caller.
func Specs() map[string]Spec {
	out := make(map[string]Spec, len(builtinSpecs))
	for k, v := range builtinSpecs {
		out[k] = v
	}
	return out
}

var builtinSpecs = map[string]Spec{
	"kilocode": {
		InitiateURL:   "https://api.kilo.ai/api/device-auth/codes",
		PollURLBase:   "https://api.kilo.ai/api/device-auth/codes",
		ProfileURL:    "https://api.kilo.ai/api/profile",
		EmailPath:     "email",
		OrgIDPath:     "organizations.0.id",
		OrgHeader:     "X-Kilocode-OrganizationID",
		ModelsTestURL: "https://api.kilo.ai/api/gateway/models",
		IntervalSec:   3,
	},
	"github": {
		Mode:              "rfc8628",
		InitiateURL:       "https://github.com/login/device/code",
		TokenURL:          "https://github.com/login/oauth/access_token",
		ClientID:          "Iv1.b507a08c87ecfe98",
		Scope:             "read:user",
		ProfileURL:        "https://api.github.com/user",
		EmailPath:         "login",
		ModelsTestURL:     "https://api.githubcopilot.com/models",
		ExchangeURL:       "https://api.github.com/copilot_internal/v2/token",
		ExchangeTokenPath: "token",
		ExchangeExpPath:   "expires_at",
		IntervalSec:       5,
	},
	"qwen": {
		Mode:          "rfc8628",
		InitiateURL:   "https://chat.qwen.ai/api/v1/oauth2/device/code",
		TokenURL:      "https://chat.qwen.ai/api/v1/oauth2/token",
		ClientID:      "f0304373b74a44d2b584a3fb70ca9e56",
		Scope:         "openid profile email model.completion",
		PKCE:          true,
		ModelsTestURL: "https://portal.qwen.ai/v1/models",
		IntervalSec:   3,
	},
	"kimi-coding": {
		Mode:          "rfc8628",
		InitiateURL:   "https://auth.kimi.com/api/oauth/device_authorization",
		TokenURL:      "https://auth.kimi.com/api/oauth/token",
		ClientID:      "17e5f671-d194-4dfb-9706-5516cb48c098",
		ModelsTestURL: "https://api.kimi.com/coding/v1/models",
		IntervalSec:   5,
	},
	"codebuddy-cn": {
		Mode:          "state-poll",
		StateURL:      "https://copilot.tencent.com/v2/plugin/auth/state",
		TokenURL:      "https://copilot.tencent.com/v2/plugin/auth/token",
		Platform:      "CLI",
		UserAgent:     "CLI/2.63.2 CodeBuddy/2.63.2",
		ModelsTestURL: "https://copilot.tencent.com/v2/models",
		IntervalSec:   5,
	},
	"codebuddy-intl": {
		Mode:          "state-poll",
		StateURL:      "https://www.codebuddy.ai/v2/plugin/auth/state",
		TokenURL:      "https://www.codebuddy.ai/v2/plugin/auth/token",
		Platform:      "ide",
		UserAgent:     "IDE/2.63.2 CodeBuddy/2.63.2",
		ModelsTestURL: "https://www.codebuddy.ai/v2/models",
		IntervalSec:   5,
	},
	"workbuddy": {
		Mode:          "state-poll",
		StateURL:      "https://www.workbuddy.ai/v2/plugin/auth/state",
		TokenURL:      "https://www.workbuddy.ai/v2/plugin/auth/token",
		Platform:      "workbuddy",
		UserAgent:     "CLI/2.63.2 WorkBuddy/2.63.2",
		ModelsTestURL: "https://www.workbuddy.ai/v2/models",
		IntervalSec:   5,
	},
	"qoder": {
		Mode:          "qoder",
		LoginURL:      "https://qoder.com/device/selectAccounts",
		TokenURL:      "https://openapi.qoder.sh/api/v1/deviceToken/poll",
		ProfileURL:    "https://openapi.qoder.sh/api/v1/userinfo",
		EmailPath:     "email",
		OrgIDPath:     "organization_id",
		ModelsTestURL: "https://openapi.qoder.sh/api/v1/userinfo",
		UserAgent:     "Go-http-client/2.0",
		IntervalSec:   2,
	},
	"qoder-cn": {
		Mode:          "qoder",
		LoginURL:      "https://qoder.com.cn/device/selectAccounts",
		TokenURL:      "https://openapi.qoder.com.cn/api/v1/deviceToken/poll",
		ProfileURL:    "https://openapi.qoder.com.cn/api/v1/userinfo",
		EmailPath:     "email",
		OrgIDPath:     "organization_id",
		ModelsTestURL: "https://openapi.qoder.com.cn/api/v1/userinfo",
		UserAgent:     "Go-http-client/2.0",
		IntervalSec:   2,
	},
}
