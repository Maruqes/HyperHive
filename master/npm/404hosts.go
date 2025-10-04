package npm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

/*
	{
	  "domain_names": [
	    "testmds.com",
	    "testlipa.com"
	  ],
	  "certificate_id": 0,
	  "meta": {
	    "letsencrypt_agree": false,
	    "dns_challenge": false
	  },
	  "advanced_config": "",
	  "hsts_enabled": false,
	  "hsts_subdomains": false,
	  "http2_support": false,
	  "ssl_forced": false
	}
*/
type Host404 struct {
	ID           int      `json:"id"`
	Domain_names []string `json:"domain_names"`
	Certificate  int      `json:"certificate_id"`
	Meta         struct {
		Letsencrypt_agree bool `json:"letsencrypt_agree"`
		Dns_challenge     bool `json:"dns_challenge"`
	} `json:"meta"`
	Advanced_config string `json:"advanced_config"`
	Hsts_enabled    bool   `json:"hsts_enabled"`
	Hsts_subdomains bool   `json:"hsts_subdomains"`
	Http2_support   bool   `json:"http2_support"`
	Ssl_forced      bool   `json:"ssl_forced"`
	Enabled         bool   `json:"enabled"`
}

// POST to /api/nginx/dead-hosts
func Create404(baseURL, token string, p Host404) (int, error) {
	reqBody := map[string]any{
		"domain_names":    p.Domain_names,
		"certificate_id":  p.Certificate,
		"meta":            p.Meta,
		"advanced_config": p.Advanced_config,
		"hsts_enabled":    p.Hsts_enabled,
		"hsts_subdomains": p.Hsts_subdomains,
		"http2_support":   p.Http2_support,
		"ssl_forced":      p.Ssl_forced,
	}

	//marshal to json
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	resp, err := MakeRequest("POST", baseURL+"/api/nginx/dead-hosts", token, bytes.NewReader(jsonData))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return 0, fmt.Errorf("create 404 failed (%d): %s", resp.StatusCode, respBody)
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

func Edit404(baseURL, token string, p Host404) error {
	reqBody := map[string]any{
		"domain_names":    p.Domain_names,
		"certificate_id":  p.Certificate,
		"meta":            p.Meta,
		"advanced_config": p.Advanced_config,
		"hsts_enabled":    p.Hsts_enabled,
		"hsts_subdomains": p.Hsts_subdomains,
		"http2_support":   p.Http2_support,
		"ssl_forced":      p.Ssl_forced,
	}
	//marshal to json
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	resp, err := MakeRequest("PUT", fmt.Sprintf("%s/api/nginx/dead-hosts/%d", baseURL, p.ID), token, bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("edit 404 failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

func Delete404(baseURL, token string, id int) error {
	resp, err := MakeRequest("DELETE", fmt.Sprintf("%s/api/nginx/dead-hosts/%d", baseURL, id), token, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("delete 404 failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

func Disable404(baseURL, token string, id int) error {
	resp, err := MakeRequest("POST", fmt.Sprintf("%s/api/nginx/dead-hosts/%d/disable", baseURL, id), token, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("disable 404 failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

func Enable404(baseURL, token string, id int) error {
	resp, err := MakeRequest("POST", fmt.Sprintf("%s/api/nginx/dead-hosts/%d/enable", baseURL, id), token, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("enable 404 failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

func List404(baseURL, token string) ([]Host404, error) {
	resp, err := MakeRequest("GET", baseURL+"/api/nginx/dead-hosts", token, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("list 404 failed (%d): %s", resp.StatusCode, respBody)
	}

	var hosts []Host404
	if err := json.Unmarshal(respBody, &hosts); err != nil {
		return nil, err
	}

	return hosts, nil
}
