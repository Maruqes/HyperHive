package services

import (
	"512SvMan/db"
	"512SvMan/virsh"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"google.golang.org/grpc"
)

const (
	VMXMLTemplatePlaceholderQCOW2 = "$qcow2"
	VMXMLTemplatePlaceholderName  = "$name"
)

type VMXMLTemplateRenderParams struct {
	VMName    string
	QCOW2Path string
}

type vmXMLTemplateValidateDomain struct {
	XMLName xml.Name                  `xml:"domain"`
	Name    string                    `xml:"name"`
	Devices vmXMLTemplateValidateDevs `xml:"devices"`
}

type vmXMLTemplateValidateDevs struct {
	Disks []vmXMLTemplateValidateDisk `xml:"disk"`
}

type vmXMLTemplateValidateDisk struct {
	Type   string                    `xml:"type,attr"`
	Device string                    `xml:"device,attr"`
	Driver *vmXMLTemplateValidateDrv `xml:"driver"`
	Source *vmXMLTemplateValidateSrc `xml:"source"`
}

type vmXMLTemplateValidateDrv struct {
	Type string `xml:"type,attr"`
}

type vmXMLTemplateValidateSrc struct {
	File string `xml:"file,attr"`
}

func ValidateVMXMLTemplate(xmlContent string) error {
	xmlContent = strings.TrimSpace(xmlContent)
	if xmlContent == "" {
		return fmt.Errorf("vm xml template is empty")
	}

	var doc vmXMLTemplateValidateDomain
	if err := xml.Unmarshal([]byte(xmlContent), &doc); err != nil {
		return fmt.Errorf("invalid domain xml: %w", err)
	}
	if !strings.EqualFold(doc.XMLName.Local, "domain") {
		return fmt.Errorf("invalid domain xml: missing <domain> root element")
	}
	if strings.TrimSpace(doc.Name) == "" {
		return fmt.Errorf("invalid domain xml: missing <name>")
	}

	for _, disk := range doc.Devices.Disks {
		device := strings.ToLower(strings.TrimSpace(disk.Device))
		if device != "" && device != "disk" {
			continue
		}
		if disk.Source == nil {
			continue
		}
		sourceFile := strings.TrimSpace(disk.Source.File)
		if !strings.Contains(sourceFile, VMXMLTemplatePlaceholderQCOW2) {
			continue
		}

		driverType := ""
		if disk.Driver != nil {
			driverType = strings.ToLower(strings.TrimSpace(disk.Driver.Type))
		}
		if driverType != "qcow2" {
			return fmt.Errorf("template disk using %s must have driver type='qcow2'", VMXMLTemplatePlaceholderQCOW2)
		}
		return nil
	}

	return fmt.Errorf("template must contain a qcow2 disk source with placeholder %s", VMXMLTemplatePlaceholderQCOW2)
}

