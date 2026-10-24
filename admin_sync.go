package main

import (
	"net/http"

	"cline-go-proxy/internal/adminapi"
)

func providerSyncStatusSnapshot() map[string]any {
	loadProviderRecheckState()
	provRecheckMu.Lock()
	out := map[string]providerRecheckState{}
	for id, st := range providerRecheckStat {
		out[id] = *st
	}
	provRecheckMu.Unlock()
	for _, p := range listProviders() {
		if !p.Enabled {
			continue
		}
		if _, ok := out[p.ID]; !ok {
			out[p.ID] = providerRecheckState{}
		}
	}
	providers := make(map[string]any, len(out))
	for id, st := range out {
		providers[id] = st
	}
	return providers
}

var adminSync = &adminapi.Sync{
	ModelSync: func() (any, string) {
		res := triggerModelSync()
		return res, res.Error
	},
	ZenModelSync: func() (any, string) {
		res := syncZenModels()
		setLastZenModelSync(res)
		return res, res.Error
	},
	RecheckAll: func() any { return runAllProviderRechecks() },
	RecheckOne: func(id string) (any, error) {
		return recheckOneProvider(id)
	},
	SyncStatus: providerSyncStatusSnapshot,
	Translate:  func(r *http.Request, key string) string { return tAPI(r, key) },
}
