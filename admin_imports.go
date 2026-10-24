package main

import (
	"time"

	"cline-go-proxy/internal/adminapi"
	"cline-go-proxy/internal/platform"
)

var adminImports = &adminapi.Imports{
	Store:       rootAccountStore{},
	Refresh:     validateNewAccountRefreshToken,
	IsDuplicate: isDuplicateImportToken,
	Truncate:    platform.Truncate,
	Now:         time.Now,
	Translate:   tAPI,
}
