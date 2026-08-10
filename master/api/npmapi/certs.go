package npmapi

import (
	"512SvMan/npm"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	logger "github.com/Maruqes/512SvMan/logger"
	"github.com/go-chi/chi/v5"
)

const maxManualCertBodyBytes int64 = 32 << 20

func readFormFile(r *http.Request, fieldNames ...string) ([]byte, error) {
	for _, field := range fieldNames {
		file, _, err := r.FormFile(field)
		if err != nil {
			if errors.Is(err, http.ErrMissingFile) {
				continue
			}
			return nil, err
		}
		data, readErr := io.ReadAll(file)
		file.Close()
		if readErr != nil {
			return nil, readErr
		}
		return data, nil
	}
	return nil, http.ErrMissingFile
}

func createCert(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxManualCertBodyBytes)
	if err := r.ParseMultipartForm(maxManualCertBodyBytes); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "multipart form exceeds 32 MiB limit", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	certPem, err := readFormFile(r, "certPem", "cert_pem", "certificate")
	if err != nil {
		http.Error(w, "missing certificate", http.StatusBadRequest)
		return
	}
	if len(certPem) == 0 {
		http.Error(w, "certificate must not be empty", http.StatusBadRequest)
		return
	}

	keyPem, err := readFormFile(r, "keyPem", "key_pem", "certificate_key")
	if err != nil {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	if len(keyPem) == 0 {
		http.Error(w, "key must not be empty", http.StatusBadRequest)
		return
	}

	intermediateCSR, err := readFormFile(r, "intermediateCSR", "intermediate_csr", "intermediate_certificate")
	if err != nil && !errors.Is(err, http.ErrMissingFile) {
		http.Error(w, "invalid intermediate certificate: "+err.Error(), http.StatusBadRequest)
		return
	}
	// Optional field: only treat ErrMissingFile as nil payload
	if errors.Is(err, http.ErrMissingFile) {
		intermediateCSR = nil
	}

	cert := npm.Cert{
		Name:            name,
		CertPem:         certPem,
		KeyPem:          keyPem,
		IntermediateCSR: intermediateCSR,
	}

	loginToken := GetTokenFromContext(r)

	created, createErr := npm.CreateCertDetails(baseURL, loginToken, cert)
	if createErr != nil {
		stage, category, status := npm.CertFailureInfo(createErr)
		logger.Errorf("manual certificate creation failed: stage=%s category=%s status=%d", stage, category, status)
		if errors.Is(createErr, npm.ErrCertValidation) {
			http.Error(w, "invalid certificate upload", http.StatusBadRequest)
			return
		}
		http.Error(w, "failed to create certificate", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(created)
}

func createCertLetsEncrypt(w http.ResponseWriter, r *http.Request) {
	var p npm.LetsEncryptCert
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	p.Provider = "letsencrypt"

	// validações úteis
	if len(p.DomainNames) == 0 {
		http.Error(w, "domain_names is required", http.StatusBadRequest)
		return
	}

	if p.Meta.DNSChallenge && p.Meta.DNSProvider == "" {
		http.Error(w, "meta.dns_provider is required when dns_challenge=true", http.StatusBadRequest)
		return
	}

	// normaliza credenciais por provider
	if p.Meta.DNSChallenge {
		switch strings.ToLower(p.Meta.DNSProvider) {
		case "dynu":
			// Se vieres a receber só o token cru do cliente, transforma aqui:
			// p.Meta.DNSProviderCredentials = fmt.Sprintf("dns_dynu_auth_token = %s", token)
			if !strings.Contains(p.Meta.DNSProviderCredentials, "dns_dynu_auth_token") {
				http.Error(w, "for dynu, meta.dns_provider_credentials must be: 'dns_dynu_auth_token = <TOKEN>'", http.StatusBadRequest)
				return
			}
			// outros providers têm o seu formato específico (Cloudflare, deSEC, etc.)
		}
	}

	loginToken := GetTokenFromContext(r)
	id, err := npm.CreateLetsEncryptCert(baseURL, loginToken, p)
	if err != nil {
		http.Error(w, "failed to create cert: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func listCerts(w http.ResponseWriter, r *http.Request) {
	loginToken := GetTokenFromContext(r)

	certs, err := npm.ListCerts(baseURL, loginToken)
	if err != nil {
		http.Error(w, "failed to list certs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(certs); err != nil {
		http.Error(w, "failed to marshal certs", http.StatusInternalServerError)
		return
	}
}

func listDNSProviders(w http.ResponseWriter, r *http.Request) {
	loginToken := GetTokenFromContext(r)

	body, err := npm.ListDNSProviders(baseURL, loginToken)
	if err != nil {
		http.Error(w, "failed to list dns providers: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

type certIDPayload struct {
	ID int `json:"id"`
}

func downloadCert(w http.ResponseWriter, r *http.Request) {
	var payload certIDPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if payload.ID <= 0 {
		http.Error(w, "invalid certificate id", http.StatusBadRequest)
		return
	}

	loginToken := GetTokenFromContext(r)
	data, contentType, err := npm.DownloadCert(baseURL, loginToken, payload.ID)
	if err != nil {
		http.Error(w, "failed to download cert: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"certificate-%d\"", payload.ID))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func renewCert(w http.ResponseWriter, r *http.Request) {
	var payload certIDPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if payload.ID <= 0 {
		http.Error(w, "invalid certificate id", http.StatusBadRequest)
		return
	}

	loginToken := GetTokenFromContext(r)
	if err := npm.RenewCert(baseURL, loginToken, payload.ID); err != nil {
		http.Error(w, "failed to renew cert: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func deleteCert(w http.ResponseWriter, r *http.Request) {
	var payload certIDPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if payload.ID <= 0 {
		http.Error(w, "invalid certificate id", http.StatusBadRequest)
		return
	}

	loginToken := GetTokenFromContext(r)
	if err := npm.DeleteCert(baseURL, loginToken, payload.ID); err != nil {
		http.Error(w, "failed to delete certificate", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func SetupCertAPI(r chi.Router) chi.Router {
	return r.Route("/certs", func(r chi.Router) {
		r.Get("/dns-providers", listDNSProviders)
		r.Get("/list", listCerts)
		r.Post("/create", createCert)
		r.Post("/create-lets-encrypt", createCertLetsEncrypt)
		r.Post("/download", downloadCert)
		r.Post("/renew", renewCert)
		r.Delete("/delete", deleteCert)
	})
}
