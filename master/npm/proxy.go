package npm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type StringOrInt string

func (s *StringOrInt) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*s = ""
		return nil
	}
	if data[0] == '"' {
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return err
		}
		*s = StringOrInt(str)
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(data, &num); err != nil {
		return err
	}
	*s = StringOrInt(num.String())
	return nil
}

func (s StringOrInt) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

func (s StringOrInt) String() string {
	return string(s)
}

type Proxy struct {
	ID                    int            `json:"id"`
	DomainNames           []string       `json:"domain_names"`
	ForwardScheme         string         `json:"forward_scheme"`
	ForwardHost           string         `json:"forward_host"`
	ForwardPort           int            `json:"forward_port"`
	CachingEnabled        bool           `json:"caching_enabled"`
	BlockExploits         bool           `json:"block_exploits"`
	AllowWebsocketUpgrade bool           `json:"allow_websocket_upgrade"`
	AccessListID          StringOrInt    `json:"access_list_id"`
	CertificateID         int            `json:"certificate_id"`
	Meta                  map[string]any `json:"meta"`
	AdvancedConfig        string         `json:"advanced_config"`
	Locations             []any          `json:"locations"`
	Http2Support          bool           `json:"http2_support"`
	HstsEnabled           bool           `json:"hsts_enabled"`
	HstsSubdomains        bool           `json:"hsts_subdomains"`
	SslForced             bool           `json:"ssl_forced"`
	Enabled               bool           `json:"enabled"`
}

/*
	{
	   "domain_names":[
	      "ola.127.0.0.1"
	   ],
	   "forward_scheme":"http",
	   "forward_host":"192.168.1.89",
	   "forward_port":95,
	   "caching_enabled":true,
	   "block_exploits":true,
	   "allow_websocket_upgrade":true,
	   "access_list_id":"0",
	   "certificate_id":0,
	   "meta":{
	      "letsencrypt_agree":false,
	      "dns_challenge":false
	   },
	   "advanced_config":"",
	   "locations":[

	   ],
	   "http2_support":false,
	   "hsts_enabled":false,
	   "hsts_subdomains":false,
	   "ssl_forced":false
	}
*/
func CreateProxy(baseURL, token string, p Proxy) (int, error) {
	reqBody := map[string]any{
		"domain_names":            p.DomainNames,
		"forward_scheme":          p.ForwardScheme,
		"forward_host":            p.ForwardHost,
		"forward_port":            p.ForwardPort,
		"caching_enabled":         p.CachingEnabled,
		"block_exploits":          p.BlockExploits,
		"allow_websocket_upgrade": p.AllowWebsocketUpgrade,
		"access_list_id":          p.AccessListID.String(),
		"certificate_id":          p.CertificateID,
		"meta":                    p.Meta,
		"advanced_config":         p.AdvancedConfig,
		"locations":               p.Locations,
		"http2_support":           p.Http2Support,
		"hsts_enabled":            p.HstsEnabled,
		"hsts_subdomains":         p.HstsSubdomains,
		"ssl_forced":              p.SslForced,
	}

	//marshal to json
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequest("POST", baseURL+"/api/nginx/proxy-hosts", bytes.NewReader(jsonData))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return 0, fmt.Errorf("create proxy failed (%d): %s", resp.StatusCode, respBody)
	}

	//print body
	id := -1
	var respData map[string]any
	if err := json.Unmarshal(respBody, &respData); err == nil {
		if d, ok := respData["id"].(float64); ok {
			id = int(d)
		}
	}
	return id, nil
}

func EditProxy(baseURL, token string, p Proxy) error {
	reqBody := map[string]any{
		"domain_names":            p.DomainNames,
		"forward_scheme":          p.ForwardScheme,
		"forward_host":            p.ForwardHost,
		"forward_port":            p.ForwardPort,
		"caching_enabled":         p.CachingEnabled,
		"block_exploits":          p.BlockExploits,
		"allow_websocket_upgrade": p.AllowWebsocketUpgrade,
		"access_list_id":          p.AccessListID.String(),
		"certificate_id":          p.CertificateID,
		"meta":                    p.Meta,
		"advanced_config":         p.AdvancedConfig,
		"locations":               p.Locations,
		"http2_support":           p.Http2Support,
		"hsts_enabled":            p.HstsEnabled,
		"hsts_subdomains":         p.HstsSubdomains,
		"ssl_forced":              p.SslForced,
	}

	//marshal to json
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", fmt.Sprintf("%s/api/nginx/proxy-hosts/%d", baseURL, p.ID), bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("edit proxy failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

// POST TO /api/nginx/proxy-hosts/{id}/disable
func DisableProxy(baseURL, token string, id int) error {
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/nginx/proxy-hosts/%d/disable", baseURL, id), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("disable proxy failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

func EnableProxy(baseURL, token string, id int) error {
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/nginx/proxy-hosts/%d/enable", baseURL, id), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("enable proxy failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

// DELETE /api/nginx/proxy-hosts/{id}
func DeleteProxy(baseURL, token string, id int) error {
	req, err := http.NewRequest("DELETE", fmt.Sprintf("%s/api/nginx/proxy-hosts/%d", baseURL, id), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return fmt.Errorf("delete proxy failed (%d): %s", resp.StatusCode, respBody)
	}
	return nil
}

func GetAllProxys(baseURL, token string) ([]Proxy, error) {
	req, err := http.NewRequest("GET", baseURL+"/api/nginx/proxy-hosts", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("get proxys failed (%d): %s", resp.StatusCode, respBody)
	}

	var proxys []Proxy
	if err := json.Unmarshal(respBody, &proxys); err != nil {
		return nil, err
	}

	return proxys, nil
}
