package main

import (
	"log"
	"net/http"

	"cline-go-proxy/internal/adminapi"
)

var adminConfig = &adminapi.Config{
	Proxy: getProxyConfig,
	SetProxy: func(cfg *proxyConfigData) {
		setProxyConfig(cfg)
	},
	Models:       getAllModels,
	DefaultModel: getDefaultModel,
	SetDefaultModel: func(model string) {
		p := loadPool()
		poolMu.Lock()
		p.DefaultModel = model
		poolMu.Unlock()
		savePool()
	},
	SetListenHost: func(host string) {
		p := loadPool()
		poolMu.Lock()
		p.ListenHost = host
		poolMu.Unlock()
		savePool()
	},
	LocalIPs:           detectLocalIPs,
	ListenHost:         func() string { return listenHost },
	ListenPort:         func() int { return listenPort },
	EffectiveAdminHost: effectiveAdminHost,
	Version:            appVersion,
	ZenHeaders:         func() map[string]string { return getZenConfig().ZenHeaders },
	PoolPath:           poolPath,
	HasPassword:        func() bool { return loadPool().AdminPasswordHash != "" },
	RestartListener:    restartListener,
	Translate:          func(r *http.Request, key string) string { return tAPI(r, key) },
	Logf:               log.Printf,
}
