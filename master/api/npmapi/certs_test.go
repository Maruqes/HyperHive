package npmapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func handlerPEM(t *testing.T) (cert, key []byte, expires string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	notAfter := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "handler.test"}, DNSNames: []string{"handler.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}), notAfter.Format("2006-01-02 15:04:05")
}

func handlerNPMServer(t *testing.T, cert, key []byte, expires string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/validate"):
			fmt.Fprint(w, `{"certificate":{"cn":"handler.test"},"certificate_key":true}`)
		case r.URL.Path == "/api/nginx/certificates" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"id":31}`)
		case strings.HasSuffix(r.URL.Path, "/upload"):
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatal(err)
			}
			read := func(field string) string {
				f, _, err := r.FormFile(field)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				var b bytes.Buffer
				b.ReadFrom(f)
				return b.String()
			}
			json.NewEncoder(w).Encode(map[string]string{"certificate": read("certificate"), "certificate_key": read("certificate_key")})
		case r.URL.Path == "/api/nginx/certificates/31" && r.Method == http.MethodGet:
			fmt.Fprintf(w, `{"id":31,"domain_names":["handler.test"],"created_on":"2026-01-01 00:00:00","expires_on":%q}`, expires)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestCreateCertReturnsDetailsAndRetainsAliases(t *testing.T) {
	cert, key, expires := handlerPEM(t)
	server := handlerNPMServer(t, cert, key, expires)
	defer server.Close()
	old := baseURL
	baseURL = server.URL
	defer func() { baseURL = old }()
	aliases := []struct{ cert, key, chain string }{{"certPem", "keyPem", "intermediateCSR"}, {"cert_pem", "key_pem", "intermediate_csr"}, {"certificate", "certificate_key", "intermediate_certificate"}}
	for _, fields := range aliases {
		t.Run(fields.cert, func(t *testing.T) {
			req := newCertRequest(t, true, " manual ", fields.cert, cert, fields.key, key, "", nil).WithContext(context.WithValue(context.Background(), "token", "handler-token"))
			response := httptest.NewRecorder()
			createCert(response, req)
			if response.Code != 200 {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
			var got struct {
				ID          int      `json:"id"`
				DomainNames []string `json:"domain_names"`
				ExpiresOn   string   `json:"expires_on"`
			}
			if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.ID != 31 || len(got.DomainNames) != 1 || got.ExpiresOn != expires {
				t.Fatalf("response=%#v", got)
			}
		})
	}
}

func TestCreateCertLocalValidationIsSanitized400(t *testing.T) {
	_, key, _ := handlerPEM(t)
	req := newCertRequest(t, true, "manual", "certPem", []byte("NOT_A_CERT_SECRET"), "keyPem", key, "", nil)
	response := httptest.NewRecorder()
	createCert(response, req)
	if response.Code != http.StatusBadRequest || strings.TrimSpace(response.Body.String()) != "invalid certificate upload" || strings.Contains(response.Body.String(), "SECRET") {
		t.Fatalf("response=(%d,%q)", response.Code, response.Body.String())
	}
}

func TestCreateCertUpstreamFailureIsSanitized500(t *testing.T) {
	cert, key, _ := handlerPEM(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/validate") {
			fmt.Fprint(w, `{"certificate":{"cn":"handler.test"},"certificate_key":true}`)
			return
		}
		http.Error(w, "RAW_UPSTREAM_PRIVATE_DETAIL", http.StatusBadGateway)
	}))
	defer server.Close()
	old := baseURL
	baseURL = server.URL
	defer func() { baseURL = old }()
	req := newCertRequest(t, true, "manual", "certPem", cert, "keyPem", key, "", nil)
	response := httptest.NewRecorder()
	createCert(response, req)
	if response.Code != 500 || strings.TrimSpace(response.Body.String()) != "failed to create certificate" || strings.Contains(response.Body.String(), "RAW_") {
		t.Fatalf("response=(%d,%q)", response.Code, response.Body.String())
	}
}

func TestCreateCertRequestValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		req    *http.Request
		status int
	}{
		{"malformed", func() *http.Request {
			r := httptest.NewRequest("POST", "/api/certs/create", strings.NewReader("bad"))
			r.Header.Set("Content-Type", "multipart/form-data; boundary=broken")
			return r
		}(), 400},
		{"missing name", newCertRequest(t, false, "", "certPem", []byte("x"), "keyPem", []byte("y"), "", nil), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			createCert(w, tc.req)
			if w.Code != tc.status {
				t.Fatalf("status=%d", w.Code)
			}
		})
	}
}

func TestCreateCertBoundsRequestBody(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("name", "cert")
	part, _ := writer.CreateFormFile("certPem", "cert.pem")
	_, _ = part.Write(bytes.Repeat([]byte("x"), int(maxManualCertBodyBytes)))
	_ = writer.Close()
	req := httptest.NewRequest("POST", "/api/certs/create", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	createCert(response, req)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestDeleteCertRouteBehavior(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/api/certs/delete", strings.NewReader(`{"id":0}`))
	response := httptest.NewRecorder()
	deleteCert(response, req)
	if response.Code != 400 {
		t.Fatalf("status=%d", response.Code)
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
	write := func(field, name string, data []byte) {
		if field == "" {
			return
		}
		part, err := writer.CreateFormFile(field, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	write(certField, "cert.pem", cert)
	write(keyField, "key.pem", key)
	write(intermediateField, "chain.pem", intermediate)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/certs/create", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}
