package npm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

//streams.go

/*
{
   "incoming_port":6060,
   "forwarding_host":"10.0.0.1",    pode ser um ipv4/6
   "forwarding_port":60,
   "tcp_forwarding":true,
   "udp_forwarding":true,
   "certificate_id":0,
   "meta":{
      "dns_provider_credentials":"",
      "letsencrypt_agree":false,
      "dns_challenge":true
   }
}
*/

type Stream struct {
	UpstreamID      json.RawMessage `json:"upstream_id,omitempty"`
	ID              int             `json:"id"`
	Description     string          `json:"description"`
	Incoming_port   int             `json:"incoming_port"`
	Forwarding_host string          `json:"forwarding_host"`
	Forwarding_port int             `json:"forwarding_port"`
	Tcp_forwarding  bool            `json:"tcp_forwarding"`
	Udp_forwarding  bool            `json:"udp_forwarding"`
	Certificate     int             `json:"certificate_id"`
	Meta            map[string]any  `json:"meta"`
	Enabled         bool            `json:"enabled"`
}

// POST to /api/nginx/streams
func rawCreateStream(baseURL, token string, p Stream) (int, error) {
	reqBody := map[string]any{
		"incoming_port":   p.Incoming_port,
		"forwarding_host": p.Forwarding_host,
		"forwarding_port": p.Forwarding_port,
		"tcp_forwarding":  p.Tcp_forwarding,
		"udp_forwarding":  p.Udp_forwarding,
		"certificate_id":  p.Certificate,
		"meta":            p.Meta,
	}
	//marshal to json
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	resp, err := MakeRequest("POST", baseURL+"/api/nginx/streams", token, bytes.NewReader(jsonData), 30)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return 0, fmt.Errorf("create stream failed (%d): %s", resp.StatusCode, respBody)
	}

	//print body
	id := -1
	var respData map[string]any
	if err := json.Unmarshal(respBody, &respData); err == nil {
		if d, ok := respData["id"].(float64); ok {
			id = int(d)
		}
	}
	if id < 0 {
		return 0, fmt.Errorf("create stream response did not include an id")
	}
	return id, nil
}

func rawEditStream(baseURL, token string, p Stream) error {
	reqBody := map[string]any{
		"incoming_port":   p.Incoming_port,
		"forwarding_host": p.Forwarding_host,
		"forwarding_port": p.Forwarding_port,
		"tcp_forwarding":  p.Tcp_forwarding,
		"udp_forwarding":  p.Udp_forwarding,
		"certificate_id":  p.Certificate,
		"meta":            p.Meta,
	}
	//marshal to json
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	resp, err := MakeRequest("PUT", fmt.Sprintf("%s/api/nginx/streams/%d", baseURL, p.ID), token, bytes.NewReader(jsonData), 30)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("edit stream failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

func rawDeleteStream(baseURL, token string, id int) error {
	resp, err := MakeRequest("DELETE", fmt.Sprintf("%s/api/nginx/streams/%d", baseURL, id), token, nil, 30)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 && resp.StatusCode != 204 {
		return fmt.Errorf("delete stream failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

func rawEnableStream(baseURL, token string, id int) error {
	resp, err := MakeRequest("POST", fmt.Sprintf("%s/api/nginx/streams/%d/enable", baseURL, id), token, nil, 30)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("enable stream failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

func rawDisableStream(baseURL, token string, id int) error {
	resp, err := MakeRequest("POST", fmt.Sprintf("%s/api/nginx/streams/%d/disable", baseURL, id), token, nil, 30)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("disable stream failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

func rawListStreams(baseURL, token string) ([]Stream, error) {
	resp, err := MakeRequest("GET", baseURL+"/api/nginx/streams", token, nil, 30)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("list streams failed (%d): %s", resp.StatusCode, respBody)
	}

	var streams []Stream
	if err := json.Unmarshal(respBody, &streams); err != nil {
		return nil, err
	}

	return streams, nil
}
