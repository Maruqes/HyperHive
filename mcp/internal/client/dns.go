package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

// DNSObject preserves the complete DNS API response, including extension fields.
type DNSObject map[string]any

func (c *Client) ListDNSAliases(ctx context.Context) (DNSObject, error) {
	var raw json.RawMessage
	if err := c.getJSON(ctx, "/dnsmasq/alias/all", &raw); err != nil {
		return nil, err
	}
	var out DNSObject
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&out) != nil || out == nil {
		return nil, errors.New("HyperHive API returned an invalid DNS aliases object")
	}
	aliases, ok := out["aliases"].([]any)
	if !ok {
		return nil, errors.New("HyperHive API returned invalid DNS aliases")
	}
	for _, entry := range aliases {
		alias, ok := entry.(map[string]any)
		if !ok {
			return nil, errors.New("HyperHive API returned an invalid DNS alias")
		}
		if _, ok := alias["alias"].(string); !ok {
			return nil, errors.New("HyperHive API returned an invalid DNS alias name")
		}
		if _, ok := alias["ip"].(string); !ok {
			return nil, errors.New("HyperHive API returned an invalid DNS alias IP")
		}
		for _, field := range []string{"ssh_username", "related_vm"} {
			if value, exists := alias[field]; exists {
				if _, ok := value.(string); !ok {
					return nil, errors.New("HyperHive API returned invalid DNS alias metadata")
				}
			}
		}
	}
	return out, nil
}

// DNSAliasInput identifies a DNS alias/address pair. Both values are required.
type DNSAliasInput struct {
	Alias string `json:"alias" jsonschema:"DNS alias; nonblank and no internal whitespace or comma"`
	IP    string `json:"ip" jsonschema:"IPv4 or IPv6 address"`
}

// DNSAliasWriteInput adds independent optional metadata to an alias/address pair.
// Pointers retain the difference between omitted fields and explicit empty strings.
type DNSAliasWriteInput struct {
	Alias       string  `json:"alias" jsonschema:"DNS alias; nonblank and no whitespace or comma"`
	IP          string  `json:"ip" jsonschema:"IPv4 or IPv6 address"`
	SSHUsername *string `json:"ssh_username,omitempty" jsonschema:"Optional manually entered SSH username; omission or an empty string clears the stored value"`
	RelatedVM   *string `json:"related_vm,omitempty" jsonschema:"Optional exact VM name from list_vms; the backend validates existence; omission or an empty string clears the link"`
}

func (in DNSAliasWriteInput) Validate() error {
	return ValidateDNSAliasWrite(DNSAliasInput{Alias: in.Alias, IP: in.IP})
}

func (in DNSAliasInput) Validate() error {
	alias, ip := strings.TrimSpace(in.Alias), strings.TrimSpace(in.IP)
	if alias == "" || strings.ContainsAny(alias, " \t\r\n,") {
		return errors.New("alias must be nonblank and contain no internal whitespace or comma")
	}
	if net.ParseIP(ip) == nil {
		return errors.New("ip must be a valid IPv4 or IPv6 address")
	}
	return nil
}

// ValidateDNSAliasWrite protects the unescaped dnsmasq configuration writer.
// Reads may trim surrounding whitespace, but writes preserve exact strings.
func ValidateDNSAliasWrite(in DNSAliasInput) error {
	for _, value := range []string{in.Alias, in.IP} {
		if strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return errors.New("DNS writes must contain no whitespace or control characters")
		}
	}
	return in.Validate()
}

func (c *Client) GetDNSAlias(ctx context.Context, in DNSAliasInput) (DNSObject, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	query := url.Values{"alias": {strings.TrimSpace(in.Alias)}, "ip": {strings.TrimSpace(in.IP)}}
	var raw json.RawMessage
	if err := c.getJSON(ctx, "/dnsmasq/alias/get?"+query.Encode(), &raw); err != nil {
		return nil, err
	}
	var out DNSObject
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&out) != nil || out == nil {
		return nil, errors.New("HyperHive API returned an invalid DNS alias result")
	}
	if _, ok := out["exists"].(bool); !ok {
		return nil, errors.New("HyperHive API returned an invalid DNS alias existence value")
	}
	return out, nil
}

// AddDNSAlias upserts an alias and restarts DNS. Failures can leave configuration changed.
func (c *Client) AddDNSAlias(ctx context.Context, in DNSAliasWriteInput) error {
	if err := in.Validate(); err != nil {
		return err
	}
	return c.request(ctx, http.MethodPost, "/dnsmasq/alias/add", in, nil)
}
