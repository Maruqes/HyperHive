package npmapi

import (
	"512SvMan/npm"
	"encoding/json"
	"errors"
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
	if p.Meta.LetsEncryptEmail == "" || !p.Meta.LetsEncryptAgree {
		http.Error(w, "meta.letsencrypt_email and meta.letsencrypt_agree=true are required", http.StatusBadRequest)
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

func SetupCertAPI(r chi.Router) chi.Router {
	return r.Route("/certs", func(r chi.Router) {
		r.Post("/create", createCert)
		r.Post("/create-lets-encrypt", createCertLetsEncrypt)
	})
}
