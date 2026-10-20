package tools

import (
	"encoding/json"
	"errors"

	"github.com/google/jsonschema-go/jsonschema"
)

func validatePoolTarget(raw json.RawMessage, scheme, host string, port int, preserve bool) error {
	if scheme != "http" && scheme != "https" {
		return errors.New("forward_scheme must be http or https")
	}
	if len(raw) == 0 && preserve && host == "" && port == 0 {
		return nil
	}
	if len(raw) > 0 && string(raw) != "null" {
		var id int64
		if json.Unmarshal(raw, &id) != nil || id < 1 {
			return errors.New("upstream_id must be positive or null")
		}
		if host != "" || port != 0 {
			return errors.New("selected upstream requires empty direct host and zero direct port")
		}
		return nil
	}
	return validateTarget(scheme, host, port)
}

func addUpstreamSelectionSchema(schema *jsonschema.Schema) {
	if schema == nil {
		return
	}
	for _, definition := range schema.Defs {
		addUpstreamSelectionSchema(definition)
	}
	if _, ok := schema.Properties["upstream_id"]; ok {
		min := float64(1)
		schema.Properties["upstream_id"] = &jsonschema.Schema{Description: "Positive compatible pool ID; null explicitly detaches; omission on update preserves the pool. Use empty direct host and zero port for a selected pool.", AnyOf: []*jsonschema.Schema{{Type: "integer", Minimum: &min}, {Type: "null"}}}
	}
	if locations, ok := schema.Properties["locations"]; ok && locations.Items != nil {
		addUpstreamSelectionSchema(locations.Items)
	}
}
