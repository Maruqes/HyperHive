package npm

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

const MaxUpstreamMembers = 128

type UpstreamMember struct {
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Settings string `json:"settings_string"`
}

type UpstreamOptions struct {
	HashKey string `json:"hash_key,omitempty"`
}

type Upstream struct {
	ID          int64               `json:"id"`
	Name        string              `json:"name"`
	Context     string              `json:"context"`
	Method      string              `json:"loadbalancer_type"`
	Options     UpstreamOptions     `json:"loadbalancer_options,omitempty"`
	Members     []UpstreamMember    `json:"server_arr"`
	Revision    int64               `json:"revision"`
	ApplyStatus string              `json:"apply_status"`
	Generation  int64               `json:"applied_generation"`
	LastError   string              `json:"last_error,omitempty"`
	RecoveryID  int64               `json:"recovery_id,omitempty"`
	References  []UpstreamReference `json:"references"`
	Warnings    []string            `json:"warnings,omitempty"`
}

type UpstreamReference struct {
	Consumer string `json:"consumer_type"`
	ID       int    `json:"npm_id"`
	Path     string `json:"location_path,omitempty"`
}

var upstreamName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
var upstreamDuration = regexp.MustCompile(`^[1-9][0-9]{0,5}(ms|s|m|h|d)$`)

var upstreamMethods = map[string]string{
	"round_robin": "", "least_conn": "least_conn;", "random": "random;",
	"random_two": "random two least_conn;", "ip_hash": "ip_hash;",
	"hash": "hash", "hash_consistent": "hash",
}

// NormalizeUpstream accepts a deliberately smaller grammar than raw NGINX configuration.
func NormalizeUpstream(p *Upstream) error {
	p.Name = strings.ToLower(strings.TrimSpace(p.Name))
	if !upstreamName.MatchString(p.Name) || strings.HasPrefix(p.Name, "hh_") || p.Name == "localhost" || p.Name == "default" {
		return fmt.Errorf("name must be a unique ASCII identifier; hh_, localhost and default are reserved")
	}
	if p.Context == "" {
		p.Context = "http"
	}
	if p.Context != "http" && p.Context != "stream" {
		return fmt.Errorf("context must be http or stream")
	}
	if _, ok := upstreamMethods[p.Method]; !ok || p.Context == "stream" && p.Method == "ip_hash" {
		return fmt.Errorf("unsupported loadbalancer_type for context")
	}
	if strings.HasPrefix(p.Method, "hash") {
		allowed := map[string]bool{"$remote_addr": true}
		if p.Context == "http" {
			for _, k := range []string{"$request_uri", "$uri", "$host", "$remote_addr$uri"} {
				allowed[k] = true
			}
		}
		if !allowed[p.Options.HashKey] {
			return fmt.Errorf("hash_key is not an allowed expression for context")
		}
	} else if p.Options.HashKey != "" {
		return fmt.Errorf("hash_key requires hash or hash_consistent")
	}
	if len(p.Members) == 0 || len(p.Members) > MaxUpstreamMembers {
		return fmt.Errorf("server_arr must contain 1 to %d members", MaxUpstreamMembers)
	}
	seen := map[string]bool{}
	primary := false
	for i := range p.Members {
		m := &p.Members[i]
		address, err := netip.ParseAddr(m.IP)
		if err != nil || address.Zone() != "" || address.IsUnspecified() || address.IsMulticast() {
			return fmt.Errorf("server_arr[%d].ip must be a unicast IP literal without a zone", i)
		}
		m.IP = address.Unmap().String()
		if m.Port < 1 || m.Port > 65535 {
			return fmt.Errorf("server_arr[%d].port must be 1 to 65535", i)
		}
		endpoint := netip.AddrPortFrom(address.Unmap(), uint16(m.Port)).String()
		if seen[endpoint] {
			return fmt.Errorf("duplicate endpoint at server_arr[%d]", i)
		}
		seen[endpoint] = true
		settings, usable, err := normalizeServerSettings(m.Settings, p.Method)
		if err != nil {
			return fmt.Errorf("server_arr[%d].settings_string: %w", i, err)
		}
		m.Settings = settings
		primary = primary || usable
	}
	if !primary {
		return fmt.Errorf("at least one usable primary member is required")
	}
	p.Warnings = nil
	if len(p.Members) == 1 {
		p.Warnings = []string{"A single member cannot provide failover; NGINX ignores max_fails and fail_timeout for a single-server group."}
	}
	return nil
}

