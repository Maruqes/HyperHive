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
			{ID: "machine-info", Stream: "machine_info", StreamType: "text", Kind: "text", X: 0, Y: 0, W: 6, H: 3},
			{ID: "uptime", Stream: "uptime_seconds", StreamType: "number", Kind: "value", X: 6, Y: 0, W: 3, H: 3},
			{ID: "cpu-cores", Stream: "cpu_core_count", StreamType: "number", Kind: "value", X: 9, Y: 0, W: 3, H: 3},
			{ID: "cpu-usage", Stream: "cpu_usage_percent", StreamType: "number", Kind: "gauge", X: 0, Y: 3, W: 3, H: 3},
			{ID: "memory-usage", Stream: "memory_usage_percent", StreamType: "number", Kind: "gauge", X: 3, Y: 3, W: 3, H: 3},
			{ID: "swap-usage", Stream: "swap_usage_percent", StreamType: "number", Kind: "gauge", X: 6, Y: 3, W: 3, H: 3},
			{ID: "root-usage", Stream: "root_usage_percent", StreamType: "number", Kind: "gauge", X: 9, Y: 3, W: 3, H: 3},
			{ID: "cpu-trend", Stream: "cpu_usage_percent", StreamType: "number", Kind: "line", X: 0, Y: 6, W: 6, H: 4},
			{ID: "memory-trend", Stream: "memory_usage_percent", StreamType: "number", Kind: "area", X: 6, Y: 6, W: 6, H: 4},
			{ID: "load-1", Stream: "load_average_1m", StreamType: "number", Kind: "line", X: 0, Y: 10, W: 4, H: 3},
			{ID: "load-5", Stream: "load_average_5m", StreamType: "number", Kind: "line", X: 4, Y: 10, W: 4, H: 3},
			{ID: "load-15", Stream: "load_average_15m", StreamType: "number", Kind: "line", X: 8, Y: 10, W: 4, H: 3},
			{ID: "cpu-user", Stream: "cpu_user_percent", StreamType: "number", Kind: "line", X: 0, Y: 13, W: 6, H: 3},
			{ID: "cpu-system", Stream: "cpu_system_percent", StreamType: "number", Kind: "line", X: 6, Y: 13, W: 6, H: 3},
			{ID: "cpu-iowait", Stream: "cpu_iowait_percent", StreamType: "number", Kind: "line", X: 0, Y: 16, W: 6, H: 3},
			{ID: "swap-trend", Stream: "swap_usage_percent", StreamType: "number", Kind: "line", X: 6, Y: 16, W: 6, H: 3},
			{ID: "memory-available", Stream: "memory_available_bytes", StreamType: "number", Kind: "value", X: 0, Y: 19, W: 4, H: 2},
			{ID: "memory-cache", Stream: "memory_cached_bytes", StreamType: "number", Kind: "value", X: 4, Y: 19, W: 4, H: 2},
			{ID: "root-available", Stream: "root_available_bytes", StreamType: "number", Kind: "value", X: 8, Y: 19, W: 4, H: 2},
			{ID: "root-inodes", Stream: "root_inode_usage_percent", StreamType: "number", Kind: "line", X: 0, Y: 21, W: 4, H: 3},
			{ID: "swap-in", Stream: "swap_in_bytes_per_second", StreamType: "number", Kind: "line", X: 4, Y: 21, W: 4, H: 3},
			{ID: "swap-out", Stream: "swap_out_bytes_per_second", StreamType: "number", Kind: "line", X: 8, Y: 21, W: 4, H: 3},
			{ID: "disk-read", Stream: "disk_read_bytes_per_second", StreamType: "number", Kind: "line", X: 0, Y: 24, W: 6, H: 4},
			{ID: "disk-write", Stream: "disk_write_bytes_per_second", StreamType: "number", Kind: "line", X: 6, Y: 24, W: 6, H: 4},
			{ID: "disk-read-ops", Stream: "disk_read_operations_per_second", StreamType: "number", Kind: "line", X: 0, Y: 28, W: 4, H: 3},
			{ID: "disk-write-ops", Stream: "disk_write_operations_per_second", StreamType: "number", Kind: "line", X: 4, Y: 28, W: 4, H: 3},
			{ID: "disk-latency", Stream: "disk_io_latency_ms", StreamType: "number", Kind: "line", X: 8, Y: 28, W: 4, H: 3},
			{ID: "network-receive", Stream: "network_receive_bytes_per_second", StreamType: "number", Kind: "line", X: 0, Y: 31, W: 6, H: 4},
			{ID: "network-transmit", Stream: "network_transmit_bytes_per_second", StreamType: "number", Kind: "line", X: 6, Y: 31, W: 6, H: 4},
			{ID: "network-errors", Stream: "network_errors_per_second", StreamType: "number", Kind: "line", X: 0, Y: 35, W: 6, H: 3},
			{ID: "network-drops", Stream: "network_dropped_packets_per_second", StreamType: "number", Kind: "line", X: 6, Y: 35, W: 6, H: 3},
			{ID: "process-count", Stream: "process_count", StreamType: "number", Kind: "value", X: 0, Y: 38, W: 3, H: 2},
			{ID: "process-running", Stream: "process_running_count", StreamType: "number", Kind: "value", X: 3, Y: 38, W: 3, H: 2},
			{ID: "process-blocked", Stream: "process_blocked_count", StreamType: "number", Kind: "value", X: 6, Y: 38, W: 3, H: 2},
			{ID: "process-zombies", Stream: "process_zombie_count", StreamType: "number", Kind: "value", X: 9, Y: 38, W: 3, H: 2},
			{ID: "cpu-temperature", Stream: "cpu_temperature_celsius", StreamType: "number", Kind: "value", X: 0, Y: 40, W: 4, H: 2},
			{ID: "memory-total", Stream: "memory_total_bytes", StreamType: "number", Kind: "value", X: 4, Y: 40, W: 4, H: 2},
			{ID: "failed-service-count", Stream: "failed_service_count", StreamType: "number", Kind: "value", X: 8, Y: 40, W: 4, H: 2},
			{ID: "filesystems", Stream: "filesystems", StreamType: "event", Kind: "events", X: 0, Y: 42, W: 6, H: 5},
			{ID: "network-interfaces", Stream: "network_interfaces", StreamType: "event", Kind: "events", X: 6, Y: 42, W: 6, H: 5},
			{ID: "top-processes", Stream: "top_processes", StreamType: "event", Kind: "events", X: 0, Y: 47, W: 6, H: 5},
			{ID: "services", Stream: "services", StreamType: "event", Kind: "events", X: 6, Y: 47, W: 6, H: 5},
			{ID: "hardware", Stream: "hardware", StreamType: "event", Kind: "events", X: 0, Y: 52, W: 12, H: 4},
			{ID: "system-logs", Stream: "system_logs", StreamType: "log", Kind: "logs", X: 0, Y: 56, W: 6, H: 5},
			{ID: "machine-events", Stream: "machine_events", StreamType: "event", Kind: "events", X: 6, Y: 56, W: 6, H: 5},
		}
	default:
		return Layout{}, Invalid("unknown App template")
	}
	return layout, ValidateLayout(layout, nil)
}
