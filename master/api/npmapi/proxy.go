package npmapi

import (
	"512SvMan/npm"
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
)

const managedByKey = "managed_by"
const managedByValue = "setup_frontend"

func listProxies(w http.ResponseWriter, r *http.Request) {
	loginToken := GetTokenFromContext(r)

	proxies, err := npm.GetAllProxys(baseURL, loginToken)
	if err != nil {
		var typed *npm.UpstreamError
		if errors.As(err, &typed) {
			upstreamFailure(w, err)
			return
		}
		http.Error(w, "failed to get proxies: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(proxies); err != nil {
		http.Error(w, "failed to marshal proxies", http.StatusInternalServerError)
		return
	}
}

func createProxy(w http.ResponseWriter, r *http.Request) {
	var p npm.Proxy
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&p); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if len(p.DomainNames) == 0 {
		http.Error(w, "domain_names are required", 400)
		return
	}
	for _, domain := range p.DomainNames {
		if strings.TrimSpace(domain) == "" {
			http.Error(w, "domain_names cannot be empty", 400)
			return
		}
	}
	loginToken := GetTokenFromContext(r)

	if _, err := npm.CreateProxy(baseURL, loginToken, p); err != nil {
		http.Error(w, "failed to create proxy: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func editProxy(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var fields map[string]json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&fields); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	var p npm.Proxy
	raw, _ := json.Marshal(fields)
	if err := json.Unmarshal(raw, &p); err != nil || p.ID < 1 || len(p.DomainNames) == 0 {
		http.Error(w, "valid id and domain_names are required", 400)
		return
	}
	for _, d := range p.DomainNames {
		if strings.TrimSpace(d) == "" {
			http.Error(w, "domain names cannot be empty", 400)
			return
		}
	}
	if value, ok := fields["advanced_config"]; ok && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		http.Error(w, "advanced_config must be a string", 400)
		return
	}
	loginToken := GetTokenFromContext(r)
	proxies, err := npm.GetAllProxys(baseURL, loginToken)
	if err != nil {
		var typed *npm.UpstreamError
		if errors.As(err, &typed) {
			upstreamFailure(w, err)
			return
		}
		http.Error(w, "failed to check proxy: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var existing *npm.Proxy
	for i := range proxies {
		if proxies[i].ID == p.ID {
			existing = &proxies[i]
			break
		}
	}
	if existing == nil {
		http.Error(w, "proxy not found", 404)
		return
	}
	if isManagedProxy(existing) {
		http.Error(w, "managed proxy cannot be edited", http.StatusForbidden)
		return
	}
	mergedRaw, _ := json.Marshal(existing)
	var merged map[string]json.RawMessage
	_ = json.Unmarshal(mergedRaw, &merged)
	for key, value := range fields {
		merged[key] = value
	}
	if selection, ok := fields["upstream_id"]; ok && string(selection) != "null" {
		if _, ok := fields["forward_host"]; !ok {
			merged["forward_host"] = json.RawMessage(`""`)
		}
		if _, ok := fields["forward_port"]; !ok {
			merged["forward_port"] = json.RawMessage(`0`)
		}
	}
	mergedRaw, _ = json.Marshal(merged)
	if err := json.NewDecoder(bytes.NewReader(mergedRaw)).Decode(&p); err != nil {
		http.Error(w, "invalid request body", 400)
		return
	}

	if err := npm.EditProxy(baseURL, loginToken, p); err != nil {
		var typed *npm.UpstreamError
		if errors.As(err, &typed) {
			upstreamFailure(w, err)
			return
		}
		http.Error(w, "failed to edit proxy: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func deleteProxy(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)

	proxies, err := npm.GetAllProxys(baseURL, loginToken)
	if err != nil {
		var typed *npm.UpstreamError
		if errors.As(err, &typed) {
			upstreamFailure(w, err)
			return
		}
		http.Error(w, "failed to check proxy: "+err.Error(), http.StatusInternalServerError)
		return
	}
	for i := range proxies {
		if proxies[i].ID == payload.ID && isManagedProxy(&proxies[i]) {
			http.Error(w, "managed proxy cannot be deleted", http.StatusForbidden)
			return
		}
	}

	if err := npm.DeleteProxy(baseURL, loginToken, payload.ID); err != nil {
		var typed *npm.UpstreamError
		if errors.As(err, &typed) {
			upstreamFailure(w, err)
			return
		}
		http.Error(w, "failed to delete proxy: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func disableProxy(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)

	proxies, err := npm.GetAllProxys(baseURL, loginToken)
	if err != nil {
		var typed *npm.UpstreamError
		if errors.As(err, &typed) {
			upstreamFailure(w, err)
			return
		}
		http.Error(w, "failed to check proxy: "+err.Error(), http.StatusInternalServerError)
		return
	}
	for i := range proxies {
		if proxies[i].ID == payload.ID && isManagedProxy(&proxies[i]) {
			http.Error(w, "managed proxy cannot be disabled", http.StatusForbidden)
			return
		}
	}

	if err := npm.DisableProxy(baseURL, loginToken, payload.ID); err != nil {
		var typed *npm.UpstreamError
		if errors.As(err, &typed) {
			upstreamFailure(w, err)
			return
		}
		http.Error(w, "failed to disable proxy: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func enableProxy(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)

	if err := npm.EnableProxy(baseURL, loginToken, payload.ID); err != nil {
		var typed *npm.UpstreamError
		if errors.As(err, &typed) {
			upstreamFailure(w, err)
			return
		}
		http.Error(w, "failed to enable proxy: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func baseFrontendProxy() npm.Proxy {
	return npm.Proxy{
		ForwardScheme:         "http",
		ForwardHost:           "127.0.0.1",
		ForwardPort:           8079,
		AllowWebsocketUpgrade: true,
		CachingEnabled:        false,
		BlockExploits:         false,
		AccessListID:          "0",
		CertificateID:         0,
		Meta:                  map[string]any{managedByKey: managedByValue},
		Http2Support:          false,
		HstsEnabled:           false,
		HstsSubdomains:        false,
		SslForced:             true,
		Enabled:               true,
		AdvancedConfig: `
# --- TIMEOUTS ALTOS (NA PRÁTICA, QUASE ILIMITADOS) ---
proxy_connect_timeout 36000s;
proxy_send_timeout 36000s;
proxy_read_timeout 36000s;
send_timeout 36000s;

# --- SEM LIMITES DE TAMANHO ---
client_max_body_size 0;

# --- SEM BUFFERING (STREAMS / LOGS / LONG POLLING) ---
proxy_buffering off;
proxy_request_buffering off;

# Do not forward client-controlled IP headers to the API.
proxy_set_header X-Real-IP $remote_addr;
proxy_set_header X-Forwarded-For $remote_addr;
`,
	}
}

func applyCertificate(proxy *npm.Proxy, certID int) {
	proxy.CertificateID = certID
	proxy.SslForced = certID > 0
}

func findProxyByDomain(baseURL, token, domain string) (*npm.Proxy, error) {
	proxies, err := npm.GetAllProxys(baseURL, token)
	if err != nil {
		return nil, err
	}
	for i := range proxies {
		if slices.Contains(proxies[i].DomainNames, domain) {
			return &proxies[i], nil
		}
	}
	return nil, nil
}

func createOrUpdateProxy(baseURL, token string, proxy npm.Proxy) error {
	existing, err := findProxyByDomain(baseURL, token, proxy.DomainNames[0])
	if err != nil {
		return err
	}
	if existing != nil {
		proxy.ID = existing.ID
		return npm.EditProxy(baseURL, token, proxy)
	}
	_, err = npm.CreateProxy(baseURL, token, proxy)
	return err
}

func hasPath(proxy *npm.Proxy, path string) bool {
	for _, loc := range proxy.Locations {
		if loc.Path == path {
			return true
		}
	}
	return false
}

func findManagedProxyIDs(baseURL, token string) (privateID, publicID int, err error) {
	proxies, err := npm.GetAllProxys(baseURL, token)
	if err != nil {
		return 0, 0, err
	}
	for i := range proxies {
		if !isManagedProxy(&proxies[i]) {
			continue
		}
		switch {
		case hasPath(&proxies[i], "/api"):
			privateID = proxies[i].ID
		case hasPath(&proxies[i], "/guest_api"):
			publicID = proxies[i].ID
		}
	}
	return privateID, publicID, nil
}

func setupFrontEnd(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Domain        string `json:"domain"`
		CertificateId int    `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)
	locations := []npm.Location{
		{Path: "/api", ForwardScheme: "http", ForwardHost: "127.0.0.1", ForwardPort: apiPort},
		{Path: "/novnc", ForwardScheme: "http", ForwardHost: "127.0.0.1", ForwardPort: apiPort},
		{Path: "/guest_api", ForwardScheme: "http", ForwardHost: "127.0.0.1", ForwardPort: apiPort},
	}

	proxy := baseFrontendProxy()
	proxy.DomainNames = []string{payload.Domain}
	proxy.Locations = locations
	applyCertificate(&proxy, payload.CertificateId)

	if err := createOrUpdateProxy(baseURL, loginToken, proxy); err != nil {
		http.Error(w, "failed to setup frontend: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func setupFrontEndSecure(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		PublicDomain         string `json:"publicDomain"`
		PublicCertificateId  int    `json:"publicCertificateId"`
		PrivateDomain        string `json:"privateDomain"`
		PrivateCertificateId int    `json:"privateCertificateId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if payload.PublicDomain == "" || payload.PrivateDomain == "" {
		http.Error(w, "public and private domains are required", http.StatusBadRequest)
		return
	}

	loginToken := GetTokenFromContext(r)

	existingPrivateID, existingPublicID, err := findManagedProxyIDs(baseURL, loginToken)
	if err != nil {
		var typed *npm.UpstreamError
		if errors.As(err, &typed) {
			upstreamFailure(w, err)
			return
		}
		http.Error(w, "failed to check existing frontend: "+err.Error(), http.StatusInternalServerError)
		return
	}

	privateLocations := []npm.Location{
		{Path: "/api", ForwardScheme: "http", ForwardHost: "127.0.0.1", ForwardPort: apiPort},
		{Path: "/novnc", ForwardScheme: "http", ForwardHost: "127.0.0.1", ForwardPort: apiPort},
		{Path: "/guest_api", ForwardScheme: "http", ForwardHost: "127.0.0.1", ForwardPort: apiPort},
	}
	publicLocations := []npm.Location{
		{Path: "/guest_api", ForwardScheme: "http", ForwardHost: "127.0.0.1", ForwardPort: apiPort},
	}

	privateProxy := baseFrontendProxy()
	privateProxy.DomainNames = []string{payload.PrivateDomain}
	privateProxy.Locations = privateLocations
	applyCertificate(&privateProxy, payload.PrivateCertificateId)

	publicProxy := baseFrontendProxy()
	publicProxy.DomainNames = []string{payload.PublicDomain}
	publicProxy.Locations = publicLocations
	publicProxy.AdvancedConfig += `
# --- BLOQUEAR TUDO EXCETO /guest_api ---
location / {
    return 403;
}
`
	applyCertificate(&publicProxy, payload.PublicCertificateId)

	if err := saveFrontendProxy(baseURL, loginToken, privateProxy, existingPrivateID); err != nil {
		http.Error(w, "failed to setup private frontend: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := saveFrontendProxy(baseURL, loginToken, publicProxy, existingPublicID); err != nil {
		http.Error(w, "failed to setup public frontend: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func saveFrontendProxy(baseURL, token string, proxy npm.Proxy, existingID int) error {
	if existingID > 0 {
		proxy.ID = existingID
		return npm.EditProxy(baseURL, token, proxy)
	}
	return createOrUpdateProxy(baseURL, token, proxy)
}

func isManagedProxy(proxy *npm.Proxy) bool {
	if proxy == nil || proxy.Meta == nil {
		return false
	}
	v, ok := proxy.Meta[managedByKey].(string)
	return ok && v == managedByValue
}

func SetupProxyAPI(r chi.Router) chi.Router {
	return r.Route("/proxy", func(r chi.Router) {
		r.Get("/list", listProxies)
		r.Post("/create", createProxy)
		r.Put("/edit", editProxy)
		r.Delete("/delete", deleteProxy)
		r.Post("/disable", disableProxy)
		r.Post("/enable", enableProxy)

		r.Post("/setupFrontEnd", setupFrontEnd)
		r.Post("/setupFrontEndSecure", setupFrontEndSecure)
	})
}
