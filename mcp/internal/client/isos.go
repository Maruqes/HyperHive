package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const ISOsPath = "/isos/"

// ISOObject retains the API's capitalized fields and exact JSON integers.
type ISOObject map[string]any

func decodeISOsJSON(raw []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(dst) != nil {
		return errors.New("HyperHive API returned invalid ISO JSON")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("HyperHive API returned trailing ISO data")
	}
	return nil
}

func (c *Client) ListISOs(ctx context.Context) ([]ISOObject, error) {
	var raw json.RawMessage
	if err := c.getJSON(ctx, ISOsPath, &raw); err != nil {
		return nil, err
	}
	var result []ISOObject
	if err := decodeISOsJSON(raw, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("HyperHive API returned an invalid ISO array")
	}
	for _, iso := range result {
		id, ok := iso["Id"].(json.Number)
		if !ok {
			return nil, errors.New("HyperHive API returned an invalid ISO ID")
		}
		value, err := id.Int64()
		if err != nil || value <= 0 {
			return nil, errors.New("HyperHive API returned an invalid ISO ID")
		}
		for _, key := range []string{"MachineName", "FilePath", "Name"} {
			if _, ok := iso[key].(string); !ok {
				return nil, errors.New("HyperHive API returned an invalid ISO field")
			}
		}
		available, ok := iso["available_on_slaves"].(map[string]any)
		if !ok {
			return nil, errors.New("HyperHive API returned invalid ISO availability")
		}
		for _, value := range available {
			if _, ok := value.(bool); !ok {
				return nil, errors.New("HyperHive API returned invalid ISO availability")
			}
		}
	}
	return result, nil
}

// StartISODownloadInput specifies the entire asynchronous initiation request.
type StartISODownloadInput struct {
	URL        string `json:"url" jsonschema:"Absolute http(s) URL without credentials or fragment ending exactly with lowercase .iso"`
	ISOName    string `json:"iso_name" jsonschema:"New leaf ISO filename without path separators or NUL ending exactly with lowercase .iso"`
	NFSShareID int64  `json:"nfs_share_id" jsonschema:"Positive existing NFS share ID on a connected slave"`
}

func ValidateStartISODownload(input StartISODownloadInput) error {
	parsed, err := url.Parse(input.URL)
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || parsed.Fragment != "" || !strings.HasSuffix(input.URL, ".iso") {
		return errors.New("url must be an absolute http(s) URL without credentials or fragment, ending with .iso")
	}
	if !strings.HasSuffix(input.ISOName, ".iso") || strings.ContainsAny(input.ISOName, "/\\\x00") {
		return errors.New("iso_name must be a leaf filename ending with .iso, without path separators or NUL")
	}
	if input.NFSShareID <= 0 {
		return errors.New("nfs_share_id must be a positive integer")
	}
	return nil
}

// ISOAcceptance is the API initiation response, never a completed ISO object.
type ISOAcceptance map[string]any

type isoAcceptanceDecoder struct{ result ISOAcceptance }

func (*isoAcceptanceDecoder) acceptStatus(int) bool { return false }
func (dst *isoAcceptanceDecoder) decodeResponse(raw []byte, _ http.Header, status int) error {
	if status != http.StatusAccepted {
		return errors.New("HyperHive API returned an unexpected ISO initiation status; expected HTTP 202")
	}
	if err := decodeISOsJSON(raw, &dst.result); err != nil {
		return err
	}
	if dst.result["status"] != "started" {
		return errors.New("HyperHive API returned invalid ISO initiation acceptance")
	}
	return nil
}

// StartISODownload returns acceptance only. Completion is reported asynchronously by the API.
func (c *Client) StartISODownload(ctx context.Context, input StartISODownloadInput) (ISOAcceptance, error) {
	if err := ValidateStartISODownload(input); err != nil {
		return nil, err
	}
	response := &isoAcceptanceDecoder{}
	if err := c.request(ctx, http.MethodPost, ISOsPath+"download", input, response); err != nil {
		return nil, err
	}
	return response.result, nil
}
