package domain

import (
	"strings"
	"testing"
)

func TestWidgetKindCompatibility(t *testing.T) {
	kinds := map[string]string{
		"value": "number text boolean", "line": "number", "area": "number", "bar": "number",
		"sparkline": "number", "gauge": "number", "progress": "number", "text": "text",
		"boolean": "boolean", "logs": "log", "events": "event", "table": "number text boolean log event",
	}
	for kind, allowed := range kinds {
		for _, streamType := range []string{"number", "text", "boolean", "log", "event"} {
			for _, pending := range []bool{false, true} {
				widget := Widget{ID: "widget", Stream: "cpu", Kind: kind, W: 6, H: 3}
				streams := map[string]Stream{}
				if pending {
					widget.StreamType = streamType
				} else {
					streams["cpu"] = Stream{Name: "cpu", Type: streamType}
				}
				err := ValidateLayout(Layout{Widgets: []Widget{widget}}, streams)
				wantValid := strings.Contains(" "+allowed+" ", " "+streamType+" ")
				if (err == nil) != wantValid {
					t.Errorf("kind=%s type=%s pending=%v: error=%v, want valid=%v", kind, streamType, pending, err, wantValid)
				}
			}
		}
	}
}

func TestWidgetStreamContracts(t *testing.T) {
	base := Widget{ID: "widget", Stream: "cpu", Kind: "line", W: 6, H: 3, StreamType: "number"}
	for _, test := range []struct {
		name    string
		change  func(*Widget)
		streams map[string]Stream
	}{
		{name: "missing expected type", change: func(w *Widget) { w.StreamType = "" }},
		{name: "invalid expected type", change: func(w *Widget) { w.StreamType = "json" }},
		{name: "empty name", change: func(w *Widget) { w.Stream = "" }},
		{name: "blank name", change: func(w *Widget) { w.Stream = " " }},
		{name: "long name", change: func(w *Widget) { w.Stream = strings.Repeat("x", 129) }},
		{name: "declared type mismatch", streams: map[string]Stream{"cpu": {Name: "cpu", Type: "text"}}},
		{name: "unknown kind", change: func(w *Widget) { w.Kind = "pie" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			widget := base
			if test.change != nil {
				test.change(&widget)
			}
			if err := ValidateLayout(Layout{Widgets: []Widget{widget}}, test.streams); err == nil {
				t.Fatal("invalid widget accepted")
			}
		})
	}
	other := base
	other.ID, other.Kind, other.StreamType = "other", "text", "text"
	if err := ValidateLayout(Layout{Widgets: []Widget{base, other}}, nil); err == nil {
		t.Fatal("contradictory pending stream types accepted")
	}
	other.Kind, other.StreamType = "line", ""
	if err := ValidateLayout(Layout{Widgets: []Widget{base, other}}, nil); err == nil {
		t.Fatal("pending widget must supply its own stream_type")
	}
	if err := ValidateLayout(Layout{Widgets: []Widget{base}}, map[string]Stream{"cpu": {Name: "cpu", Type: "number"}}); err != nil {
		t.Fatal("matching explicit stream type rejected", err)
	}
}
