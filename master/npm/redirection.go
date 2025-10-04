package npm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// /api/nginx/redirection-hosts

/*
{
   "domain_names":[
      "ola.peni9s.com",
      "meudeis.edwed"
   ],
   "forward_scheme":"https",
   "forward_domain_name":"https://google.com",
   "forward_http_code":"307",
   "preserve_path":true,
   "block_exploits":true,
   "certificate_id":0,
   "meta":{
      "letsencrypt_agree":false,
      "dns_challenge":false
   },
   "advanced_config":"",
   "http2_support":false,
   "hsts_enabled":false,
   "hsts_subdomains":false,
   "ssl_forced":false
}
*/

type Redirection struct {
	ID                int         `json:"id"`
	Domain_names      []string    `json:"domain_names"`
	Forward_scheme    string      `json:"forward_scheme"`
	Forward_domain    string      `json:"forward_domain_name"`
	Forward_http_code StringOrInt `json:"forward_http_code"`
	Preserve_path     bool        `json:"preserve_path"`
	Block_exploits    bool        `json:"block_exploits"`
	Certificate       int         `json:"certificate_id"`
	Meta              struct {
		Letsencrypt_agree bool `json:"letsencrypt_agree"`
		Dns_challenge     bool `json:"dns_challenge"`
	} `json:"meta"`
	Advanced_config string `json:"advanced_config"`
	Http2_support   bool   `json:"http2_support"`
	Hsts_enabled    bool   `json:"hsts_enabled"`
	Hsts_subdomains bool   `json:"hsts_subdomains"`
	Ssl_forced      bool   `json:"ssl_forced"`
	Enabled         bool   `json:"enabled"`
}

// POST to /api/nginx/redirection-hosts
func CreateRedirection(baseURL, token string, p Redirection) (int, error) {
	reqBody := map[string]any{
		"domain_names":        p.Domain_names,
		"forward_scheme":      p.Forward_scheme,
		"forward_domain_name": p.Forward_domain,
		"forward_http_code":   p.Forward_http_code,
		"preserve_path":       p.Preserve_path,
		"block_exploits":      p.Block_exploits,
		"certificate_id":      p.Certificate,
		"meta":                p.Meta,
		"advanced_config":     p.Advanced_config,
		"http2_support":       p.Http2_support,
		"hsts_enabled":        p.Hsts_enabled,
		"hsts_subdomains":     p.Hsts_subdomains,
		"ssl_forced":          p.Ssl_forced,
	}

	//marshal to json
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	resp, err := MakeRequest("POST", baseURL+"/api/nginx/redirection-hosts", token, bytes.NewReader(jsonData), 30)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return 0, fmt.Errorf("create redirection failed (%d): %s", resp.StatusCode, respBody)
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

func EditRedirection(baseURL, token string, p Redirection) error {
	reqBody := map[string]any{
		"domain_names":        p.Domain_names,
		"forward_scheme":      p.Forward_scheme,
		"forward_domain_name": p.Forward_domain,
		"forward_http_code":   p.Forward_http_code,
		"preserve_path":       p.Preserve_path,
		"block_exploits":      p.Block_exploits,
		"certificate_id":      p.Certificate,
		"meta":                p.Meta,
		"advanced_config":     p.Advanced_config,
		"http2_support":       p.Http2_support,
		"hsts_enabled":        p.Hsts_enabled,
		"hsts_subdomains":     p.Hsts_subdomains,
		"ssl_forced":          p.Ssl_forced,
	}
	//marshal to json
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	resp, err := MakeRequest("PUT", fmt.Sprintf("%s/api/nginx/redirection-hosts/%d", baseURL, p.ID), token, bytes.NewReader(jsonData), 30)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("edit redirection failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

func DeleteRedirection(baseURL, token string, id int) error {
	resp, err := MakeRequest("DELETE", fmt.Sprintf("%s/api/nginx/redirection-hosts/%d", baseURL, id), token, nil, 30)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("delete redirection failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

func DisableRedirection(baseURL, token string, id int) error {
	resp, err := MakeRequest("POST", fmt.Sprintf("%s/api/nginx/redirection-hosts/%d/disable", baseURL, id), token, nil, 30)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("disable redirection failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

func EnableRedirection(baseURL, token string, id int) error {
	resp, err := MakeRequest("POST", fmt.Sprintf("%s/api/nginx/redirection-hosts/%d/enable", baseURL, id), token, nil, 30)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("enable redirection failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}
func ListRedirections(baseURL, token string) ([]Redirection, error) {
	resp, err := MakeRequest("GET", fmt.Sprintf("%s/api/nginx/redirection-hosts", baseURL), token, nil, 30)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("list redirections failed (%d): %s", resp.StatusCode, respBody)
	}

	var redirections []Redirection
	if err := json.Unmarshal(respBody, &redirections); err != nil {
		return nil, err
	}

	return redirections, nil
}
