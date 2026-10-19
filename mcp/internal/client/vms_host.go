package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

type VMCoreSocket struct {
	SocketID    int32   `json:"socket_id"`
	CoreIndices []int32 `json:"core_indices"`
}
type VMCoreIsolationSettings struct {
	Sockets []VMCoreSocket `json:"sockets"`
}
type VMCoreIsolationInput struct {
	MachineName string `json:"machine_name"`
	VMCoreIsolationSettings
}
type VMHostHugePagesSettings struct {
	PageSize  string `json:"page_size"`
	PageCount int32  `json:"page_count"`
}
type VMHostHugePagesInput struct {
	MachineName string `json:"machine_name"`
	VMHostHugePagesSettings
}

func (c *Client) GetVMHostBootSnapshot(ctx context.Context, in VMHostInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.MachineName); err != nil {
		return nil, err
	}
	raw, err := c.vmJSON(ctx, "GET", "/virsh/hostsnapshot/"+url.PathEscape(in.MachineName), nil)
	if err != nil {
		return nil, err
	}
	if err = ValidateVMHostBootSnapshot(raw, in.MachineName); err != nil {
		return nil, err
	}
	return raw, nil
}

// ValidateVMHostBootSnapshot verifies both the original grubby output and its
// parsed entries, keeping the original output and cmdline bytes unchanged.
func ValidateVMHostBootSnapshot(raw json.RawMessage, machine string) error {
	bad := errors.New("invalid complete host boot snapshot; no change made")
	var snapshot struct {
		MachineName string `json:"machine_name"`
		Raw         string `json:"grubby_info"`
		Cmdline     string `json:"current_cmdline"`
		Default     string `json:"default_kernel"`
		Entries     []struct {
			Index  int32  `json:"index"`
			Kernel string `json:"kernel"`
			Args   string `json:"args"`
		} `json:"entries"`
	}
	if json.Unmarshal(raw, &snapshot) != nil || snapshot.MachineName != machine || snapshot.Raw == "" || !strings.HasSuffix(snapshot.Raw, "\n") || strings.TrimSpace(snapshot.Cmdline) == "" || snapshot.Default == "" || len(snapshot.Entries) == 0 {
		return bad
	}
	type entry struct {
		Index        int32
		Kernel, Args string
	}
	var parsed []entry
	fields := map[string]bool{}
	seen := map[int32]bool{}
	finish := func() bool {
		return len(parsed) > 0 && fields["index"] && fields["kernel"] && fields["args"] && parsed[len(parsed)-1].Kernel != ""
	}
	for _, line := range strings.Split(strings.TrimSuffix(snapshot.Raw, "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return bad
		}
		if key == "index" {
			if len(parsed) > 0 && !finish() {
				return bad
			}
			n, err := strconv.ParseInt(value, 10, 32)
			if err != nil || n < 0 || seen[int32(n)] {
				return bad
			}
			seen[int32(n)] = true
			parsed = append(parsed, entry{Index: int32(n)})
			fields = map[string]bool{}
		}
		if len(parsed) == 0 || fields[key] {
			return bad
		}
		fields[key] = true
		if key == "kernel" || key == "args" {
			if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
				return bad
			}
			value = value[1 : len(value)-1]
			if strings.ContainsAny(value, "\x00\r\n") {
				return bad
			}
			if key == "kernel" {
				parsed[len(parsed)-1].Kernel = value
			} else {
				parsed[len(parsed)-1].Args = value
			}
		}
	}
	if !finish() || len(parsed) != len(snapshot.Entries) {
		return bad
	}
	defaultFound := false
	for i, entry := range parsed {
		other := snapshot.Entries[i]
		if entry.Index != other.Index || entry.Kernel != other.Kernel || entry.Args != other.Args {
			return bad
		}
		if entry.Kernel == snapshot.Default {
			defaultFound = true
		}
	}
	if !defaultFound {
		return bad
	}
	return nil
}
func (c *Client) SetVMHostCoreIsolationPost(ctx context.Context, in VMCoreIsolationInput) (json.RawMessage, error) {
	return c.setVMHostCoreIsolation(ctx, "POST", in)
}
func (c *Client) SetVMHostCoreIsolationPut(ctx context.Context, in VMCoreIsolationInput) (json.RawMessage, error) {
	return c.setVMHostCoreIsolation(ctx, "PUT", in)
}
func (c *Client) setVMHostCoreIsolation(ctx context.Context, method string, in VMCoreIsolationInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	raw, err := c.vmJSON(ctx, method, "/virsh/coreisolation/"+url.PathEscape(in.MachineName), in.VMCoreIsolationSettings)
	if err != nil {
		return nil, err
	}
	if err = ValidateVMResponse("GetVMCoreIsolation", raw); err != nil {
		return nil, err
	}
	return raw, nil
}
func (c *Client) SetVMHostHugePagesPost(ctx context.Context, in VMHostHugePagesInput) (json.RawMessage, error) {
	return c.setVMHostHugePages(ctx, "POST", in)
}
func (c *Client) SetVMHostHugePagesPut(ctx context.Context, in VMHostHugePagesInput) (json.RawMessage, error) {
	return c.setVMHostHugePages(ctx, "PUT", in)
}
func (c *Client) setVMHostHugePages(ctx context.Context, method string, in VMHostHugePagesInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	raw, err := c.vmJSON(ctx, method, "/virsh/hosthugepages/"+url.PathEscape(in.MachineName), in.VMHostHugePagesSettings)
	if err != nil {
		return nil, err
	}
	if err = ValidateVMResponse("GetVMHostHugePages", raw); err != nil {
		return nil, err
	}
	return raw, nil
}
