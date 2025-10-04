package npm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"
)

//validate -> POST to /api/nginx/certificates/validate   valida cart key etc etc
//POST to POST /api/nginx/certificates                   cria o certeficado (sem nada apenas a estructura)
//POST /api/nginx/certificates/<id>/upload               upload fod ficheiros para o certeficado

type Cert struct {
	Name            string
	CertPem         []byte
	KeyPem          []byte
	IntermediateCSR []byte
}

func addPart(fieldName, filename, contentType string, data []byte, w *multipart.Writer) error {
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, filename))
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	part, err := w.CreatePart(h)
	if err != nil {
		return err
	}
	_, err = part.Write(data)
	return err
}

func handleCertValidate(baseURL, token string, cert Cert) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	// Match the field names & filenames from your example
	if err := addPart("certificate", "cert.crt", "application/x-x509-ca-cert", cert.CertPem, w); err != nil {
		return err
	}
	if err := addPart("certificate_key", "cert.key", "application/vnd.apple.keynote", cert.KeyPem, w); err != nil { // NPM doesn't care; type can be octet-stream too
		return err
	}
	if len(cert.IntermediateCSR) > 0 {
		if err := addPart("intermediate_certificate", "cert.csr", "application/octet-stream", cert.IntermediateCSR, w); err != nil {
			return err
		}
	}

	if err := w.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, baseURL, &body)
	if err != nil {
		panic(err)
	}
	// Required headers
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", w.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cert validate failed (%d): %s", resp.StatusCode, b)
	}
	return nil
}

func certUpload(baseURL, token string, certID int, cert Cert) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	// Match the field names & filenames from your example
	if err := addPart("certificate", "cert.crt", "application/x-x509-ca-cert", cert.CertPem, w); err != nil {
		return err
	}
	if err := addPart("certificate_key", "cert.key", "application/vnd.apple.keynote", cert.KeyPem, w); err != nil { // NPM doesn't care; type can be octet-stream too
		return err
	}
	if len(cert.IntermediateCSR) > 0 {
		if err := addPart("intermediate_certificate", "cert.csr", "application/octet-stream", cert.IntermediateCSR, w); err != nil {
			return err
		}
	}

	if err := w.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/nginx/certificates/%d/upload", baseURL, certID), &body)
	if err != nil {
		panic(err)
	}
	// Required headers
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", w.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cert upload failed (%d): %s", resp.StatusCode, b)
	}

	return nil
}

func CreateCert(baseURL, token string, cert Cert) (int, error) {
	//validate first then create it
	if err := handleCertValidate(baseURL+"/api/nginx/certificates/validate", token, cert); err != nil {
		return 0, err
	}

	//send {"nice_name":"aaaa","provider":"other"}
	data := map[string]string{
		"nice_name": cert.Name,
		"provider":  "other",
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}

	resp, err := MakeRequest("POST", baseURL+"/api/nginx/certificates", token, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return 0, fmt.Errorf("create cert failed (%d): %s", resp.StatusCode, respBody)
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

type LetsEncryptCert struct {
	DomainNames []string `json:"domain_names"`
	Meta        struct {
		LetsEncryptEmail       string            `json:"letsencrypt_email"`
		DNSChallenge           bool              `json:"dns_challenge"`
		DNSProvider            string            `json:"dns_provider"`
		DNSProviderCredentials map[string]string `json:"dns_provider_credentials"`
		LetsEncryptAgree       bool              `json:"letsencrypt_agree"`
	} `json:"meta"`
	Provider string `json:"provider"` // should be "letsencrypt"
}

func CreateLetsEncryptCert(baseURL, token string, cert LetsEncryptCert) (int, error) {
	payload, err := json.Marshal(cert)
	if err != nil {
		return 0, err
	}

	resp, err := MakeRequest("POST", baseURL+"/api/nginx/certificates", token, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return 0, fmt.Errorf("create letsencrypt cert failed (%d): %s", resp.StatusCode, respBody)
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