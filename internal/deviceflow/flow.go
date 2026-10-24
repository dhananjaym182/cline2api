package deviceflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cline-go-proxy/internal/platform"
)

const maxAuthBody = 256 << 10

// Initiation is the state produced by starting a device flow. The caller keeps
// it in memory until polling completes; none of it is persisted.
type Initiation struct {
	Code            string // device_code / state / nonce, sent back during polling
	UserCode        string // human-facing code or state
	VerificationURL string
	ExpiresIn       int // seconds
	Verifier        string
	MachineID       string // qoder only
}

// Result is a successful login result. The caller is responsible for mapping it
// onto its own connection record and store.
type Result struct {
	Token          string
	RefreshToken   string
	Email          string
	OrgID          string
	UserID         string // qoder upstream user_id, used when email is absent
	ExpiresAtMs    int64  // 0 = non-expiring (Kilo semantics)
	ExchangedToken string
	ExchangedExpMs int64
}

// Initiate starts a device authorization flow against the provider described by
// spec. It performs a single upstream request and returns the polling state.
func Initiate(ctx context.Context, client *http.Client, spec Spec) (*Initiation, error) {
	switch spec.Mode {
	case "qoder":
		return initiateQoder(spec)
	case "rfc8628":
		return initiateRFC8628(ctx, client, spec)
	case "state-poll":
		return initiateStatePoll(ctx, client, spec)
	default:
		return initiateKilo(ctx, client, spec)
	}
}

func initiateQoder(spec Spec) (*Initiation, error) {
	verifier, challenge, err := NewPKCE()
	if err != nil {
		return nil, err
	}
	machineID, err := NewUUIDv4()
	if err != nil {
		return nil, err
	}
	nonce, err := NewUUIDv4()
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("challenge", challenge)
	q.Set("challenge_method", "S256")
	q.Set("machine_id", machineID)
	q.Set("nonce", nonce)
	disp := strings.ToUpper(nonce)
	if len(disp) > 8 {
		disp = disp[:8]
	}
	return &Initiation{
		Code:            nonce,
		UserCode:        disp,
		VerificationURL: spec.LoginURL + "?" + q.Encode(),
		ExpiresIn:       300,
		Verifier:        verifier,
		MachineID:       machineID,
	}, nil
}

func initiateRFC8628(ctx context.Context, client *http.Client, spec Spec) (*Initiation, error) {
	form := url.Values{}
	form.Set("client_id", spec.ClientID)
	if spec.Scope != "" {
		form.Set("scope", spec.Scope)
	}
	var verifier string
	if spec.PKCE {
		v, challenge, err := NewPKCE()
		if err != nil {
			return nil, err
		}
		verifier = v
		form.Set("code_challenge", challenge)
		form.Set("code_challenge_method", "S256")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, spec.InitiateURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, dialHintError("initiate", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxAuthBody))
	data, err := decodeJSONBody("device auth initiation", resp, body)
	if err != nil {
		return nil, err
	}
	code := JSONPathString(data, "device_code")
	userDisp := JSONPathString(data, "user_code")
	verifURL := JSONPathString(data, "verification_uri_complete")
	if verifURL == "" {
		verifURL = JSONPathString(data, "verification_uri")
	}
	expiresIn := 600
	if n, ok := data["expires_in"].(float64); ok && n > 0 {
		expiresIn = int(n)
	}
	if code == "" {
		return nil, fmt.Errorf("initiate missing device_code")
	}
	if userDisp == "" {
		userDisp = code
	}
	return &Initiation{Code: code, UserCode: userDisp, VerificationURL: verifURL, ExpiresIn: expiresIn, Verifier: verifier}, nil
}

func initiateStatePoll(ctx context.Context, client *http.Client, spec Spec) (*Initiation, error) {
	target := spec.StateURL
	if spec.Platform != "" {
		target += "?platform=" + url.QueryEscape(spec.Platform)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	for k, v := range StatePollHeaders(spec, false) {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, dialHintError("initiate", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxAuthBody))
	data, err := decodeJSONBody("device auth initiation", resp, body)
	if err != nil {
		return nil, err
	}
	if c, _ := data["code"].(float64); c != 0 {
		msg, _ := data["msg"].(string)
		return nil, fmt.Errorf("state error: %s", platform.Truncate(msg, 160))
	}
	inner, _ := data["data"].(map[string]any)
	state, _ := inner["state"].(string)
	authURL, _ := inner["authUrl"].(string)
	if state == "" {
		return nil, fmt.Errorf("state missing in response")
	}
	if authURL == "" {
		authURL = state
	}
	return &Initiation{Code: state, UserCode: state, VerificationURL: authURL, ExpiresIn: 900}, nil
}

func initiateKilo(ctx context.Context, client *http.Client, spec Spec) (*Initiation, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, spec.InitiateURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, dialHintError("initiate", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxAuthBody))
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("too many pending authorization requests, try again later")
	}
	data, err := decodeJSONBody("device auth initiation", resp, body)
	if err != nil {
		return nil, err
	}
	code := JSONPathString(data, "code")
	verifURL := JSONPathString(data, "verificationUrl")
	expiresIn := 300
	if n, ok := data["expiresIn"].(float64); ok && n > 0 {
		expiresIn = int(n)
	}
	return &Initiation{Code: code, UserCode: code, VerificationURL: verifURL, ExpiresIn: expiresIn}, nil
}

