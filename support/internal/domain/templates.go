package domain

func TemplateLayout(id string) (Layout, error) {
	layout := Layout{Widgets: []Widget{}}
	switch id {
	case "":
	case "small":
		layout.Widgets = []Widget{
			{ID: "cpu-value", Stream: "cpu", StreamType: "number", Kind: "value", X: 0, Y: 0, W: 4, H: 2},
			{ID: "cpu-trend", Stream: "cpu", StreamType: "number", Kind: "line", X: 4, Y: 0, W: 8, H: 3},
			{ID: "logs", Stream: "logs", StreamType: "log", Kind: "logs", X: 0, Y: 3, W: 12, H: 4},
		}
	case "linux-machine":
		layout.Widgets = []Widget{
			{ID: "machine-info", Stream: "machine_info", StreamType: "text", Kind: "text", X: 0, Y: 0, W: 4, H: 3},
			{ID: "uptime", Stream: "uptime_seconds", StreamType: "number", Kind: "value", X: 0, Y: 3, W: 2, H: 1},
			{ID: "cpu-usage", Stream: "cpu_usage_percent", StreamType: "number", Kind: "value", X: 2, Y: 3, W: 2, H: 1},
			{ID: "memory-usage", Stream: "memory_usage_percent", StreamType: "number", Kind: "value", X: 0, Y: 4, W: 2, H: 1},
			{ID: "root-usage", Stream: "root_usage_percent", StreamType: "number", Kind: "value", X: 2, Y: 4, W: 2, H: 1},
			{ID: "service-failures", Stream: "service_failures", StreamType: "event", Kind: "events", X: 4, Y: 0, W: 8, H: 5},
			{ID: "system-logs", Stream: "system_logs", StreamType: "log", Kind: "logs", X: 0, Y: 5, W: 12, H: 6, Severity: []string{"warn", "error", "fatal"}},
			{ID: "journal-logs", Stream: "journal_logs", StreamType: "log", Kind: "logs", X: 0, Y: 11, W: 12, H: 6},
			{ID: "kernel-logs", Stream: "kernel_logs", StreamType: "log", Kind: "logs", X: 0, Y: 17, W: 6, H: 5},
			{ID: "service-logs", Stream: "service_logs", StreamType: "log", Kind: "logs", X: 6, Y: 17, W: 6, H: 5},
			{ID: "machine-events", Stream: "machine_events", StreamType: "event", Kind: "events", X: 0, Y: 22, W: 6, H: 4},
			{ID: "services", Stream: "services", StreamType: "event", Kind: "events", X: 6, Y: 22, W: 6, H: 4},
			{ID: "cpu-trend", Stream: "cpu_usage_percent", StreamType: "number", Kind: "line", X: 0, Y: 26, W: 6, H: 3},
			{ID: "memory-trend", Stream: "memory_usage_percent", StreamType: "number", Kind: "area", X: 6, Y: 26, W: 6, H: 3},
			{ID: "network-receive", Stream: "network_receive_bytes_per_second", StreamType: "number", Kind: "line", X: 0, Y: 29, W: 6, H: 3},
			{ID: "network-transmit", Stream: "network_transmit_bytes_per_second", StreamType: "number", Kind: "line", X: 6, Y: 29, W: 6, H: 3},
			{ID: "filesystems", Stream: "filesystems", StreamType: "event", Kind: "events", X: 0, Y: 32, W: 6, H: 4},
			{ID: "network-interfaces", Stream: "network_interfaces", StreamType: "event", Kind: "events", X: 6, Y: 32, W: 6, H: 4},
			{ID: "top-processes", Stream: "top_processes", StreamType: "event", Kind: "events", X: 0, Y: 36, W: 6, H: 4},
			{ID: "hardware", Stream: "hardware", StreamType: "event", Kind: "events", X: 6, Y: 36, W: 6, H: 4},
		}
	default:
		return Layout{}, Invalid("unknown App template")
	}
	return layout, ValidateLayout(layout, nil)
}