func RenderVMXMLTemplate(xmlContent string, params VMXMLTemplateRenderParams) (string, error) {
	if err := ValidateVMXMLTemplate(xmlContent); err != nil {
		return "", err
	}

	params.VMName = strings.TrimSpace(params.VMName)
	params.QCOW2Path = strings.TrimSpace(params.QCOW2Path)
	if params.VMName == "" {
		return "", fmt.Errorf("vm name is required")
	}
	if params.QCOW2Path == "" {
		return "", fmt.Errorf("qcow2 path is required")
	}

	dec := xml.NewDecoder(strings.NewReader(xmlContent))
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)

	depth := 0
	skipDepth := 0
	inRootName := false
	wroteRootName := false
	wroteCurrentRootNameText := false

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("parse domain xml: %w", err)
		}

		if skipDepth > 0 {
			switch tok.(type) {
			case xml.StartElement:
				skipDepth++
			case xml.EndElement:
				skipDepth--
			}
			continue
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 1 && strings.EqualFold(t.Name.Local, "uuid") {
				skipDepth = 1
				continue
			}
			for i := range t.Attr {
				t.Attr[i].Value = replaceVMXMLTemplatePlaceholders(t.Attr[i].Value, params)
			}
			if depth == 1 && strings.EqualFold(t.Name.Local, "name") {
				inRootName = true
				wroteCurrentRootNameText = false
			}
			if err := enc.EncodeToken(t); err != nil {
				return "", fmt.Errorf("encode domain xml: %w", err)
			}
			depth++
		case xml.EndElement:
			if inRootName && depth == 2 && strings.EqualFold(t.Name.Local, "name") {
				inRootName = false
			}
			if err := enc.EncodeToken(t); err != nil {
				return "", fmt.Errorf("encode domain xml: %w", err)
			}
			if depth > 0 {
				depth--
			}
		case xml.CharData:
			if inRootName {
				if wroteCurrentRootNameText {
					continue
				}
				if err := enc.EncodeToken(xml.CharData([]byte(params.VMName))); err != nil {
					return "", fmt.Errorf("encode domain xml: %w", err)
				}
				wroteRootName = true
				wroteCurrentRootNameText = true
				continue
			}
			replaced := replaceVMXMLTemplatePlaceholders(string(t), params)
			if err := enc.EncodeToken(xml.CharData([]byte(replaced))); err != nil {
				return "", fmt.Errorf("encode domain xml: %w", err)
			}
		default:
			if err := enc.EncodeToken(tok); err != nil {
				return "", fmt.Errorf("encode domain xml: %w", err)
			}
		}
	}

	if err := enc.Flush(); err != nil {
		return "", fmt.Errorf("flush domain xml: %w", err)
	}

	out := buf.String()
	if !wroteRootName {
		return "", fmt.Errorf("template is missing writable root <name> element")
	}
	if strings.Contains(out, VMXMLTemplatePlaceholderQCOW2) {
		return "", fmt.Errorf("template placeholder %s was not fully replaced", VMXMLTemplatePlaceholderQCOW2)
	}
	return out, nil
}

func replaceVMXMLTemplatePlaceholders(in string, params VMXMLTemplateRenderParams) string {
	replacer := strings.NewReplacer(
		VMXMLTemplatePlaceholderQCOW2, params.QCOW2Path,
		VMXMLTemplatePlaceholderName, params.VMName,
	)
	return replacer.Replace(in)
}

func (v *VirshService) prepareVMXMLTemplateForCreate(ctx context.Context, templateID int, vmName, qcow2Path string) (string, error) {
	if templateID <= 0 {
		return "", nil
	}

	tmpl, err := db.GetVMXMLTemplateByID(ctx, templateID)
	if err != nil {
		return "", fmt.Errorf("failed to get vm xml template %d: %w", templateID, err)
	}
	if tmpl == nil {
		return "", fmt.Errorf("vm xml template %d not found", templateID)
	}

	renderedXML, err := RenderVMXMLTemplate(tmpl.XML, VMXMLTemplateRenderParams{
		VMName:    vmName,
		QCOW2Path: qcow2Path,
	})
	if err != nil {
		return "", fmt.Errorf("invalid vm xml template %d (%s): %w", tmpl.Id, tmpl.Name, err)
	}
	return renderedXML, nil
}

func (v *VirshService) applyRenderedVMXMLTemplateAfterCreate(ctx context.Context, slaveConn *grpc.ClientConn, vmName, renderedXML string) error {
	if strings.TrimSpace(renderedXML) == "" {
		return nil
	}

	vm, err := virsh.GetVmByName(slaveConn, &grpcVirsh.GetVmByNameRequest{Name: vmName})
	if err != nil {
		return fmt.Errorf("failed to get newly created VM %s: %w", vmName, err)
	}
	if vm == nil {
		return fmt.Errorf("newly created VM %s not found", vmName)
	}

	if err := virsh.ForceShutdownVM(slaveConn, vm); err != nil {
		return fmt.Errorf("failed to shutdown VM %s before applying xml template: %w", vmName, err)
	}

	if err := virsh.UpdateVMXml(slaveConn, vmName, renderedXML); err != nil {
		return fmt.Errorf("failed to apply xml template to VM %s: %w", vmName, err)
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if err := virsh.StartVm(ctx, slaveConn, vm); err != nil {
		return fmt.Errorf("failed to restart VM %s after applying xml template: %w", vmName, err)
	}

	return nil
}
