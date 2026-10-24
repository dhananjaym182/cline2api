package main

import (
	"net/http"

	"cline-go-proxy/internal/adminapi"
	"cline-go-proxy/internal/domain"
)

// rootComboStore adapts the combo registry to adminapi.ComboStore.
type rootComboStore struct{}

func (rootComboStore) List() []domain.ModelCombo { return listCombos() }

func (rootComboStore) Catalog() []map[string]any { return combosCatalog() }

func (rootComboStore) Find(id string) (domain.ModelCombo, bool) { return findCombo(id) }

func (rootComboStore) Upsert(c domain.ModelCombo) (domain.ModelCombo, error) {
	return upsertCombo(c)
}

func (rootComboStore) Delete(id string) bool { return deleteCombo(id) }

func (rootComboStore) SetEnabled(id string, enabled bool) (domain.ModelCombo, error) {
	return setComboEnabled(id, enabled)
}

func (rootComboStore) Clone(id string) (domain.ModelCombo, error) { return cloneCombo(id) }

var adminCombos = &adminapi.Combos{
	Store:         rootComboStore{},
	LocalizeError: localizeComboError,
	Translate:     func(r *http.Request, key string) string { return tAPI(r, key) },
}
