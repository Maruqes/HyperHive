package domain

import (
	"encoding/json"
	"time"
)

type Tab struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Instance struct {
	ID            string     `json:"instance_id"`
	LastHeartbeat *time.Time `json:"last_heartbeat_at"`
	Connectivity  string     `json:"connectivity"`
	Health        string     `json:"health"`
	HealthMessage string     `json:"health_message"`
}
type App struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	TabID         string     `json:"tab_id"`
	Tags          []string   `json:"tags"`
	Timeout       int        `json:"heartbeat_timeout_seconds"`
	Retention     int        `json:"retention_days"`
	Connectivity  string     `json:"connectivity"`
	Health        string     `json:"health"`
	HealthMessage string     `json:"health_message"`
	LastHeartbeat *time.Time `json:"last_heartbeat_at"`
	Instances     []Instance `json:"instances"`
}
type Stream struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Unit  string `json:"unit"`
	Label string `json:"label"`
}
type Sample struct {
	ID         int64           `json:"id"`
	AppID      string          `json:"app_id"`
	Stream     string          `json:"stream"`
	InstanceID *string         `json:"instance_id"`
	Timestamp  time.Time       `json:"timestamp"`
	ReceivedAt time.Time       `json:"received_at"`
	Value      json.RawMessage `json:"value"`
}
type Widget struct {
	ID         string   `json:"id"`
	Stream     string   `json:"stream"`
	StreamType string   `json:"stream_type,omitempty"`
	Kind       string   `json:"kind"`
	X          int      `json:"x"`
	Y          int      `json:"y"`
	W          int      `json:"w"`
	H          int      `json:"h"`
	Min        *float64 `json:"min,omitempty"`
	Max        *float64 `json:"max,omitempty"`
	Window     *int     `json:"window_minutes,omitempty"`
	Severity   []string `json:"severity,omitempty"`
}
type Layout struct {
	Revision int64    `json:"revision"`
	Widgets  []Widget `json:"widgets"`
}
type HealthReport struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}
type IngestSample struct {
	Stream    string          `json:"stream"`
	Timestamp *time.Time      `json:"timestamp"`
	Value     json.RawMessage `json:"value"`
}
type IngestRequest struct {
	InstanceID *string        `json:"instance_id"`
	Heartbeat  bool           `json:"heartbeat"`
	Health     *HealthReport  `json:"health"`
	Streams    []Stream       `json:"streams"`
	Samples    []IngestSample `json:"samples"`
}
type AppSettings struct {
	Name      string   `json:"name"`
	TabID     string   `json:"tab_id"`
	Tags      []string `json:"tags"`
	Timeout   *int     `json:"heartbeat_timeout_seconds"`
	Retention *int     `json:"retention_days"`
}
type ReplicateAppSettings struct {
	Name  string   `json:"name"`
	TabID string   `json:"tab_id"`
	Tags  []string `json:"tags"`
}
type AppDetail struct {
	App     App      `json:"app"`
	Streams []Stream `json:"streams"`
	Layout  Layout   `json:"layout"`
}
type HistoryQuery struct {
	Stream   string
	Limit    int
	Before   *int64
	After    *int64
	Since    *time.Time
	Severity []string
}
type History struct {
	Samples    []Sample `json:"samples"`
	NextBefore *int64   `json:"next_before"`
}
