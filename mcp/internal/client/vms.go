package client

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

type VMEmptyInput struct{}
type VMNameInput struct {
	VMName string `json:"vm_name" jsonschema:"Existing VM name"`
}
type VMHostInput struct {
	MachineName string `json:"machine_name" jsonschema:"Existing HyperHive machine name"`
}
type VMIDInput struct {
	ID int64 `json:"id" jsonschema:"Positive database ID"`
}
type VMXMLInput struct {
	VMName      string `json:"vm_name"`
	MachineName string `json:"machine_name,omitempty" jsonschema:"Optional machine override; omitted resolves current VM host"`
}
type VMCPUXMLInput struct {
	VMName      string `json:"vm_name"`
	MachineName string `json:"machine_name"`
}
type VMCPUFeaturesInput struct {
	SlavesNames []string `json:"slavesnames,omitempty" jsonschema:"Optional host list; omitted includes all connected hosts"`
}

func ValidateVMName(value string) error {
	if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "/\\?#%\x00\r\n") || value == "." || value == ".." {
		return errors.New("VM and machine names must be nonblank single path segments")
	}
	return nil
}
func ValidateVMID(value int64) error {
	if value <= 0 {
		return errors.New("id must be positive")
	}
	return nil
}
func (c *Client) vmJSON(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.request(ctx, method, path, body, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}
func (c *Client) ListVMs(ctx context.Context, in VMEmptyInput) (json.RawMessage, error) {
	return c.vmReadJSON(ctx, "ListVMs", "GET", "/virsh/getallvms", nil)
}
func (c *Client) GetVM(ctx context.Context, in VMNameInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.VMName); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "GetVM", "GET", "/virsh/getvmbyname/"+url.PathEscape(in.VMName)+"", nil)
}
func (c *Client) GetVMXML(ctx context.Context, in VMXMLInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.VMName); err != nil {
		return nil, err
	}
	if in.MachineName != "" {
		if err := ValidateVMName(in.MachineName); err != nil {
			return nil, err
		}
	}
	path := "/virsh/vmxml/" + url.PathEscape(in.VMName) + ""
	if in.MachineName != "" {
		path += "?machine_name=" + url.QueryEscape(in.MachineName)
	}
	return c.vmReadJSON(ctx, "GetVMXML", "GET", path, nil)
}
func (c *Client) GetVMCPUXML(ctx context.Context, in VMCPUXMLInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.VMName); err != nil {
		return nil, err
	}
	if err := ValidateVMName(in.MachineName); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "GetVMCPUXML", "GET", "/virsh/cpuxml/"+url.PathEscape(in.VMName)+"", struct {
		MachineName string `json:"machine_name"`
	}{in.MachineName})
}
func (c *Client) ListVMAttachedDisks(ctx context.Context, in VMNameInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.VMName); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "ListVMAttachedDisks", "GET", "/virsh/vm_disk/"+url.PathEscape(in.VMName)+"", nil)
}
func (c *Client) GetVMVideo(ctx context.Context, in VMNameInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.VMName); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "GetVMVideo", "GET", "/virsh/novncvideo/"+url.PathEscape(in.VMName)+"", nil)
}
func (c *Client) GetVMBallooning(ctx context.Context, in VMNameInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.VMName); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "GetVMBallooning", "GET", "/virsh/ballooning/"+url.PathEscape(in.VMName)+"", nil)
}
func (c *Client) GetVMHugePages(ctx context.Context, in VMNameInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.VMName); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "GetVMHugePages", "GET", "/virsh/hugepages/"+url.PathEscape(in.VMName)+"", nil)
}
func (c *Client) GetVMKVM(ctx context.Context, in VMNameInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.VMName); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "GetVMKVM", "GET", "/virsh/kvm/"+url.PathEscape(in.VMName)+"", nil)
}
func (c *Client) GetVMHyperV(ctx context.Context, in VMNameInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.VMName); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "GetVMHyperV", "GET", "/virsh/hyperv/"+url.PathEscape(in.VMName)+"", nil)
}
func (c *Client) GetVMCPUPinning(ctx context.Context, in VMNameInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.VMName); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "GetVMCPUPinning", "GET", "/virsh/cpupinning/"+url.PathEscape(in.VMName)+"", nil)
}
func (c *Client) ListVMMachineTypes(ctx context.Context, in VMHostInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.MachineName); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "ListVMMachineTypes", "GET", "/virsh/machine_types/"+url.PathEscape(in.MachineName)+"", nil)
}
func (c *Client) GetVMCPUTopology(ctx context.Context, in VMHostInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.MachineName); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "GetVMCPUTopology", "GET", "/virsh/cputopology/"+url.PathEscape(in.MachineName)+"", nil)
}
func (c *Client) GetVMTunedProfiles(ctx context.Context, in VMHostInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.MachineName); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "GetVMTunedProfiles", "GET", "/virsh/tunedadm/"+url.PathEscape(in.MachineName)+"", nil)
}
func (c *Client) GetVMIrqBalance(ctx context.Context, in VMHostInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.MachineName); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "GetVMIrqBalance", "GET", "/virsh/irqbalance/"+url.PathEscape(in.MachineName)+"", nil)
}
func (c *Client) GetVMCoreIsolation(ctx context.Context, in VMHostInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.MachineName); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "GetVMCoreIsolation", "GET", "/virsh/coreisolation/"+url.PathEscape(in.MachineName)+"", nil)
}
func (c *Client) GetVMHostHugePages(ctx context.Context, in VMHostInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.MachineName); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "GetVMHostHugePages", "GET", "/virsh/hosthugepages/"+url.PathEscape(in.MachineName)+"", nil)
}
func (c *Client) ListVMBackups(ctx context.Context, in VMEmptyInput) (json.RawMessage, error) {
	return c.vmReadJSON(ctx, "ListVMBackups", "GET", "/virsh/backups", nil)
}
func (c *Client) ListVMAutomaticBackups(ctx context.Context, in VMEmptyInput) (json.RawMessage, error) {
	return c.vmReadJSON(ctx, "ListVMAutomaticBackups", "GET", "/virsh/autobak", nil)
}
func (c *Client) ListVMXMLTemplates(ctx context.Context, in VMEmptyInput) (json.RawMessage, error) {
	return c.vmReadJSON(ctx, "ListVMXMLTemplates", "GET", "/virsh/xmltemplates/", nil)
}
func (c *Client) GetVMXMLTemplate(ctx context.Context, in VMIDInput) (json.RawMessage, error) {
	if err := ValidateVMID(in.ID); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "GetVMXMLTemplate", "GET", "/virsh/xmltemplates/"+strconv.FormatInt(in.ID, 10)+"", nil)
}
func (c *Client) ListVMDisks(ctx context.Context, in VMEmptyInput) (json.RawMessage, error) {
	return c.vmReadJSON(ctx, "ListVMDisks", "GET", "/vm-disk/list", nil)
}
func (c *Client) GetVMCPUDisableFeatures(ctx context.Context, in VMCPUFeaturesInput) (json.RawMessage, error) {
	q := url.Values{}
	for _, name := range in.SlavesNames {
		if err := ValidateVMName(name); err != nil {
			return nil, err
		}
		if strings.Contains(name, ",") {
			return nil, errors.New("host names cannot contain commas")
		}
		q.Add("slavesnames", name)
	}
	path := "/virsh/getcpudisablefeatures"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return c.vmReadJSON(ctx, "GetVMCPUDisableFeatures", "GET", path, nil)
}

