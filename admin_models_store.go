package main

import (
	"net/http"

	"cline-go-proxy/internal/adminapi"
	"cline-go-proxy/internal/domain"
)

// rootModelStore adapts the account pool's model catalog to adminapi.ModelStore.
// Locking and persistence stay in the root composition layer.
type rootModelStore struct{}

func (rootModelStore) List() []domain.Model {
	return getAllModels()
}

func (rootModelStore) Add(m domain.Model) {
	p := loadPool()
	poolMu.Lock()
	p.Models = append(p.Models, m)
	poolMu.Unlock()
	savePool()
}

func (rootModelStore) Delete(id string) adminapi.DeleteModelResult {
	p := loadPool()
	poolMu.Lock()
	for i, m := range p.Models {
		if m.ID != id {
			continue
		}
		// Custom models may be removed; synchronized models only once delisted.
		if !m.Custom && !m.Delisted {
			poolMu.Unlock()
			return adminapi.ModelBuiltin
		}
		p.Models = append(p.Models[:i], p.Models[i+1:]...)
		if p.DefaultModel == id {
			p.DefaultModel = ""
		}
		poolMu.Unlock()
		savePool()
		return adminapi.ModelDeleted
	}
	poolMu.Unlock()
	return adminapi.ModelNotFound
}

func (rootModelStore) UpdateContext(id string, context, output int) bool {
	p := loadPool()
	poolMu.Lock()
	for i := range p.Models {
		if p.Models[i].ID != id {
			continue
		}
		p.Models[i].Context = context
		p.Models[i].Output = output
		// Zeroing both returns the limits to unknown; otherwise the user locks them
		// against sync overwrites.
		p.Models[i].MetaLocked = !(context == 0 && output == 0)
		poolMu.Unlock()
		savePool()
		return true
	}
	poolMu.Unlock()
	return false
}

func (rootModelStore) UpdateCapabilities(id string, caps adminapi.ModelCapabilities) bool {
	p := loadPool()
	poolMu.Lock()
	for i := range p.Models {
		if p.Models[i].ID != id {
			continue
		}
		// nil means "leave unchanged" (the handler distinguishes it from an
		// explicit empty list, which clears the field).
		if caps.ThinkingLevels != nil {
			p.Models[i].ThinkingLevels = caps.ThinkingLevels
		}
		if caps.InputModalities != nil {
			p.Models[i].InputModalities = caps.InputModalities
		}
		if caps.OutputModalities != nil {
			p.Models[i].OutputModalities = caps.OutputModalities
		}
		poolMu.Unlock()
		savePool()
		return true
	}
	poolMu.Unlock()
	return false
}

func (rootModelStore) SetDisabled(ids []string, disabled bool) int {
	target := make(map[string]bool, len(ids))
	for _, id := range ids {
		target[id] = true
	}
	p := loadPool()
	poolMu.Lock()
	changed := 0
	for i := range p.Models {
		if !target[p.Models[i].ID] {
			continue
		}
		p.Models[i].Disabled = disabled
		changed++
		if disabled && p.DefaultModel == p.Models[i].ID {
			p.DefaultModel = ""
		}
	}
	poolMu.Unlock()
	if changed > 0 {
		savePool()
	}
	return changed
}

// normalizeAdminModels applies the billing normalization the admin UI expects:
// zen free models always display as free, matching route classification.
func normalizeAdminModels(models []domain.Model) []domain.Model {
	for i := range models {
		if isZenSource(models[i]) && isZenFreeModel(models[i]) && models[i].Cost != "free" {
			models[i].Cost = "free"
		}
	}
	return models
}

var adminModels = &adminapi.Models{
	Store:     rootModelStore{},
	LastSync:  func() any { return getModelSyncResult() },
	Normalize: normalizeAdminModels,
	Translate: func(r *http.Request, key string) string { return tAPI(r, key) },
}