func normalizeServerSettings(raw, method string) (string, bool, error) {
	if len(raw) > 512 || strings.ContainsAny(raw, ";{}#\r\n$\t") {
		return "", false, fmt.Errorf("invalid settings characters or length")
	}
	values := map[string]string{}
	for _, token := range strings.Fields(raw) {
		parts := strings.Split(token, "=")
		key := parts[0]
		if _, ok := values[key]; ok {
			return "", false, fmt.Errorf("duplicate %s", key)
		}
		switch key {
		case "backup", "down":
			if len(parts) != 1 {
				return "", false, fmt.Errorf("%s takes no value", key)
			}
			if key == "backup" && (strings.HasPrefix(method, "hash") || strings.HasPrefix(method, "random") || method == "ip_hash") {
				return "", false, fmt.Errorf("backup is incompatible with this method")
			}
			values[key] = ""
		case "weight", "max_conns", "max_fails":
			if len(parts) != 2 {
				return "", false, fmt.Errorf("%s requires an integer", key)
			}
			n, err := strconv.Atoi(parts[1])
			min := 0
			if key == "weight" {
				min = 1
			}
			if err != nil || n < min || n > 1000000 {
				return "", false, fmt.Errorf("%s is outside its supported range", key)
			}
			values[key] = strconv.Itoa(n)
		case "fail_timeout":
			if len(parts) != 2 || !upstreamDuration.MatchString(parts[1]) {
				return "", false, fmt.Errorf("fail_timeout requires a positive duration with ms/s/m/h/d unit")
			}
			values[key] = parts[1]
		default:
			return "", false, fmt.Errorf("unsupported server parameter")
		}
	}
	var tokens []string
	for _, key := range []string{"weight", "max_conns", "max_fails", "fail_timeout", "backup", "down"} {
		if val, ok := values[key]; ok {
			if val != "" {
				tokens = append(tokens, key+"="+val)
			} else {
				tokens = append(tokens, key)
			}
		}
	}
	_, backup := values["backup"]
	_, down := values["down"]
	return strings.Join(tokens, " "), !backup && !down, nil
}

func upstreamInternalName(id int64) string { return fmt.Sprintf("hh_upstream_%d", id) }

func RenderUpstreams(pools []Upstream, context string) string {
	var b strings.Builder
	b.WriteString("# Managed by HyperHive. Derived from persisted upstream definitions.\n")
	for _, p := range pools {
		if p.Context != context {
			continue
		}
		fmt.Fprintf(&b, "upstream %s {\n  zone hh_%s_%d 64k;\n", upstreamInternalName(p.ID), context, p.ID)
		directive := upstreamMethods[p.Method]
		if strings.HasPrefix(p.Method, "hash") {
			directive = "hash " + p.Options.HashKey
			if p.Method == "hash_consistent" {
				directive += " consistent"
			}
			directive += ";"
		}
		if directive != "" {
			fmt.Fprintf(&b, "  %s\n", directive)
		}
		for _, m := range p.Members {
			addr, _ := netip.ParseAddr(m.IP)
			fmt.Fprintf(&b, "  server %s", netip.AddrPortFrom(addr, uint16(m.Port)))
			if m.Settings != "" {
				fmt.Fprintf(&b, " %s", m.Settings)
			}
			b.WriteString(";\n")
		}
		b.WriteString("}\n")
	}
	return b.String()
}
