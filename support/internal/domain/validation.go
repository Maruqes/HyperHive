package domain

import (
	"encoding/json"
	"math"
	"strings"
	"unicode/utf8"
)

func ValidLength(value string, min, max int) bool {
	count := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && count >= min && count <= max
}
func ValidSeverity(value string) bool {
	switch value {
	case "debug", "info", "warn", "error", "fatal":
		return true
	}
	return false
}
func ValidStreamType(value string) bool {
	switch value {
	case "number", "text", "boolean", "log", "event":
		return true
	}
	return false
}
func NormalizeApp(app *App) error {
	app.Name = strings.TrimSpace(app.Name)
	if !ValidLength(app.Name, 1, 120) || app.TabID == "" {
		return Invalid("name and tab_id are required")
	}
	if app.Timeout < 10 || app.Timeout > 86400 || app.Retention < 1 || app.Retention > 365 {
		return Invalid("heartbeat timeout or retention is out of range")
	}
	if len(app.Tags) > 32 {
		return Invalid("maximum 32 tags")
	}
	tags := []string{}
	seen := map[string]bool{}
	for _, tag := range app.Tags {
		tag = strings.TrimSpace(tag)
		if !ValidLength(tag, 1, 64) {
			return Invalid("invalid tag")
		}
		if !seen[tag] {
			tags = append(tags, tag)
			seen[tag] = true
		}
	}
	app.Tags = tags
	return nil
}
func ValidateValue(kind string, value json.RawMessage) error {
	var decoded any
	if len(value) == 0 || json.Unmarshal(value, &decoded) != nil || decoded == nil {
		return Invalid("sample value is required")
	}
	switch kind {
	case "number":
		number, ok := decoded.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return Invalid("number value must be finite")
		}
	case "text":
		text, ok := decoded.(string)
		if !ok || len(text) > 16*1024 {
			return Invalid("invalid text value")
		}
	case "boolean":
		if _, ok := decoded.(bool); !ok {
			return Invalid("invalid boolean value")
		}
	case "log":
		var log struct {
			Message  *string `json:"message"`
			Severity string  `json:"severity"`
		}
		if StrictJSON(value, &log) != nil || log.Message == nil || len(*log.Message) > 16*1024 || !ValidSeverity(log.Severity) {
			return Invalid("invalid log value")
		}
	case "event":
		var event struct {
			Name   string                     `json:"name"`
			Fields map[string]json.RawMessage `json:"fields"`
		}
		if StrictJSON(value, &event) != nil || !ValidLength(event.Name, 1, 120) || event.Fields == nil {
			return Invalid("invalid event value")
		}
		fields, _ := json.Marshal(event.Fields)
		if len(fields) > 16*1024 {
			return Invalid("event fields exceed 16 KiB")
		}
	default:
		return Invalid("invalid stream type")
	}
	return nil
}
func ValidateLayout(layout Layout, streams map[string]Stream) error {
	if layout.Revision < 0 || len(layout.Widgets) > 50 {
		return Invalid("invalid layout revision or widget count")
	}
	seen := map[string]bool{}
	types := map[string]string{}
	for name, stream := range streams {
		types[name] = stream.Type
	}
	for _, widget := range layout.Widgets {
		if !ValidLength(widget.ID, 1, 128) || seen[widget.ID] || widget.X < 0 || widget.Y < 0 || widget.W < 1 || widget.W > 12 || widget.H < 1 || widget.X > 12-widget.W {
			return Invalid("invalid widget identity or grid position")
		}
		seen[widget.ID] = true
		if !ValidLength(widget.Stream, 1, 128) || strings.TrimSpace(widget.Stream) == "" {
			return Invalid("invalid widget stream name")
		}
		if _, exists := streams[widget.Stream]; !exists && widget.StreamType == "" {
			return Invalid("stream_type is required for an undeclared widget stream")
		}
		streamType := types[widget.Stream]
		if widget.StreamType != "" {
			if !ValidStreamType(widget.StreamType) {
				return Invalid("invalid widget stream_type")
			}
			if streamType != "" && streamType != widget.StreamType {
				return Invalid("widget stream_type does not match stream")
			}
			streamType = widget.StreamType
			types[widget.Stream] = streamType
		}
		compatible := false
		switch widget.Kind {
		case "value":
			compatible = streamType == "number" || streamType == "text" || streamType == "boolean"
		case "line", "area", "bar", "sparkline", "gauge", "progress":
			compatible = streamType == "number"
		case "text":
			compatible = streamType == "text"
		case "boolean":
			compatible = streamType == "boolean"
		case "logs":
			compatible = streamType == "log"
		case "events":
			compatible = streamType == "event"
		case "table":
			compatible = true
		}
		if !compatible {
			return Invalid("widget kind is incompatible with stream")
		}
		minimum, maximum := 0.0, 100.0
		if widget.Min != nil {
			minimum = *widget.Min
		}
		if widget.Max != nil {
			maximum = *widget.Max
		}
		if math.IsNaN(minimum) || math.IsInf(minimum, 0) || math.IsNaN(maximum) || math.IsInf(maximum, 0) || minimum >= maximum {
			return Invalid("invalid gauge range")
		}
		if widget.Window != nil && (*widget.Window < 1 || *widget.Window > 365*24*60) {
			return Invalid("invalid history window")
		}
		for _, severity := range widget.Severity {
			if !ValidSeverity(severity) {
				return Invalid("invalid severity")
			}
		}
	}
	return nil
}
