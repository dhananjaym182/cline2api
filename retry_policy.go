package main

import "time"

// Retry policy helpers: administrator-configured fallback breadth.
// Zero values always mean "historic built-in behavior", so an unconfigured
// instance routes exactly like before this feature.

// failoverModelCap bounds model attempts in the cline→zen failover loop.
func failoverModelCap() int {
	if n := getProxyConfig().Retry.MaxFailoverModels; n > 0 {
		return n
	}
	return 3
}

// retryRoundSettings returns the fallback-chain pass count (>=1), the account
// attempt cap per model per pass (0 = unlimited), and the wait between passes.
func retryRoundSettings() (rounds, credCap int, interval time.Duration) {
	r := getProxyConfig().Retry
	rounds = r.MaxRounds
	if rounds < 1 {
		rounds = 1
	}
	if r.MaxCredentialsPerRound > 0 {
		credCap = r.MaxCredentialsPerRound
	}
	if r.MaxIntervalSeconds > 0 {
		interval = time.Duration(r.MaxIntervalSeconds) * time.Second
	}
	return rounds, credCap, interval
}
