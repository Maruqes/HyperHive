package dnsmasq

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	aliasFilePath  = "/etc/dnsmasq.d/hyperhive-aliases.conf"
	serviceName    = "dnsmasq"
	managedComment = "# Managed by HyperHive"
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
	file, err := os.Open(aliasFilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []AliasEntry{}, nil
		}
		return nil, fmt.Errorf("failed to open alias file: %w", err)
	}
	defer file.Close()

	entries := make([]AliasEntry, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		entry, ok := parseAliasLine(scanner.Text())
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read alias file: %w", err)
	}

	return entries, nil
}

func parseAliasLine(line string) (AliasEntry, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return AliasEntry{}, false
	}
	if !strings.HasPrefix(line, "host-record=") {
		return AliasEntry{}, false
	}

	payload := strings.TrimPrefix(line, "host-record=")
	parts := strings.Split(payload, ",")
	if len(parts) < 2 {
		return AliasEntry{}, false
	}

	alias := strings.TrimSpace(parts[0])
	ip := strings.TrimSpace(parts[1])
	if alias == "" || ip == "" {
		return AliasEntry{}, false
	}

	return AliasEntry{
		Alias: alias,
		IP:    ip,
	}, true
}

func writeAliasEntries(entries []AliasEntry) error {
	if err := os.MkdirAll(filepath.Dir(aliasFilePath), 0755); err != nil {
		return fmt.Errorf("failed to create dnsmasq directory: %w", err)
	}

	file, err := os.OpenFile(aliasFilePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open alias file for writing: %w", err)
	}
	defer file.Close()

	if _, err := fmt.Fprintln(file, managedComment); err != nil {
		return fmt.Errorf("failed to write alias file header: %w", err)
	}
	for _, entry := range entries {
		if _, err := fmt.Fprintf(file, "host-record=%s,%s\n", entry.Alias, entry.IP); err != nil {
			return fmt.Errorf("failed to write alias entry: %w", err)
		}
	}

	return nil
}

func reloadDnsmasq() error {
	reloadOut, reloadErr := exec.Command("systemctl", "reload", serviceName).CombinedOutput()
	if reloadErr == nil {
		return nil
	}

	restartOut, restartErr := exec.Command("systemctl", "restart", serviceName).CombinedOutput()
	if restartErr != nil {
		return fmt.Errorf(
			"failed to reload/restart dnsmasq: reload error: %v (%s), restart error: %v (%s)",
			reloadErr,
			strings.TrimSpace(string(reloadOut)),
			restartErr,
			strings.TrimSpace(string(restartOut)),
		)
	}
	return nil
}