// Poll waits for the user to approve the authorization and returns the
// resulting credentials. It respects ctx cancellation and the initiation's
// lifetime.
func Poll(ctx context.Context, client *http.Client, spec Spec, in *Initiation) (*Result, error) {
	if in == nil {
		return nil, fmt.Errorf("poll failed: nil initiation")
	}
	interval := spec.IntervalSec
	if interval <= 0 {
		interval = 3
	}
	ttl := time.Duration(in.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 600 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, ttl)
	defer cancel()

	for {
		timer := time.NewTimer(time.Duration(interval) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("device authorization expired (timeout)")
		case <-timer.C:
		}

		var body []byte
		var statusCode int

		switch spec.Mode {
		case "rfc8628":
			form := url.Values{}
			form.Set("client_id", spec.ClientID)
			form.Set("device_code", in.Code)
			form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
			if spec.PKCE && in.Verifier != "" {
				form.Set("code_verifier", in.Verifier)
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, spec.TokenURL, strings.NewReader(form.Encode()))
			if err != nil {
				return nil, fmt.Errorf("poll failed: %v", err)
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Accept", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("poll failed: %v", err)
			}
			body, _ = io.ReadAll(io.LimitReader(resp.Body, maxAuthBody))
			statusCode = resp.StatusCode
			resp.Body.Close()
		case "state-poll":
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.TokenURL+"?state="+url.QueryEscape(in.Code), nil)
			if err != nil {
				return nil, fmt.Errorf("poll failed: %v", err)
			}
			for k, v := range StatePollHeaders(spec, true) {
				req.Header.Set(k, v)
			}
			resp, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("poll failed: %v", err)
			}
			body, _ = io.ReadAll(io.LimitReader(resp.Body, maxAuthBody))
			statusCode = resp.StatusCode
			resp.Body.Close()
		case "qoder":
			q := url.Values{}
			q.Set("nonce", in.Code)
			q.Set("verifier", in.Verifier)
			q.Set("challenge_method", "S256")
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.TokenURL+"?"+q.Encode(), nil)
			if err != nil {
				return nil, fmt.Errorf("poll failed: %v", err)
			}
			req.Header.Set("Accept", "application/json")
			if spec.UserAgent != "" {
				req.Header.Set("User-Agent", spec.UserAgent)
			}
			resp, err := client.Do(req)
			if err != nil {
				return nil, dialHintError("poll", err)
			}
			body, _ = io.ReadAll(io.LimitReader(resp.Body, maxAuthBody))
			statusCode = resp.StatusCode
			resp.Body.Close()
			switch statusCode {
			case http.StatusOK:
			case http.StatusAccepted, http.StatusNotFound: // pending
				continue
			default:
				return nil, fmt.Errorf("poll failed: %d %s", statusCode, platform.Truncate(strings.TrimSpace(string(body)), 160))
			}
		default:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.PollURLBase+"/"+url.PathEscape(in.Code), nil)
			if err != nil {
				return nil, fmt.Errorf("poll failed: %v", err)
			}
			resp, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("poll failed: %v", err)
			}
			body, _ = io.ReadAll(io.LimitReader(resp.Body, maxAuthBody))
			statusCode = resp.StatusCode
			resp.Body.Close()
			switch statusCode {
			case http.StatusAccepted: // pending
				continue
			case http.StatusForbidden:
				return nil, fmt.Errorf("authorization denied by user")
			case http.StatusGone:
				return nil, fmt.Errorf("authorization code expired")
			case http.StatusOK:
			default:
				return nil, fmt.Errorf("poll failed: %d %s", statusCode, platform.Truncate(strings.TrimSpace(string(body)), 160))
			}
		}

		var data map[string]any
		if err := json.Unmarshal(body, &data); err != nil {
			if statusCode == http.StatusOK {
				return nil, fmt.Errorf("poll decode: %v", err)
			}
			continue // non-JSON transitional state (proxy/rate-limit page): keep waiting
		}

		res := &Result{}
		switch spec.Mode {
		case "state-poll":
			if statusCode != http.StatusOK {
				continue // transitional/proxy error: keep polling until expiry
			}
			c, _ := data["code"].(float64)
			inner, _ := data["data"].(map[string]any)
			switch {
			case c == 11217: // RetryFetchToken: waiting for browser authorization
				continue
			case c == 0 && inner != nil:
				res.Token, _ = inner["accessToken"].(string)
				if res.Token == "" {
					continue
				}
				res.RefreshToken, _ = inner["refreshToken"].(string)
				if n, ok := inner["expiresIn"].(float64); ok && n > 0 {
					res.ExpiresAtMs = time.Now().Add(time.Duration(n) * time.Second).UnixMilli()
				} else {
					res.ExpiresAtMs = time.Now().Add(24 * time.Hour).UnixMilli() // upstream default 86400s
				}
			default:
				msg, _ := data["msg"].(string)
				return nil, fmt.Errorf("poll failed: %s", platform.Truncate(msg, 160))
			}
		case "rfc8628":
			switch errCode, _ := data["error"].(string); errCode {
			case "authorization_pending", "slow_down", "":
			case "access_denied", "expired_token":
				return nil, fmt.Errorf("authorization denied or expired (%s)", errCode)
			default:
				desc, _ := data["error_description"].(string)
				return nil, fmt.Errorf("poll failed: %s %s", errCode, platform.Truncate(desc, 160))
			}
			res.Token, _ = data["access_token"].(string)
			res.RefreshToken, _ = data["refresh_token"].(string)
			if res.Token == "" {
				continue // not approved yet
			}
			if n, ok := data["expires_in"].(float64); ok && n > 0 {
				res.ExpiresAtMs = time.Now().Add(time.Duration(n) * time.Second).UnixMilli()
			}
		case "qoder":
			res.Token, _ = data["token"].(string)
			if res.Token == "" {
				return nil, fmt.Errorf("poll succeeded but response has no token")
			}
			res.RefreshToken, _ = data["refresh_token"].(string)
			res.UserID, _ = data["user_id"].(string)
			res.ExpiresAtMs = QoderExpiryMs(data)
		default:
			if s, _ := data["status"].(string); s != "approved" {
				continue // not approved yet
			}
			res.Token, _ = data["token"].(string)
			if res.Token == "" {
				return nil, fmt.Errorf("approved response missing token")
			}
			res.Email, _ = data["userEmail"].(string)
		}

		if spec.ProfileURL != "" {
			preq, _ := http.NewRequestWithContext(ctx, http.MethodGet, spec.ProfileURL, nil)
			preq.Header.Set("Authorization", "Bearer "+res.Token)
			preq.Header.Set("Accept", "application/json")
			if spec.Mode == "qoder" && spec.UserAgent != "" {
				preq.Header.Set("User-Agent", spec.UserAgent)
			}
			if presp, err := client.Do(preq); err == nil {
				pbody, _ := io.ReadAll(io.LimitReader(presp.Body, maxAuthBody))
				presp.Body.Close()
				var pdata map[string]any
				if json.Unmarshal(pbody, &pdata) == nil {
					if res.Email == "" && spec.EmailPath != "" {
						res.Email = JSONPathString(pdata, spec.EmailPath)
					}
					if spec.OrgIDPath != "" {
						res.OrgID = JSONPathString(pdata, spec.OrgIDPath)
					}
				}
			}
		}
		if spec.Mode == "qoder" && res.Email == "" && res.UserID != "" {
			res.Email = "qoder-user-" + res.UserID
		}
		if spec.ExchangeURL != "" {
			tok, expMs, err := ExchangeJobToken(ctx, client, spec, res.Token)
			if err != nil {
				return nil, err
			}
			res.ExchangedToken, res.ExchangedExpMs = tok, expMs
		}
		return res, nil
	}
}

