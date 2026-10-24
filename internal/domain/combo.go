package domain

import "time"

// ComboEntry is one ordered entry in a model combo.
type ComboEntry struct {
	Model    string `json:"model"`
	Disabled bool   `json:"disabled,omitempty"`
	// Source pins an upstream: "", "cline", "zen", or "provider".
	Source string `json:"source,omitempty"`
	// Provider is the target custom provider when Source == "provider".
	Provider string `json:"provider,omitempty"`
}

// ModelCombo is a named, ordered, enableable model combo.
type ModelCombo struct {
	ID                string       `json:"id"`
	Name              string       `json:"name"`
	Description       string       `json:"description,omitempty"`
	Category          string       `json:"category,omitempty"`
	Enabled           bool         `json:"enabled"`
	EmergencyFallback bool         `json:"emergencyFallback,omitempty"`
	Entries           []ComboEntry `json:"entries"`
	CreatedAt         time.Time    `json:"createdAt"`
	UpdatedAt         time.Time    `json:"updatedAt"`
}