// vmReadJSON preserves original JSON bytes after validating the source-specific
// shape. Proto encoding/json omits false/zero fields; protojson getters emit them.
func (c *Client) vmReadJSON(ctx context.Context, kind, method, path string, body any) (json.RawMessage, error) {
	raw, err := c.vmJSON(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	if err = ValidateVMResponse(kind, raw); err != nil {
		return nil, err
	}
	if kind == "ListVMDisks" && string(raw) != "null" {
		var records []map[string]json.RawMessage
		json.Unmarshal(raw, &records)
		changed := false
		for _, record := range records {
			if value, ok := record["status_error"]; ok {
				var message string
				json.Unmarshal(value, &message)
				if message != "" {
					record["status_error"] = json.RawMessage(`"backend disk inspection failed"`)
					changed = true
				}
			}
		}
		if changed {
			raw, _ = json.Marshal(records)
		}
	}
	return raw, nil
}
func ValidateVMResponse(kind string, raw json.RawMessage) error {
	bad := errors.New("HyperHive API returned an unexpected VM response shape")
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&v) != nil {
		return bad
	}
	obj, object := v.(map[string]any)
	required := map[string]string{}
	switch kind {
	case "GetVMSnapshot":
		required = map[string]string{"name": "string", "uuid": "string", "machine_name": "string", "active": "bool", "inactive_xml": "string", "current_xml": "string"}
	case "MachineType":
		required = map[string]string{"vm_name": "string", "machine_type": "string"}
	case "TunedResult":
		required = map[string]string{"ok": "bool", "message": "string", "currentActiveProfile": "string"}
	case "IrqResult":
		required = map[string]string{"ok": "bool", "message": "string", "enabled": "bool", "active": "bool", "unitName": "string"}
	case "GetVMCPUDisableFeatures":
		if _, ok := v.(string); !ok {
			return bad
		}
		return nil
	case "ListVMs", "ListVMAttachedDisks", "ListVMBackups", "ListVMAutomaticBackups", "ListVMXMLTemplates", "ListVMDisks":
		if v == nil {
			return nil
		}
		items, ok := v.([]any)
		if !ok {
			return bad
		}
		for _, item := range items {
			if _, ok := item.(map[string]any); !ok {
				return bad
			}
			switch kind {
			case "ListVMBackups":
				b, _ := json.Marshal(item)
				if ValidateVMResponse("VMBackup", b) != nil {
					return bad
				}
			case "ListVMAutomaticBackups":
				b, _ := json.Marshal(item)
				if ValidateVMResponse("VMAutomaticBackup", b) != nil {
					return bad
				}
			case "ListVMAttachedDisks":
				b, _ := json.Marshal(item)
				if ValidateVMResponse("VMAttachedDisk", b) != nil {
					return bad
				}
			case "ListVMs":
				b, _ := json.Marshal(item)
				if ValidateVMResponse("GetVM", b) != nil {
					return bad
				}
			case "ListVMXMLTemplates":
				b, _ := json.Marshal(item)
				if ValidateVMResponse("GetVMXMLTemplate", b) != nil {
					return bad
				}
			case "ListVMDisks":
				b, _ := json.Marshal(item)
				if ValidateVMResponse("VMDisk", b) != nil {
					return bad
				}
			}
		}
		return nil
	case "GetVM":
		required = map[string]string{"name": "string", "machineName": "string"}
	case "GetVMXML":
		required = map[string]string{"vm_xml": "string", "machine_name": "string"}
	case "GetVMCPUXML":
		required = map[string]string{"cpu_xml": "string"}
	case "GetVMXMLTemplate":
		required = map[string]string{"id": "number", "name": "string", "description": "string", "xml": "string"}
	case "VMAttachedDisk":
		required = map[string]string{"id": "number", "name": "string", "nfs_id": "number", "disk_path": "string", "folder_path": "string", "format": "string", "size_gb": "number", "attached_vm_name": "string", "attached_machine_name": "string", "created_at": "string"}
	case "VMBackup":
		required = map[string]string{"db_res": "object", "live": "bool", "size": "float"}
	case "VMBackupDB":
		required = map[string]string{"Id": "number", "Name": "string", "Path": "string", "NfsId": "number", "CreatedAt": "string", "Automatic": "bool"}
	case "VMAutomaticBackup":
		required = map[string]string{"Id": "number", "VmName": "string", "FrequencyDays": "number", "MinTime": "string", "MaxTime": "string", "NfsMountId": "number", "MaxBackupsRetain": "number", "Enabled": "bool"}
	case "VMDisk":
		required = map[string]string{"id": "number", "name": "string", "nfs_id": "number", "disk_path": "string", "folder_path": "string", "format": "string", "size_gb": "number", "occupied_gb": "number", "occupied_bytes": "number", "attached_vm_name": "string"}
	case "ListVMMachineTypes":
		required = map[string]string{"machine_types": "array?"}
	case "GetVMCPUPinning":
		required = map[string]string{"hasPinning": "bool", "pins": "array", "rangeStart": "number", "rangeEnd": "number", "hyperThreading": "bool", "socketId": "number", "emulatorCpuset": "string"}
	case "GetVMCPUTopology":
		required = map[string]string{"sockets": "array"}
	case "GetVMTunedProfiles":
		required = map[string]string{"profiles": "array", "currentActiveProfile": "string"}
	case "GetVMIrqBalance":
		required = map[string]string{"enabled": "bool", "active": "bool", "unitName": "string"}
	case "GetVMCoreIsolation":
		required = map[string]string{"enabled": "bool", "rebootRequired": "bool", "message": "string", "isolcpus": "string", "nohzFull": "string", "rcuNocbs": "string", "activeIsolcpus": "string", "configuredCpus": "array", "activeCpus": "array", "sockets": "array"}
	case "GetVMHostHugePages":
		required = map[string]string{"enabled": "bool", "rebootRequired": "bool", "message": "string", "pageSize": "string", "pageCount": "number", "activePageSize": "string", "activePageCount": "number", "defaultHugepagesz": "string", "hugepagesz": "string", "hugepages": "string", "activeDefaultHugepagesz": "string", "activeHugepagesz": "string", "activeHugepages": "string"}
	case "GetVMKVM":
		required = map[string]string{"vm_name": "string", "hidden": "bool"}
	case "GetVMHyperV":
		required = map[string]string{"vm_name": "string", "hyperv": "bool"}
	case "GetVMVideo", "GetVMBallooning", "GetVMHugePages": // All fields can be omitted by encoding/json.
	default:
		return bad
	}
	if !object || obj == nil {
		return bad
	}
	for key, typ := range required {
		val, ok := obj[key]
		if !ok {
			return bad
		}
		switch typ {
		case "object":
			if _, ok := val.(map[string]any); !ok {
				return bad
			}
		case "float":
			if _, ok := val.(json.Number); !ok {
				return bad
			}
		case "string":
			if _, ok := val.(string); !ok {
				return bad
			}
		case "bool":
			if _, ok := val.(bool); !ok {
				return bad
			}
		case "number":
			number, ok := val.(json.Number)
			if !ok {
				return bad
			}
			if key != "occupied_gb" {
				if _, err := number.Int64(); err != nil {
					return bad
				}
			}
		case "array?":
			if val == nil {
				continue
			}
			fallthrough
		case "array":
			if _, ok := val.([]any); !ok {
				return bad
			}
		}
	}
	// Validate optional fields of proto encoding/json getters without requiring
	// fields omitted for false/zero values.
	optional := map[string]string{}
	switch kind {
	case "GetVMVideo":
		optional = map[string]string{"enabled": "bool", "video_count": "number", "model_type": "string", "video_xml": "string"}
	case "GetVMBallooning":
		optional = map[string]string{"enabled": "bool", "memory_locked": "bool", "has_memballoon": "bool", "memballoon_model": "string"}
	case "GetVMHugePages":
		optional = map[string]string{"enabled": "bool", "has_hugepages": "bool", "memory_locked": "bool"}
	}
	for key, typ := range optional {
		val, exists := obj[key]
		if !exists {
			continue
		}
		switch typ {
		case "bool":
			if _, ok := val.(bool); !ok {
				return bad
			}
		case "number":
			number, ok := val.(json.Number)
			if !ok {
				return bad
			}
			if key != "occupied_gb" {
				if _, err := number.Int64(); err != nil {
					return bad
				}
			}
		case "string":
			if _, ok := val.(string); !ok {
				return bad
			}
		}
	}
	if kind == "VMBackup" {
		b, _ := json.Marshal(obj["db_res"])
		if ValidateVMResponse("VMBackupDB", b) != nil {
			return bad
		}
	}
	if kind == "GetVMCPUPinning" {
		for _, key := range []string{"rangeStart", "rangeEnd", "socketId"} {
			number := obj[key].(json.Number)
			if _, err := strconv.ParseInt(number.String(), 10, 32); err != nil {
				return bad
			}
		}
		for _, pin := range obj["pins"].([]any) {
			entry, ok := pin.(map[string]any)
			if !ok {
				return bad
			}
			vcpu, ok := entry["vcpu"].(json.Number)
			if !ok {
				return bad
			}
			if _, err := strconv.ParseInt(vcpu.String(), 10, 32); err != nil {
				return bad
			}
			if _, ok := entry["cpuset"].(string); !ok {
				return bad
			}
			if _, ok := entry["isHt"].(bool); !ok {
				return bad
			}
		}
	}
	if kind == "VMDisk" || kind == "VMAttachedDisk" {
		for _, key := range []string{"id", "nfs_id", "size_gb"} {
			number := obj[key].(json.Number)
			n, err := number.Int64()
			if err != nil || n <= 0 {
				return bad
			}
		}
		if obj["name"] == "" || obj["disk_path"] == "" || obj["folder_path"] == "" || obj["format"] == "" {
			return bad
		}
		if value, ok := obj["status_error"]; ok {
			if _, ok := value.(string); !ok {
				return bad
			}
		}
	}

	if kind == "GetVMSnapshot" {
		name := obj["name"].(string)
		uuid := obj["uuid"].(string)
		machine := obj["machine_name"].(string)
		if name == "" || uuid == "" || machine == "" {
			return bad
		}
		validate := func(value string) bool {
			var doc struct {
				XMLName xml.Name `xml:"domain"`
				Name    string   `xml:"name"`
				UUID    string   `xml:"uuid"`
			}
			return xml.Unmarshal([]byte(value), &doc) == nil && doc.XMLName.Local == "domain" && doc.Name == name && doc.UUID == uuid
		}
		if !validate(obj["inactive_xml"].(string)) {
			return bad
		}
		if obj["active"].(bool) {
			if !validate(obj["current_xml"].(string)) {
				return bad
			}
		} else if obj["current_xml"].(string) != "" {
			return bad
		}
	}

	if kind == "GetVMXML" {
		var doc struct {
			XMLName xml.Name `xml:"domain"`
			Name    string   `xml:"name"`
		}
		value := obj["vm_xml"].(string)
		if xml.Unmarshal([]byte(value), &doc) != nil || doc.XMLName.Local != "domain" || strings.TrimSpace(doc.Name) == "" || strings.TrimSpace(obj["machine_name"].(string)) == "" {
			return bad
		}
	}
	if kind == "GetVM" && (strings.TrimSpace(obj["name"].(string)) == "" || strings.TrimSpace(obj["machineName"].(string)) == "") {
		return bad
	}
	return nil
}

// GetVMSnapshot is an internal pre-change helper, not a separately registered tool.
func (c *Client) GetVMSnapshot(ctx context.Context, in VMXMLInput) (json.RawMessage, error) {
	if err := ValidateVMName(in.VMName); err != nil {
		return nil, err
	}
	if err := ValidateVMName(in.MachineName); err != nil {
		return nil, err
	}
	return c.vmReadJSON(ctx, "GetVMSnapshot", "GET", "/virsh/snapshot/"+url.PathEscape(in.VMName)+"?machine_name="+url.QueryEscape(in.MachineName), nil)
}
