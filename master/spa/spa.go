package spa

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// EnableSPA blocks the given port with iptables and only allows traffic from IPs present in the ipset.
func EnableSPA(port int) error {
	if err := validatePort(port); err != nil {
		return err
	}

	setName := ipsetName(port)
	if err := ensureIPSet(setName); err != nil {
		return fmt.Errorf("ensure ipset %s: %w", setName, err)
	}

	for _, rule := range rulesForPort(port, setName) {
		if err := deleteAllMatchingRules(rule); err != nil {
			return err
		}
		if err := ensureIptablesRule(rule); err != nil {
			return err
		}
	}
	return nil
}

// DisableSPA removes SPA rules and tears down the ipset for the given port.
func DisableSPA(port int) error {
	if err := validatePort(port); err != nil {
		return err
	}

	setName := ipsetName(port)
	for _, rule := range rulesForPort(port, setName) {
		if err := deleteAllMatchingRules(rule); err != nil {
			return err
		}
	}

	if err := destroyIPSet(setName); err != nil {
		return fmt.Errorf("destroy ipset %s: %w", setName, err)
	}
	return nil
}

// AllowIP adds an IP address to the ipset for the given port with a limited lifetime.
func AllowIP(port int, ip string, seconds int) error {
	if err := validatePort(port); err != nil {
		return err
	}
	if seconds <= 0 {
		return fmt.Errorf("timeout seconds must be positive")
	}
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() == nil {
		return fmt.Errorf("invalid IPv4 address %q", ip)
	}

	setName := ipsetName(port)
	exists, err := ipsetExists(setName)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("SPA not enabled for port %d (missing ipset %s)", port, setName)
	}

	secondsStr := strconv.Itoa(seconds)
	if err := runCommand("ipset", "add", "-exist", setName, parsed.String(), "timeout", secondsStr); err != nil {
		return fmt.Errorf("add IP to %s: %w", setName, err)
	}
	return nil
}

type AllowEntry struct {
	IP               string
	RemainingSeconds int
}

// ListAllows returns allowed IPs with their remaining time for the given port.
func ListAllows(port int) ([]AllowEntry, error) {
	if err := validatePort(port); err != nil {
		return nil, err
	}

	setName := ipsetName(port)
	exists, err := ipsetExists(setName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("SPA not enabled for port %d (missing ipset %s)", port, setName)
	}

	output, err := runCommandOutput("ipset", "list", "-t", setName)
	if err != nil {
		return nil, fmt.Errorf("list ipset %s: %w", setName, err)
	}
	return parseIPSetMembers(output)
}

func ipsetName(port int) string {
	return fmt.Sprintf("spa_allow_%d", port)
}

func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %d", port)
	}
	return nil
}

func ensureIPSet(name string) error {
	exists, err := ipsetExists(name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return runCommand("ipset", "create", "-exist", name, "hash:ip", "timeout", "0")
}

func destroyIPSet(name string) error {
	exists, err := ipsetExists(name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return runCommand("ipset", "destroy", name)
}

func ipsetExists(name string) (bool, error) {
	cmd := exec.Command("ipset", "list", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("ipset list %s: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return true, nil
}

type spaRule struct {
	chain    string
	args     []string
	position int // insertion position (1-based) when adding; ignored for deletes
}

func rulesForPort(port int, setName string) []spaRule {
	portStr := strconv.Itoa(port)
	var out []spaRule
	for _, chain := range []string{"INPUT", "FORWARD"} {
		for _, proto := range []string{"tcp", "udp"} {
			out = append(out,
				spaRule{chain: chain, args: []string{"-p", proto, "--dport", portStr, "-m", "set", "--match-set", setName, "src", "-j", "ACCEPT"}, position: 1},
				spaRule{chain: chain, args: []string{"-p", proto, "--dport", portStr, "-j", "DROP"}, position: 2},
			)
		}
	}
	return out
}

func ensureIptablesRule(rule spaRule) error {
	exists, err := iptablesRuleExists(rule)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return runIptables("-I", rule)
}

func deleteAllMatchingRules(rule spaRule) error {
	for {
		exists, err := iptablesRuleExists(rule)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		if err := runIptables("-D", rule); err != nil {
			return err
		}
	}
}

func iptablesRuleExists(rule spaRule) (bool, error) {
	args := append([]string{"-w", "-C", rule.chain}, rule.args...)
	cmd := exec.Command("iptables", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("iptables %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return true, nil
}

func runIptables(action string, rule spaRule) error {
	args := []string{"-w", action, rule.chain}
	if action == "-I" && rule.position > 0 {
		args = append(args, strconv.Itoa(rule.position))
	}
	args = append(args, rule.args...)
	return runCommand("iptables", args...)
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		out := strings.TrimSpace(string(output))
		if out != "" {
			return fmt.Errorf("%s %s failed: %s: %w", name, strings.Join(args, " "), out, err)
		}
		return fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func runCommandOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		out := strings.TrimSpace(string(output))
		if out != "" {
			return "", fmt.Errorf("%s %s failed: %s: %w", name, strings.Join(args, " "), out, err)
		}
		return "", fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return string(output), nil
}

func parseIPSetMembers(output string) ([]AllowEntry, error) {
	lines := strings.Split(output, "\n")
	var out []AllowEntry
	foundMembers := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "members:") {
			foundMembers = true
			continue
		}

		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		ip := fields[0]
		if parsed := net.ParseIP(ip); parsed == nil || parsed.To4() == nil {
			if foundMembers {
				return nil, fmt.Errorf("invalid ipset entry %q", trimmed)
			}
			continue
		}

		remaining := 0
		for i := 1; i+1 < len(fields); i++ {
			if fields[i] == "timeout" {
				val, err := strconv.Atoi(fields[i+1])
				if err != nil {
					return nil, fmt.Errorf("parse timeout for %s: %w", ip, err)
				}
				remaining = val
				break
			}
		}
		out = append(out, AllowEntry{IP: ip, RemainingSeconds: remaining})
	}

	if !foundMembers {
		// Some ipset builds/locales omit the Members section when empty or localized.
		return []AllowEntry{}, nil
	}

	return out, nil
}
