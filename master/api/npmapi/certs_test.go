package npmapi

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateCertRequiresNameAndNonemptyFiles(t *testing.T) {
	for _, test := range []struct {
		name        string
		includeName bool
		certName    string
		certField   string
		cert        []byte
		keyField    string
		key         []byte
		wantBody    string
	}{
		{name: "absent name", includeName: false, certField: "certPem", cert: []byte("cert"), keyField: "keyPem", key: []byte("key"), wantBody: "name is required"},
		{name: "blank name", includeName: true, certName: "  ", certField: "certPem", cert: []byte("cert"), keyField: "keyPem", key: []byte("key"), wantBody: "name is required"},
		{name: "absent certificate", includeName: true, certName: "cert", keyField: "keyPem", key: []byte("key"), wantBody: "missing certificate"},
		{name: "empty certificate", includeName: true, certName: "cert", certField: "certPem", cert: []byte{}, keyField: "keyPem", key: []byte("key"), wantBody: "certificate must not be empty"},
		{name: "absent key", includeName: true, certName: "cert", certField: "certPem", cert: []byte("cert"), wantBody: "missing key"},
		{name: "empty key", includeName: true, certName: "cert", certField: "certPem", cert: []byte("cert"), keyField: "keyPem", key: []byte{}, wantBody: "key must not be empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := newCertRequest(t, test.includeName, test.certName, test.certField, test.cert, test.keyField, test.key, "", nil)
			response := httptest.NewRecorder()

			createCert(response, req)

			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), test.wantBody) {
				t.Fatalf("response = (%d, %q), want status %d containing %q", response.Code, response.Body.String(), http.StatusBadRequest, test.wantBody)
			}
		})
	}
}

func TestCreateCertRejectsMalformedMultipart(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/certs/create", strings.NewReader("not multipart data"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=broken")
	response := httptest.NewRecorder()

	createCert(response, req)

	if response.Code != http.StatusBadRequest || strings.TrimSpace(response.Body.String()) != "invalid multipart form" {
		t.Fatalf("response = (%d, %q), want 400 invalid multipart form", response.Code, response.Body.String())
	}
}

func TestCreateCertRetainsMultipartAliases(t *testing.T) {
	originalBaseURL := baseURL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/nginx/certificates/validate":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/nginx/certificates" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"id":12}`)
		case r.URL.Path == "/api/nginx/certificates/12/upload":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL = server.URL
	defer func() { baseURL = originalBaseURL }()

	aliases := []struct {
		cert         string
		key          string
		intermediate string
	}{
		{cert: "certPem", key: "keyPem", intermediate: "intermediateCSR"},
		{cert: "cert_pem", key: "key_pem", intermediate: "intermediate_csr"},
		{cert: "certificate", key: "certificate_key", intermediate: "intermediate_certificate"},
	}
	for _, fields := range aliases {
		t.Run(fields.cert, func(t *testing.T) {
			req := newCertRequest(t, true, " manual cert ", fields.cert, []byte("cert"), fields.key, []byte("key"), fields.intermediate, []byte("chain"))
			response := httptest.NewRecorder()

			createCert(response, req)

			if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"id":12}` {
				t.Fatalf("response = (%d, %q), want 200 JSON id", response.Code, response.Body.String())
			}
		})
	}
}

func TestCreateCertAllowsOmittedIntermediateAndForwardsToken(t *testing.T) {
	originalBaseURL := baseURL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer handler-token" {
			t.Errorf("Authorization = %q, want Bearer handler-token", got)
		}
		switch {
		case r.URL.Path == "/api/nginx/certificates/validate":
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/nginx/certificates" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"id":13}`)
		case r.URL.Path == "/api/nginx/certificates/13/upload":
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL = server.URL
	defer func() { baseURL = originalBaseURL }()

	req := newCertRequest(t, true, "manual cert", "certPem", []byte("cert"), "keyPem", []byte("key"), "", nil)
	req = req.WithContext(context.WithValue(req.Context(), "token", "handler-token"))
	response := httptest.NewRecorder()

	createCert(response, req)

	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"id":13}` {
		t.Fatalf("response = (%d, %q), want 200 JSON id", response.Code, response.Body.String())
	}
}

func TestManualCertHandlersDoNotExposeUpstreamErrors(t *testing.T) {
	originalBaseURL := baseURL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "RAW_UPSTREAM_PRIVATE_DETAIL", http.StatusBadGateway)
	}))
	defer server.Close()
	baseURL = server.URL
	defer func() { baseURL = originalBaseURL }()

	t.Run("create", func(t *testing.T) {
		req := newCertRequest(t, true, "manual cert", "certPem", []byte("cert"), "keyPem", []byte("key"), "", nil)
		response := httptest.NewRecorder()
		createCert(response, req)

		if response.Code != http.StatusInternalServerError || strings.TrimSpace(response.Body.String()) != "failed to create certificate" {
			t.Fatalf("response = (%d, %q), want stable create error", response.Code, response.Body.String())
		}
	})

	t.Run("delete", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/certs/delete", strings.NewReader(`{"id":9}`))
		response := httptest.NewRecorder()
		deleteCert(response, req)

		if response.Code != http.StatusInternalServerError || strings.TrimSpace(response.Body.String()) != "failed to delete certificate" {
			t.Fatalf("response = (%d, %q), want stable delete error", response.Code, response.Body.String())
		}
	})
}

func TestCreateCertBoundsRequestBody(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("name", "cert"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("certPem", "cert.pem")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("x"), int(maxManualCertBodyBytes))); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/certs/create", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	createCert(response, req)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestDeleteCertRejectsNonPositiveID(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/api/certs/delete", strings.NewReader(`{"id":0}`))
	response := httptest.NewRecorder()

	deleteCert(response, req)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func newCertRequest(t *testing.T, includeName bool, name, certField string, cert []byte, keyField string, key []byte, intermediateField string, intermediate []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if includeName {
		if err := writer.WriteField("name", name); err != nil {
			t.Fatal(err)
		}
	}
	writeFile := func(field, filename string, data []byte) {
		if field == "" {
			return
		}
		part, err := writer.CreateFormFile(field, filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(certField, "cert.pem", cert)
	writeFile(keyField, "key.pem", key)
	if intermediateField != "" {
		writeFile(intermediateField, "chain.pem", intermediate)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/certs/create", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}
