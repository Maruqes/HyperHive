package tools

import (
	"errors"
	"strings"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/safety"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Domain validation runs before prompting or persisting a Medium review. The
// feature handlers and API client retain their checks at the dispatch boundary.
func validateRegisteredInput(req *mcp.CallToolRequest, input any, area string, level safety.Level) error {
	if area == "vms" {
		if err := vmArguments(req); err != nil {
			return err
		}
		if isVMWrite(input, level) {
			return client.ValidateVMWrite(input)
		}
	}
	if area == "nginx" && req.Params.Name != "list_dns_providers" {
		switch input.(type) {
		case struct{}:
		default:
			return validateNginxInput(req, input)
		}
	}
	switch in := input.(type) {
	case client.DNSAliasWriteInput:
		return validateDNSInput(req, in)
	case client.DNSAliasInput:
		return validateDNSInput(req, in)
	case client.StartISODownloadInput:
		return client.ValidateStartISODownload(in)
	case client.NoteContent:
		return client.ValidateNoteContent(in)
	case client.EditNoteInput:
		return client.ValidateEditNote(in)
	case client.NoteIDInput:
		return client.ValidateNoteID(in.ID)
	case client.UpdateNotificationRuleInput:
		return client.ValidateUpdateNotificationRule(in)
	case client.SPAPortInput:
		return client.ValidateSPAPort(in)
	case client.CelldFleetIDInput:
		if err := rejectCelldNulls(req); err != nil {
			return err
		}
		return client.ValidateCelldFleetID(in.ID)
	case client.CelldObservationInput:
		if err := rejectCelldNulls(req); err != nil {
			return err
		}
		return client.ValidateCelldObservationInput(in)
	case client.CreateCelldFleetInput:
		if err := rejectCelldNulls(req); err != nil {
			return err
		}
		if in.Name == "" || len(in.Name) > 128 || in.Name != strings.TrimSpace(in.Name) {
			return errors.New("name must have 1–128 bytes without surrounding whitespace")
		}
		if len(in.Servers) == 0 {
			return errors.New("servers must contain at least one server")
		}
		seen := map[string]bool{}
		for _, name := range in.Servers {
			if name == "" || seen[name] {
				return errors.New("servers must contain nonempty unique names")
			}
			seen[name] = true
		}
		if p := in.Ports; p != nil && (p.HTTP < 1 || p.HTTP > 65535 || p.Internal < 1 || p.Internal > 65535 || p.HTTP == p.Internal) {
			return errors.New("ports must be distinct integers between 1 and 65535")
		}
	case client.ProxyConfig:
		return validateConfig(in)
	case client.UpdateProxyInput:
		if in.ID <= 0 {
			return errors.New("id must be positive")
		}
		return validateConfigWithPreserve(in.ProxyConfig, true)
	case client.ProxyIDInput:
		if in.ID <= 0 {
			return errors.New("id must be positive")
		}
	case client.FrontendProxyInput:
		if strings.TrimSpace(in.Domain) == "" || in.ID < 0 {
			return errors.New("domain must be nonblank and certificate ID nonnegative")
		}
	case client.SecureFrontendProxyInput:
		if strings.TrimSpace(in.PublicDomain) == "" || strings.TrimSpace(in.PrivateDomain) == "" || in.PublicCertificateID < 0 || in.PrivateCertificateID < 0 {
			return errors.New("domains must be nonblank and certificate IDs nonnegative")
		}
	case client.UpstreamID:
		if in.ID < 1 {
			return errors.New("id must be positive")
		}
	case client.UpstreamConfig:
		return validateUpstreamConfig(in)
	case client.UpstreamUpdate:
		if in.ID < 1 || in.Revision < 1 {
			return errors.New("positive id and expected revision required")
		}
		return validateUpstreamConfig(in.UpstreamConfig)
	default:
		if validator, ok := input.(interface{ Validate() error }); ok {
			return validator.Validate()
		}
		if level != safety.Low {
			return errors.New("tool has no domain preflight validator; no operation dispatched")
		}
	}
	return nil
}