// ExchangeJobToken performs the optional second exchange (GitHub Copilot):
// OAuth token -> short-lived job token.
func ExchangeJobToken(ctx context.Context, client *http.Client, spec Spec, accessToken string) (string, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.ExchangeURL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("token exchange failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("token exchange: %d %s", resp.StatusCode, platform.Truncate(strings.TrimSpace(string(body)), 160))
	}
	var data map[string]any
	if json.Unmarshal(body, &data) != nil {
		return "", 0, fmt.Errorf("token exchange decode")
	}
	token := JSONPathString(data, spec.ExchangeTokenPath)
	if token == "" {
		return "", 0, fmt.Errorf("token exchange missing %s", spec.ExchangeTokenPath)
	}
	var expMs int64
	switch v := data[spec.ExchangeExpPath].(type) {
	case float64:
		expMs = int64(v) * 1000
	case string:
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			expMs = n * 1000
		}
	}
	return token, expMs, nil
}

// RefreshRFC8628 performs a standard OAuth refresh-token exchange. It returns
// ok=false when the spec/token cannot refresh or the upstream rejects it; the
// caller keeps its existing expiry in that case.
func RefreshRFC8628(ctx context.Context, client *http.Client, spec Spec, refreshToken string, currentExpiresAt int64) (accessToken, newRefresh string, expiresAtMs int64, ok bool) {
	if spec.TokenURL == "" || refreshToken == "" {
		return "", "", 0, false
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	if spec.ClientID != "" {
		form.Set("client_id", spec.ClientID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, spec.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", 0, false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return "", "", 0, false
	}
	var data map[string]any
	if json.Unmarshal(body, &data) != nil {
		return "", "", 0, false
	}
	at, _ := data["access_token"].(string)
	if at == "" {
		return "", "", 0, false
	}
	rt, _ := data["refresh_token"].(string)
	if rt == "" {
		rt = refreshToken
	}
	expMs := currentExpiresAt
	if n, ok := data["expires_in"].(float64); ok && n > 0 {
		expMs = time.Now().Add(time.Duration(n) * time.Second).UnixMilli()
	}
	return at, rt, expMs, true
}
