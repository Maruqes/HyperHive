package client

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// CelldObservationInput mirrors every supported logs/events query parameter.
// Pointers preserve an omitted value versus an explicitly supplied zero/false.
type CelldObservationInput struct {
	CelldFleetIDInput
	From     *int64 `json:"from,omitempty" jsonschema:"Start Unix milliseconds; zero defaults to one hour before to; API clamps to seven-day retention"`
	To       *int64 `json:"to,omitempty" jsonschema:"End Unix milliseconds; zero defaults to now or the cursor end time"`
	Limit    *int   `json:"limit,omitempty" jsonschema:"Page size, 0 for default 200, maximum 500 or 2000 with download"`
	Cursor   string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor returned by the API; maximum 1024 bytes"`
	Node     string `json:"node,omitempty" jsonschema:"Exact node filter, maximum 128 bytes"`
	Workload string `json:"workload,omitempty" jsonschema:"Exact workload filter, maximum 256 bytes"`
	Source   string `json:"source,omitempty" jsonschema:"Exact source filter, maximum 32 bytes"`
	Severity string `json:"severity,omitempty" jsonschema:"Severity filter, normalized to uppercase by API, maximum 16 bytes"`
	Text     string `json:"text,omitempty" jsonschema:"Text search, maximum 256 bytes"`
	Download *bool  `json:"download,omitempty" jsonschema:"Request bounded NDJSON export; false or omission selects a JSON page"`
}

func ValidateCelldObservationInput(input CelldObservationInput) error {
	if err := ValidateCelldFleetID(input.ID); err != nil {
		return err
	}
	cap := 500
	if input.Download != nil && *input.Download {
		cap = 2000
	}
	if input.Limit != nil && (*input.Limit < 0 || *input.Limit > cap) {
		return errors.New("limit must be 0–500, or 0–2000 for download")
	}
	if input.From != nil && input.To != nil && *input.From != 0 && *input.To != 0 && *input.From > *input.To {
		return errors.New("from must not exceed to")
	}
	if len(input.Cursor) > 1024 || len(input.Node) > 128 || len(input.Workload) > 256 || len(input.Source) > 32 || len(strings.ToUpper(input.Severity)) > 16 || len(input.Text) > 256 {
		return errors.New("Celld observation filter exceeds its byte limit")
	}
	return nil
}

func (input CelldObservationInput) query() string {
	q := url.Values{}
	if input.From != nil {
		q.Set("from", strconv.FormatInt(*input.From, 10))
	}
	if input.To != nil {
		q.Set("to", strconv.FormatInt(*input.To, 10))
	}
	if input.Limit != nil {
		q.Set("limit", strconv.Itoa(*input.Limit))
	}
	if input.Download != nil {
		q.Set("download", strconv.FormatBool(*input.Download))
	}
	for k, v := range map[string]string{"cursor": input.Cursor, "node": input.Node, "workload": input.Workload, "source": input.Source, "severity": input.Severity, "text": input.Text} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

type celldObservationResponse struct {
	download bool
	result   CelldObject
}

func (*celldObservationResponse) acceptStatus(int) bool { return false }
func (r *celldObservationResponse) decodeResponse(data []byte, headers http.Header, _ int) error {
	if !r.download {
		result, err := decodeCelldObject(data, map[string]string{"records": "array", "from": "number", "to": "number", "truncated": "bool", "incomplete": "bool"})
		if err != nil {
			return err
		}
		for _, record := range result["records"].([]any) {
			if _, ok := record.(map[string]any); !ok {
				return errors.New("HyperHive API returned an invalid observation record")
			}
		}
		r.result = result
		return nil
	}
	if !strings.HasPrefix(headers.Get("Content-Type"), "application/x-ndjson") {
		return errors.New("HyperHive API returned an unexpected Celld export content type")
	}
	records := []CelldObject{}
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		record, err := decodeCelldObject(line, map[string]string{"id": "string", "node": "string", "generation": "string", "received_ms": "number"})
		if err != nil {
			return err
		}
		records = append(records, record)
		if len(records) > 2000 {
			return errors.New("HyperHive API exceeded the Celld export record limit")
		}
	}
	truncated := headers.Get("X-Observability-Truncated")
	if truncated != "" && truncated != "true" {
		return errors.New("HyperHive API returned invalid Celld export metadata")
	}
	r.result = CelldObject{"records": records, "truncated": truncated == "true"}
	if cursor := headers.Get("X-Next-Cursor"); cursor != "" {
		r.result["next_cursor"] = cursor
	}
	return nil
}

func (c *Client) celldObservations(ctx context.Context, input CelldObservationInput, kind string) (CelldObject, error) {
	if err := ValidateCelldObservationInput(input); err != nil {
		return nil, err
	}
	path, err := celldFleetPath(input.ID, "/"+kind)
	if err != nil {
		return nil, err
	}
	response := celldObservationResponse{download: input.Download != nil && *input.Download}
	if err = c.request(ctx, http.MethodGet, path+input.query(), nil, &response); err != nil {
		return nil, err
	}
	return response.result, nil
}
func (c *Client) GetCelldLogs(ctx context.Context, input CelldObservationInput) (CelldObject, error) {
	return c.celldObservations(ctx, input, "logs")
}

func (c *Client) GetCelldEvents(ctx context.Context, input CelldObservationInput) (CelldObject, error) {
	return c.celldObservations(ctx, input, "events")
}
