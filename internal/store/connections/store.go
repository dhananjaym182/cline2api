// Package connections persists provider login connections. It performs no
// authentication flows and no upstream calls.
package connections

import (
	"encoding/json"
	"os"

	"cline-go-proxy/internal/domain"
	"cline-go-proxy/internal/platform"
)

// FileName is the data file holding provider connections.
const FileName = ".cline-connections.json"

// Load reads path. A missing or unreadable file yields nil and no error; a
// decode error yields nil and the error so the caller can back up the file.
func Load(path string) ([]*domain.Connection, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var list []*domain.Connection
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// Save writes the connection list atomically.
func Save(path string, list []*domain.Connection) error {
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(path, data, 0600)
}
