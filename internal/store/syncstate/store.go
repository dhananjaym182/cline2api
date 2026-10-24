// Package syncstate persists provider catalog recheck status. It performs no
// upstream calls.
package syncstate

import (
	"encoding/json"
	"os"

	"cline-go-proxy/internal/platform"
)

// FileName is the data file holding provider recheck status.
const FileName = ".cline-providers-sync.json"

// State is the persisted display status for one provider.
type State struct {
	LastCheckedMs  int64  `json:"lastCheckedMs"`
	LastError      string `json:"lastError,omitempty"`
	DelistedMarked int    `json:"delistedMarked,omitempty"`
	MissCounting   int    `json:"missCounting,omitempty"`
	NewQueued      int    `json:"newQueued,omitempty"`
	BatchID        string `json:"batchId,omitempty"`
}

// Load reads path. A missing or unreadable file yields nil and no error; a
// decode error yields nil and the error so the caller can back up the file.
func Load(path string) (map[string]*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var states map[string]*State
	if err := json.Unmarshal(data, &states); err != nil {
		return nil, err
	}
	return states, nil
}

// Save writes the state table atomically.
func Save(path string, states map[string]State) error {
	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(path, data, 0600)
}
