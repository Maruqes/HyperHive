package domain

import (
	"strings"
	"time"
)

// Automation is the Support-owned config of an HH Automation. Scripts and runs
// live on the master; Support decides when a condition fires.
type Automation struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Enabled        bool            `json:"enabled"`
	Condition      string          `json:"condition"`
	Cooldown       int             `json:"cooldown_seconds"`
	For            int             `json:"for_seconds"`
	Timeout        int             `json:"timeout_seconds"`
	MaxRunsPerHour int             `json:"max_runs_per_hour"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	State          AutomationState `json:"state"`
}

// AutomationState is the trigger state. Baseline means the next evaluation only
// records whether the condition is true, so enabling an automation whose
// condition is already true does not fire it.
type AutomationState struct {
	ConditionValue  *bool          `json:"condition_value"`
	LastEvaluatedAt *time.Time     `json:"last_evaluated_at"`
	LastError       string         `json:"last_error"`
	Armed           bool           `json:"armed"`
	Baseline        bool           `json:"baseline"`
	TrueSince       *time.Time     `json:"true_since"`
	LastFiredAt     *time.Time     `json:"last_fired_at"`
	LastFireError   string         `json:"last_fire_error"`
	DisabledReason  string         `json:"disabled_reason"`
	RecentFires     []time.Time    `json:"recent_fires"`
	Snapshot        map[string]any `json:"snapshot"`
}

// AutomationInput is the create/replace body. Pointers distinguish omitted
// values, which get defaults on create and are required on replace.
type AutomationInput struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Enabled        *bool  `json:"enabled"`
	Condition      string `json:"condition"`
	Cooldown       *int   `json:"cooldown_seconds"`
	For            *int   `json:"for_seconds"`
	Timeout        *int   `json:"timeout_seconds"`
	MaxRunsPerHour *int   `json:"max_runs_per_hour"`
}

type AutomationSettings struct {
	Paused bool `json:"paused"`
}

type AutomationList struct {
	Paused      bool         `json:"paused"`
	Automations []Automation `json:"automations"`
}

type ConditionResult struct {
	Value    *bool          `json:"value"`
	Error    string         `json:"error"`
	Snapshot map[string]any `json:"snapshot"`
}

// ConditionCatalog lists what a condition can read, for editor autocomplete.
type ConditionCatalog struct {
	Apps []CatalogApp `json:"apps"`
}

type CatalogApp struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Connectivity string          `json:"connectivity"`
	Health       string          `json:"health"`
	Instances    []string        `json:"instances"`
	Streams      []CatalogStream `json:"streams"`
}

// CatalogStream is a declared stream. Value is the current App-level value,
// absent when missing or stale.
type CatalogStream struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Unit  string `json:"unit,omitempty"`
	Label string `json:"label,omitempty"`
	Value any    `json:"value,omitempty"`
}

const (
	DefaultAutomationTimeout = 600
	DefaultMaxRunsPerHour    = 10
)

// ApplyAutomationInput copies input onto automation. On replace every numeric
// setting and enabled must be present.
func ApplyAutomationInput(automation *Automation, input AutomationInput, create bool) error {
	if !create && (input.Enabled == nil || input.Cooldown == nil || input.For == nil || input.Timeout == nil || input.MaxRunsPerHour == nil) {
		return Invalid("all automation settings are required")
	}
	automation.Name = strings.TrimSpace(input.Name)
	automation.Description = strings.TrimSpace(input.Description)
	automation.Condition = strings.TrimSpace(input.Condition)
	if create {
		automation.Enabled = false
		automation.Timeout = DefaultAutomationTimeout
		automation.MaxRunsPerHour = DefaultMaxRunsPerHour
	} else {
		automation.Enabled = *input.Enabled
	}
	if input.Cooldown != nil {
		automation.Cooldown = *input.Cooldown
	}
	if input.For != nil {
		automation.For = *input.For
	}
	if input.Timeout != nil {
		automation.Timeout = *input.Timeout
	}
	if input.MaxRunsPerHour != nil {
		automation.MaxRunsPerHour = *input.MaxRunsPerHour
	}
	return ValidateAutomation(*automation)
}

func ValidateAutomation(automation Automation) error {
	if !ValidLength(automation.Name, 1, 120) {
		return Invalid("name is required (maximum 120 characters)")
	}
	if !ValidLength(automation.Description, 0, 2000) {
		return Invalid("description is too long")
	}
	if !ValidLength(automation.Condition, 0, 4000) {
		return Invalid("condition is too long")
	}
	if automation.Cooldown < 0 || automation.Cooldown > 7*86400 {
		return Invalid("cooldown_seconds must be between 0 and 604800")
	}
	if automation.For < 0 || automation.For > 86400 {
		return Invalid("for_seconds must be between 0 and 86400")
	}
	if automation.Timeout < 1 || automation.Timeout > 86400 {
		return Invalid("timeout_seconds must be between 1 and 86400")
	}
	if automation.MaxRunsPerHour < 1 || automation.MaxRunsPerHour > 3600 {
		return Invalid("max_runs_per_hour must be between 1 and 3600")
	}
	if automation.Condition != "" {
		if _, err := CompileCondition(automation.Condition, nil); err != nil {
			return Invalid("invalid condition: " + err.Error())
		}
	}
	return nil
}
