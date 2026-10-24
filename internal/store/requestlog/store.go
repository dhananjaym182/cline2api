// Package requestlog persists request attribution records. It performs no
// upstream calls and no routing decisions.
package requestlog

import (
	"encoding/json"
	"os"

	"cline-go-proxy/internal/domain"
	"cline-go-proxy/internal/platform"
)

// FileName is the data file holding request logs.
const FileName = ".cline-request-logs.json"

// Load reads path. A missing or unreadable file yields nil and no error; a
// decode error yields nil and the error so the caller can back up the file.
func Load(path string) ([]domain.RequestLog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var entries []domain.RequestLog
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// Save writes the log slice atomically.
func Save(path string, entries []domain.RequestLog) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(path, data, 0600)
}
