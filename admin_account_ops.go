package main

import (
	"log"
	"time"

	"cline-go-proxy/internal/adminapi"
	"cline-go-proxy/internal/domain"
)

func snapshotAccounts() []*domain.Account {
	p := loadPool()
	poolMu.Lock()
	out := make([]*domain.Account, len(p.Accounts))
	copy(out, p.Accounts)
	poolMu.Unlock()
	return out
}

func refreshAllAccounts() {
	p := loadPool()
	poolMu.Lock()
	for _, a := range p.Accounts {
		if err := refreshAccountToken(a); err != nil {
			log.Printf("Refresh failed for %s: %v", a.Email, err)
		}
	}
	poolMu.Unlock()
}

func clearAccountPool() {
	poolMu.Lock()
	pool = &AccountPool{Accounts: []*Account{}, Keys: []string{}}
	poolMu.Unlock()
	savePool()
}

var adminAccountOps = &adminapi.AccountOps{
	All:          snapshotAccounts,
	Get:          getAccountByID,
	DoRefreshAll: refreshAllAccounts,
	Refresh:      refreshAccountToken,
	ClearAll:     clearAccountPool,
	Probe:        func(acc *domain.Account) any { return testAccount(acc) },
	OpenBrowser:  openBrowser,
	Now:          time.Now,
	Translate:    tAPI,
}
