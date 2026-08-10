package npm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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

type Certificate struct {
	ID            int             `json:"id"`
	OwnerUserID   int             `json:"owner_user_id"`
	OwnerTeamID   int             `json:"owner_team_id"`
	NiceName      string          `json:"nice_name"`
	Provider      string          `json:"provider"`
	Status        string          `json:"status"`
	DomainNames   []string        `json:"domain_names"`
	ExpiresOn     string          `json:"expires_on"`
	CreatedOn     string          `json:"created_on"`
	UpdatedOn     string          `json:"updated_on"`
	Meta          map[string]any  `json:"meta"`
	RequestConfig json.RawMessage `json:"request_config"`
	RawCertData   json.RawMessage `json:"certificate"`
}

// UnmarshalJSON retains the legacy UpdatedOn field while accepting NPM's
// actual modified_on field. Marshaling Certificate remains unchanged.
func (c *Certificate) UnmarshalJSON(data []byte) error {
	type certificateAlias Certificate
	var decoded struct {
		certificateAlias
		ModifiedOn string `json:"modified_on"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*c = Certificate(decoded.certificateAlias)
	if decoded.ModifiedOn != "" {
		c.UpdatedOn = decoded.ModifiedOn
	}
	return nil
}

// CertificateDetails is the additive response returned after creating a
// manual certificate.
type CertificateDetails struct {
	ID            int             `json:"id"`
	OwnerUserID   int             `json:"owner_user_id,omitempty"`
	OwnerTeamID   int             `json:"owner_team_id,omitempty"`
	NiceName      string          `json:"nice_name,omitempty"`
	Provider      string          `json:"provider,omitempty"`
	Status        string          `json:"status,omitempty"`
	DomainNames   []string        `json:"domain_names,omitempty"`
	ExpiresOn     string          `json:"expires_on,omitempty"`
	CreatedOn     string          `json:"created_on,omitempty"`
	ModifiedOn    string          `json:"modified_on,omitempty"`
	Meta          map[string]any  `json:"meta,omitempty"`
	RequestConfig json.RawMessage `json:"request_config,omitempty"`
	RawCertData   json.RawMessage `json:"certificate,omitempty"`
}

func handleCertValidate(baseURL, token string, cert Cert) error {
	body, contentType, err := buildCertificateMultipart(cert)
	if err != nil {
		return newCertOperationError("validate", 0, "multipart", err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL, body)
	if err != nil {
		return newCertOperationError("validate", 0, "request", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentType)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return newCertOperationError("validate", 0, "transport", err)
	}
	defer resp.Body.Close()
	// NPM 2.13.5 defines both manual-certificate multipart calls as HTTP 200
	// JSON contracts; other 2xx responses do not prove validation succeeded.
	if resp.StatusCode != http.StatusOK {
		return newCertOperationError("validate", resp.StatusCode, "status", nil)
	}
	var validated struct {
		Certificate    map[string]json.RawMessage `json:"certificate"`
		CertificateKey bool                       `json:"certificate_key"`
	}
	if err := decodeSingleJSON(resp.Body, &validated); err != nil || len(validated.Certificate) == 0 || !validated.CertificateKey {
		return newCertOperationError("validate", resp.StatusCode, "contract", err)
	}
	return nil
}

func certUpload(baseURL, token string, certID int, cert Cert) error {
	body, contentType, err := buildCertificateMultipart(cert)
	if err != nil {
		return newCertOperationError("upload", 0, "multipart", err)
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/nginx/certificates/%d/upload", baseURL, certID), body)
	if err != nil {
		return newCertOperationError("upload", 0, "request", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentType)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return newCertOperationError("upload", 0, "transport", err)
	}
	defer resp.Body.Close()
	// The NPM 2.13.5 upload contract is exactly HTTP 200 plus echoed PEM JSON.
	if resp.StatusCode != http.StatusOK {
		return newCertOperationError("upload", resp.StatusCode, "status", nil)
	}
	var uploaded uploadedCertificatePEM
	if err := decodeSingleJSON(resp.Body, &uploaded); err != nil {
		return newCertOperationError("upload", resp.StatusCode, "contract", err)
	}
	if uploaded.Certificate == nil || uploaded.CertificateKey == nil ||
		!bytes.Equal([]byte(*uploaded.Certificate), cert.CertPem) ||
		!bytes.Equal([]byte(*uploaded.CertificateKey), cert.KeyPem) ||
		!optionalUploadedPEMMatches(uploaded.IntermediateCertificate, cert.IntermediateCSR) {
		return newCertOperationError("upload", resp.StatusCode, "content-mismatch", nil)
	}
	return nil
}

func CreateCert(baseURL, token string, cert Cert) (int, error) {
	created, err := CreateCertDetails(baseURL, token, cert)
	if err != nil {
		return 0, err
	}
	return created.ID, nil
}

// CreateCertDetails creates and uploads a manual certificate and returns the
// certificate metadata reported by NPM after the upload.
func CreateCertDetails(baseURL, token string, cert Cert) (CertificateDetails, error) {
	if strings.TrimSpace(cert.Name) == "" {
		return CertificateDetails{}, newCertValidationError("certificate name is required")
	}
	normalized, leaf, err := normalizeCertificate(cert)
	if err != nil {
		return CertificateDetails{}, err
	}

	if err := handleCertValidate(baseURL+"/api/nginx/certificates/validate", token, normalized); err != nil {
		return CertificateDetails{}, err
	}

	//send {"nice_name":"aaaa","provider":"other"}
	data := map[string]string{
		"nice_name": cert.Name,
		"provider":  "other",
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return CertificateDetails{}, err
	}

	resp, err := MakeRequest("POST", baseURL+"/api/nginx/certificates", token, bytes.NewReader(payload), 30)
	if err != nil {
		return CertificateDetails{}, newCertOperationError("create", 0, "transport", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return CertificateDetails{}, newCertOperationError("create", resp.StatusCode, "status", nil)
	}

	createdID, err := decodeCreatedCertificateID(resp.Body)
	if err != nil {
		responseErr := newCertOperationError("create", resp.StatusCode, "response", err)
		if createdID > 0 {
			return CertificateDetails{}, rollbackCreatedCertificate(baseURL, token, createdID, responseErr)
		}
		return CertificateDetails{}, responseErr
	}
	if createdID <= 0 {
		return CertificateDetails{}, newCertOperationError("create", resp.StatusCode, "response", nil)
	}

	if err := certUpload(baseURL, token, createdID, normalized); err != nil {
		return CertificateDetails{}, rollbackCreatedCertificate(baseURL, token, createdID, err)
	}

	confirmed, err := confirmCertificate(baseURL, token, createdID, leaf.NotAfter)
	if err != nil {
		return CertificateDetails{}, rollbackCreatedCertificate(baseURL, token, createdID, err)
	}
	return newCertificateDetails(confirmed), nil
}

func decodeCreatedCertificateID(r io.Reader) (int, error) {
	decoder := json.NewDecoder(r)
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return 0, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return 0, fmt.Errorf("create response is not an object")
	}

	id := 0
	seenID := false
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return id, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return id, fmt.Errorf("invalid create response field")
		}
		if key != "id" {
			var ignored json.RawMessage
			if err := decoder.Decode(&ignored); err != nil {
				return id, err
			}
			continue
		}
		if seenID {
			return id, fmt.Errorf("duplicate certificate id")
		}
		seenID = true
		var number json.Number
		if err := decoder.Decode(&number); err != nil {
			return id, err
		}
		parsed, err := strconv.ParseInt(string(number), 10, 0)
		if err != nil {
			return id, fmt.Errorf("invalid certificate id")
		}
		id = int(parsed)
	}
	if _, err := decoder.Token(); err != nil {
		return id, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return id, fmt.Errorf("multiple create response values")
		}
		return id, err
	}
	return id, nil
}

func newCertificateDetails(cert Certificate) CertificateDetails {
	return CertificateDetails{
		ID:            cert.ID,
		OwnerUserID:   cert.OwnerUserID,
		OwnerTeamID:   cert.OwnerTeamID,
		NiceName:      cert.NiceName,
		Provider:      cert.Provider,
		Status:        cert.Status,
		DomainNames:   cert.DomainNames,
		ExpiresOn:     cert.ExpiresOn,
		CreatedOn:     cert.CreatedOn,
		ModifiedOn:    cert.UpdatedOn,
		Meta:          cert.Meta,
		RequestConfig: cert.RequestConfig,
		RawCertData:   cert.RawCertData,
	}
}

func getCertificate(baseURL, token string, certID int) (Certificate, error) {
	resp, err := MakeRequest(http.MethodGet, fmt.Sprintf("%s/api/nginx/certificates/%d", baseURL, certID), token, nil, 60)
	if err != nil {
		return Certificate{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Certificate{}, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return Certificate{}, newCertOperationError("confirm", resp.StatusCode, "status", nil)
	}

	var cert Certificate
	if err := json.Unmarshal(body, &cert); err != nil {
		return Certificate{}, newCertOperationError("confirm", resp.StatusCode, "contract", err)
	}
	if cert.ID <= 0 {
		return Certificate{}, newCertOperationError("confirm", resp.StatusCode, "contract", nil)
	}
	return cert, nil
}

type LetsEncryptCert struct {
	DomainNames []string `json:"domain_names"`
	Meta        struct {
		DNSChallenge           bool   `json:"dns_challenge"`
		DNSProvider            string `json:"dns_provider"`
		DNSProviderCredentials string `json:"dns_provider_credentials"`
	} `json:"meta"`
	Provider string `json:"provider"` // "letsencrypt"
}

type npmResp struct {
	ID       int            `json:"id"`
	Error    string         `json:"error"`
	Errors   []string       `json:"errors"`
	Messages []string       `json:"messages"`
	Meta     map[string]any `json:"meta"`
}

func CreateLetsEncryptCert(baseURL, token string, cert LetsEncryptCert) (int, error) {
	payload, err := json.Marshal(cert)
	if err != nil {
		return 0, err
	}

	resp, err := MakeRequest("POST", baseURL+"/api/nginx/certificates", token, bytes.NewReader(payload), 600) //10 mins
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		// tenta extrair mensagem de erro útil do NPM
		var e npmResp
		if json.Unmarshal(respBody, &e) == nil {
			msg := e.Error
			if msg == "" && len(e.Errors) > 0 {
				msg = strings.Join(e.Errors, "; ")
			}
			if msg == "" && len(e.Messages) > 0 {
				msg = strings.Join(e.Messages, "; ")
			}
			if msg != "" {
				return 0, fmt.Errorf("create letsencrypt cert failed (%d): %s", resp.StatusCode, msg)
			}
		}
		return 0, fmt.Errorf("create letsencrypt cert failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var ok npmResp
	if err := json.Unmarshal(respBody, &ok); err == nil && ok.ID != 0 {
		return ok.ID, nil
	}

	// fallback para map[string]any se mudar o schema
	var respData map[string]any
	if err := json.Unmarshal(respBody, &respData); err == nil {
		if d, ok := respData["id"].(float64); ok {
			return int(d), nil
		}
	}
	return 0, fmt.Errorf("cert created but id not found in response: %s", string(respBody))
}

func ListCerts(baseURL, token string) ([]Certificate, error) {
	resp, err := MakeRequest(http.MethodGet, baseURL+"/api/nginx/certificates", token, nil, 60)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("list certs failed (%d): %s", resp.StatusCode, string(body))
	}

	var certs []Certificate
	if err := json.Unmarshal(body, &certs); err != nil {
		return nil, err
	}

	return certs, nil
}

func ListDNSProviders(baseURL, token string) ([]byte, error) {
	resp, err := MakeRequest(http.MethodGet, baseURL+"/api/nginx/certificates/dns-providers", token, nil, 60)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("list dns providers failed (%d): %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func DownloadCert(baseURL, token string, certID int) ([]byte, string, error) {
	url := fmt.Sprintf("%s/api/nginx/certificates/%d/download", baseURL, certID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download cert failed (%d): %s", resp.StatusCode, body)
	}

	return body, resp.Header.Get("Content-Type"), nil
}

func RenewCert(baseURL, token string, certID int) error {
	resp, err := MakeRequest(http.MethodPost, fmt.Sprintf("%s/api/nginx/certificates/%d/renew", baseURL, certID), token, nil, 600)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("renew cert failed (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}

func DeleteCert(baseURL, token string, certID int) error {
	if certID <= 0 {
		return fmt.Errorf("invalid certificate id")
	}

	resp, err := MakeRequest(http.MethodDelete, fmt.Sprintf("%s/api/nginx/certificates/%d", baseURL, certID), token, nil, 60)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("delete cert failed (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// GET /api/nginx/certificates/1/download
// POST /api/nginx/certificates/1/renew
// DELETE /api/nginx/certificates/1
