package dnsmasq

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
)

const (
	hostsFilePath = "/etc/hosts"
	serviceName   = "dnsmasq"
	markerBegin   = "# BEGIN HyperHive Aliases"
	markerEnd     = "# END HyperHive Aliases"
)

type AliasEntry struct {
	Alias string
	IP    string
}

func GetAllAliases() ([]AliasEntry, error) {
	entries, err := readAliasEntries()
	if err != nil {
		return nil, err
	}

	// Return a copy so callers cannot mutate internal slices by accident.
	result := make([]AliasEntry, len(entries))
	copy(result, entries)
	return result, nil
}

func Install() error {
	if _, err := exec.LookPath(serviceName); err == nil {
		return nil
	}

	output, err := exec.Command("dnf", "install", "-y", serviceName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to install dnsmasq via dnf: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}

func GetAlias(alias, ip string) (bool, error) {
	if err := validateAliasIP(alias, ip); err != nil {
		return false, err
	}

	entries, err := readAliasEntries()
	if err != nil {
		return false, err
	}

	for _, entry := range entries {
		if entry.Alias == alias && entry.IP == ip {
			return true, nil
		}
	}

	return false, nil
}

func AddAlias(alias, ip string) error {
	if err := validateAliasIP(alias, ip); err != nil {
		return err
	}

	entries, err := readAliasEntries()
	if err != nil {
		return err
	}

	updated := false
	for i := range entries {
		if entries[i].Alias == alias {
			if entries[i].IP == ip {
				return nil
			}
			entries[i].IP = ip
			updated = true
			break
		}
	}

	if !updated {
		entries = append(entries, AliasEntry{
			Alias: alias,
			IP:    ip,
		})
	}

	if err := writeAliasEntries(entries); err != nil {
		return err
	}

	return reloadDnsmasq()
}

func RemoveAlias(alias, ip string) error {
	if err := validateAliasIP(alias, ip); err != nil {
		return err
	}

	entries, err := readAliasEntries()
	if err != nil {
		return err
	}

	filtered := entries[:0]
	removed := false
	for _, entry := range entries {
		if entry.Alias == alias && entry.IP == ip {
			removed = true
			continue
		}
		filtered = append(filtered, entry)
	}

	if !removed {
		return fmt.Errorf("alias not found: %s -> %s", alias, ip)
	}

	if err := writeAliasEntries(filtered); err != nil {
		return err
	}

	return reloadDnsmasq()
}

func validateAliasIP(alias, ip string) error {
	alias = strings.TrimSpace(alias)
	ip = strings.TrimSpace(ip)

	if alias == "" {
		return fmt.Errorf("alias is required")
	}
	if ip == "" {
		return fmt.Errorf("ip is required")
	}
	if strings.ContainsAny(alias, " \t\r\n,/") {
		return fmt.Errorf("alias contains invalid characters")
	}
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("invalid ip: %s", ip)
	}

	return nil
}

func readAliasEntries() ([]AliasEntry, error) {
	data, err := os.ReadFile(hostsFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read hosts file: %w", err)
	}

	var entries []AliasEntry
	inBlock := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == markerBegin {
			inBlock = true
			continue
		}
		if trimmed == markerEnd {
			break
		}
		if !inBlock {
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		entries = append(entries, AliasEntry{
			IP:    fields[0],
			Alias: fields[1],
		})
	}

	return entries, nil
}

func writeAliasEntries(entries []AliasEntry) error {
	data, err := os.ReadFile(hostsFilePath)
	if err != nil {
		return fmt.Errorf("failed to read hosts file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var result []string
	skip := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == markerBegin {
			skip = true
			continue
		}
		if trimmed == markerEnd {
			skip = false
			continue
		}
		if !skip {
			result = append(result, line)
		}
	}

	// Remove trailing empty lines
	for len(result) > 0 && strings.TrimSpace(result[len(result)-1]) == "" {
		result = result[:len(result)-1]
	}

	// Append our managed block
	if len(entries) > 0 {
		result = append(result, "", markerBegin)
		for _, e := range entries {
			result = append(result, fmt.Sprintf("%s\t%s", e.IP, e.Alias))
		}
		result = append(result, markerEnd)
	}
	result = append(result, "") // trailing newline

	if err := os.WriteFile(hostsFilePath, []byte(strings.Join(result, "\n")), 0644); err != nil {
		return fmt.Errorf("failed to write hosts file: %w", err)
	}

	return nil
}

func reloadDnsmasq() error {
	// Send SIGHUP to all running dnsmasq processes to re-read /etc/hosts.
	// We don't use systemctl because dnsmasq may run as standalone instances
	// (e.g. 512rede, wireguard) rather than via the system service.
	out, err := exec.Command("killall", "-HUP", serviceName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to send SIGHUP to dnsmasq: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
