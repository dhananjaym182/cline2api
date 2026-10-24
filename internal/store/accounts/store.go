// Package accounts persists the Cline account pool. It performs no upstream
// calls and no dispatch decisions.
package accounts

import (
	"encoding/json"
	"os"

	"cline-go-proxy/internal/domain"
	"cline-go-proxy/internal/platform"
)

// FileName is the data file holding the account pool.
const FileName = ".cline-accounts.json"

// Empty returns a pool with initialized slices so callers never see nils.
func Empty() *domain.AccountPool {
	return &domain.AccountPool{Accounts: []*domain.Account{}, Keys: []string{}, Models: []domain.Model{}}
}

// Load reads path. A missing or unreadable file yields an empty pool and no
// error; a decode error yields an empty pool and the error so the caller can
// back up the corrupt file.
func Load(path string) (*domain.AccountPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Empty(), nil
	}
	var p domain.AccountPool
	if err := json.Unmarshal(data, &p); err != nil {
		return Empty(), err
	}
	if p.Accounts == nil {
		p.Accounts = []*domain.Account{}
	}
	if p.Keys == nil {
		p.Keys = []string{}
	}
	if p.Models == nil {
		p.Models = []domain.Model{}
	}
	return &p, nil
}

// Save writes the pool atomically.
func Save(path string, p *domain.AccountPool) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(path, data, 0600)
}
