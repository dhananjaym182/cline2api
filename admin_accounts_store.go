package main

import (
	"log"
	"time"

	"cline-go-proxy/internal/adminapi"
	"cline-go-proxy/internal/domain"
)

// rootAccountStore adapts the package-level account pool to adminapi.AccountStore.
type rootAccountStore struct{}

func (rootAccountStore) List() []*domain.Account { return listAccounts() }

func (rootAccountStore) PoolIndex() int { return loadPool().CurrentIdx }

func (rootAccountStore) FindByRefreshToken(token string) *domain.Account {
	return findAccountByRefreshToken(token)
}

func (rootAccountStore) Add(acc *domain.Account) { addAccount(acc) }

func (rootAccountStore) Remove(accountID string) bool { return removeAccount(accountID) }

func validateNewAccountRefreshToken(token string) (adminapi.RefreshedAccount, error) {
	resp, err := refreshClineToken(token)
	if err != nil {
		return adminapi.RefreshedAccount{}, err
	}
	return adminapi.RefreshedAccount{
		AccessToken:  resp.Data.AccessToken,
		RefreshToken: resp.Data.RefreshToken,
		ExpiresAt:    parseExpiry(resp.Data.ExpiresAt),
	}, nil
}

var adminAccounts = &adminapi.Accounts{
	Store:     rootAccountStore{},
	Refresh:   validateNewAccountRefreshToken,
	Now:       time.Now,
	Translate: tAPI,
	Logf:      log.Printf,
}
