package api

import (
	"fmt"
	"strconv"
	"strings"
)

// configuredAPIPort is shared by the listener and MCP's loopback API client.
func configuredAPIPort(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "9595", nil
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("API_PORT must be an integer between 1 and 65535")
	}
	return strconv.Itoa(port), nil
}
