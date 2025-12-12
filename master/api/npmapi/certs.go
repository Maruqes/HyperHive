package npmapi

import (
	"512SvMan/npm"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

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
	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB upper bound
		http.Error(w, "invalid multipart form: "+err.Error(), http.StatusBadRequest)
		return
	}

	certPem, err := readFormFile(r, "certPem", "cert_pem", "certificate")
	if err != nil {
		http.Error(w, "missing certificate: "+err.Error(), http.StatusBadRequest)
		return
	}

	keyPem, err := readFormFile(r, "keyPem", "key_pem", "certificate_key")
	if err != nil {
		http.Error(w, "missing key: "+err.Error(), http.StatusBadRequest)
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
		Name:            r.FormValue("name"),
		CertPem:         certPem,
		KeyPem:          keyPem,
		IntermediateCSR: intermediateCSR,
	}

	loginToken := GetTokenFromContext(r)

	id, createErr := npm.CreateCert(baseURL, loginToken, cert)
	if createErr != nil {
		http.Error(w, "failed to create cert: "+createErr.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id})
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
		http.Error(w, "failed to delete cert: "+err.Error(), http.StatusInternalServerError)
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
