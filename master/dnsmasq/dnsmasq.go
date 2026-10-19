package dnsmasq

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const (
	AliasConfPath  = "/etc/hyperhive/dnsmasq-aliases.conf"
	redeConfPath   = "/etc/dnsmasq.d/512rede-host.conf"
	redeService    = "dnsmasq-512rede-host.service"
	serviceName    = "dnsmasq"
	managedComment = "# Managed by HyperHive"
	includeLine    = "conf-file=" + AliasConfPath
	metadataPrefix = "# HyperHive alias metadata: "
)

var (
	aliasMu            sync.Mutex
	aliasConfPath      = AliasConfPath
	ensureAliasInclude = ensureIncludeInRedeConf
	reloadAliases      = reloadDnsmasq
)

type AliasEntry struct {
	Alias       string `json:"alias"`
	IP          string `json:"ip"`
	SSHUsername string `json:"ssh_username"`
	RelatedVM   string `json:"related_vm"`
}

func GetAllAliases() ([]AliasEntry, error) {
	aliasMu.Lock()
	defer aliasMu.Unlock()
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
	aliasMu.Lock()
	defer aliasMu.Unlock()
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

func AddAlias(alias, ip, sshUsername, relatedVM string) error {
	aliasMu.Lock()
	defer aliasMu.Unlock()
	if err := validateAliasIP(alias, ip); err != nil {
		return err
	}

	entries, err := readAliasEntries()
	if err != nil {
		return err
	}

	entry := AliasEntry{Alias: alias, IP: ip, SSHUsername: sshUsername, RelatedVM: relatedVM}
	updated := false
	for i := range entries {
		if entries[i].Alias == alias {
			if entries[i] == entry {
				return nil
			}
			entries[i] = entry
			updated = true
			break
		}
	}

	if !updated {
		entries = append(entries, entry)
	}

	if err := writeAliasEntries(entries); err != nil {
		return err
	}

	return reloadAliases()
}

func RemoveAlias(alias, ip string) error {
	aliasMu.Lock()
	defer aliasMu.Unlock()
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

	return reloadAliases()
}

// RemoveAliasesForVM deletes only aliases explicitly linked to this VM.
func RemoveAliasesForVM(vmName string) error {
	if vmName == "" {
		return nil
	}
	aliasMu.Lock()
	defer aliasMu.Unlock()
	entries, err := readAliasEntries()
	if err != nil {
		return err
	}
	filtered := make([]AliasEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.RelatedVM != vmName {
			filtered = append(filtered, entry)
		}
	}
	if len(filtered) == len(entries) {
		return nil
	}
	if err := writeAliasEntries(filtered); err != nil {
		return err
	}
	return reloadAliases()
}

func validateAliasIP(alias, ip string) error {
	if strings.TrimSpace(alias) == "" {
		return fmt.Errorf("alias is required")
	}
	if strings.TrimSpace(ip) == "" {
		return fmt.Errorf("ip is required")
	}
	if strings.ContainsAny(alias, " \t\r\n,#") || strings.TrimSpace(alias) != alias {
		return fmt.Errorf("alias contains invalid characters")
	}
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("invalid ip: %s", ip)
	}

	return nil
}

func readAliasEntries() ([]AliasEntry, error) {
	data, err := os.ReadFile(aliasConfPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read alias config: %w", err)
	}

	var entries []AliasEntry
	var metadata AliasEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, metadataPrefix) {
			metadata = AliasEntry{}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, metadataPrefix)), &metadata); err != nil {
				return nil, fmt.Errorf("invalid alias metadata: %w", err)
			}
			continue
		}
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
			entry := AliasEntry{Alias: alias, IP: ip}
			if metadata.Alias == alias && metadata.IP == ip {
				entry = metadata
			}
			entries = append(entries, entry)
		}
		metadata = AliasEntry{}
	}

	return entries, nil
}

func writeAliasEntries(entries []AliasEntry) error {
	if err := os.MkdirAll(filepath.Dir(aliasConfPath), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(managedComment + "\n")
	for _, e := range entries {
		if e.SSHUsername != "" || e.RelatedVM != "" {
			metadata, err := json.Marshal(e)
			if err != nil {
				return fmt.Errorf("encode alias metadata: %w", err)
			}
			fmt.Fprintf(&sb, "%s%s\n", metadataPrefix, metadata)
		}
		// address=/domain/ip — matches domain and all *.domain subdomains
		fmt.Fprintf(&sb, "address=/%s/%s\n", e.Alias, e.IP)
	}

	if err := os.WriteFile(aliasConfPath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("write alias config: %w", err)
	}

	if err := ensureAliasInclude(); err != nil {
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
	// SIGHUP only re-reads /etc/hosts and leases. We need to restart
	// the dnsmasq processes so they re-read the alias config.

	// Keep the 512rede instance owned by systemd. Starting dnsmasq directly
	// here races the service restart and can leave an orphan holding port 53.
	if _, err := os.Stat(redeConfPath); err == nil {
		out, restartErr := exec.Command("systemctl", "restart", redeService).CombinedOutput()
		if restartErr != nil {
			return fmt.Errorf("restart %s: %w: %s", redeService, restartErr, strings.TrimSpace(string(out)))
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat 512rede dnsmasq config: %w", err)
	}

	// Restart the wireguard instance (we manage it directly).
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
