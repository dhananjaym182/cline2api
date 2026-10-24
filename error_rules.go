package main

import (
	"errors"
	"log"
	"time"

	"cline-go-proxy/internal/config"
)

// Request error rules give the administrator per-signature control over
// fallback behavior (stop/continue, with or without a cooldown). Rules are
// evaluated before the built-in classifiers (402/429/deterministic-400);
// with no matching rule the built-in classification stays in charge.

// requestErrorStop marks a fallback failure that a stop rule declared
// terminal. Chain loops abort on it and the outermost router unwraps it
// before reporting the error to the client.
type requestErrorStop struct{ err error }

func (e *requestErrorStop) Error() string { return e.err.Error() }
func (e *requestErrorStop) Unwrap() error { return e.err }

// ruleDecision evaluates configured rules against err. Non-API errors and
// unmatched failures report matched=false.
func ruleDecision(err error) (stop, cooldown, matched bool) {
	if _, already := err.(*requestErrorStop); already {
		return false, false, false
	}
	var apiErr *clineAPIError
	if !errors.As(err, &apiErr) {
		return false, false, false
	}
	action, ok := getProxyConfig().MatchRequestError(apiErr.statusCode, apiErr.message)
	if !ok {
		return false, false, false
	}
	switch action {
	case config.ErrorActionStop:
		return true, false, true
	case config.ErrorActionStopAndCooldown:
		return true, true, true
	case config.ErrorActionContinueAndCooldown:
		return false, true, true
	default:
		return false, false, true
	}
}

// clineErrorStatus extracts the upstream status carried by err (0 when absent).
func clineErrorStatus(err error) int {
	var apiErr *clineAPIError
	if errors.As(err, &apiErr) {
		return apiErr.statusCode
	}
	return 0
}

// ruleCooldownUntil reuses the provider failure classification when it has an
// opinion about the status; otherwise applies a short 2-minute penalty.
func ruleCooldownUntil(status int) time.Time {
	if until := providerCooldownUntil(status, "", ""); !until.IsZero() {
		return until
	}
	return time.Now().Add(2 * time.Minute)
}

// logRuleHit keeps rule-driven decisions distinguishable from built-in ones
// in the server log.
func logRuleHit(action string, status int, subject string) {
	log.Printf("  error rule: action=%s status=%d subject=%s", action, status, subject)
}
