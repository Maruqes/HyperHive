package dnsmasq

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
)

const (
	AliasConfPath  = "/etc/hyperhive/dnsmasq-aliases.conf"
	redeConfPath   = "/etc/dnsmasq.d/512rede-host.conf"
	serviceName    = "dnsmasq"
	managedComment = "# Managed by HyperHive"
	includeLine    = "conf-file=" + AliasConfPath
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
	if strings.ContainsAny(alias, " \t\r\n,") {
		return fmt.Errorf("alias contains invalid characters")
	}
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("invalid ip: %s", ip)
	}

	return nil
}

func readAliasEntries() ([]AliasEntry, error) {
	data, err := os.ReadFile(AliasConfPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read alias config: %w", err)
	}

	var entries []AliasEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "address=/") {
			continue
		}
		// address=/domain/ip
		payload := strings.TrimPrefix(line, "address=/")
		slash := strings.LastIndex(payload, "/")
		if slash < 1 {
			continue
		}
		alias := payload[:slash]
		ip := payload[slash+1:]
		if alias != "" && ip != "" {
			entries = append(entries, AliasEntry{Alias: alias, IP: ip})
		}
	}

	return entries, nil
}

func writeAliasEntries(entries []AliasEntry) error {
	if err := os.MkdirAll("/etc/hyperhive", 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(managedComment + "\n")
	for _, e := range entries {
		// address=/domain/ip — matches domain and all *.domain subdomains
		fmt.Fprintf(&sb, "address=/%s/%s\n", e.Alias, e.IP)
	}

	if err := os.WriteFile(AliasConfPath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("write alias config: %w", err)
	}

	if err := ensureIncludeInRedeConf(); err != nil {
		return err
	}

	return nil
}

// ensureIncludeInRedeConf adds a conf-file= line to the 512rede dnsmasq config
// so that the standalone 512rede dnsmasq instance picks up our aliases.
func ensureIncludeInRedeConf() error {
	data, err := os.ReadFile(redeConfPath)
	if err != nil {
		return nil // 512rede config doesn't exist, nothing to include into
	}

	if strings.Contains(string(data), includeLine) {
		return nil // already included
	}

	// Append the include directive
	f, err := os.OpenFile(redeConfPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open 512rede config for append: %w", err)
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "\n# HyperHive aliases\n%s\n", includeLine); err != nil {
		return fmt.Errorf("write include to 512rede config: %w", err)
	}

	return nil
}

func reloadDnsmasq() error {
	// address= directives in included config files are only read at startup,
	// SIGHUP only re-reads /etc/hosts and leases. We need to kill and let
	// the process managers restart the instances so they re-read configs.

	// Kill the 512rede instance — it runs with -k (foreground), so its
	// process manager will automatically restart it.
	pidOut, _ := exec.Command("pgrep", "-f", "conf-file="+redeConfPath).CombinedOutput()
	if pid := strings.TrimSpace(string(pidOut)); pid != "" {
		_ = exec.Command("kill", pid).Run()
	}

	// Kill and restart the wireguard instance (we manage it ourselves).
	wgConfPath := "/etc/hyperhive/dnsmasq-wireguard.conf"
	wgPidOut, _ := exec.Command("pgrep", "-f", "conf-file="+wgConfPath).CombinedOutput()
	if pid := strings.TrimSpace(string(wgPidOut)); pid != "" {
		_ = exec.Command("kill", pid).Run()
		startOut, startErr := exec.Command("dnsmasq", "--conf-file="+wgConfPath).CombinedOutput()
		if startErr != nil {
			return fmt.Errorf("restart wireguard dnsmasq: %w: %s", startErr, strings.TrimSpace(string(startOut)))
		}
	}

	return nil
}
