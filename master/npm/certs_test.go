package npm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestCreateCertValidatesCreatesAndUploads(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("Authorization = %q", got)
		}

		switch r.URL.Path {
		case "/api/nginx/certificates/validate", "/api/nginx/certificates/42/upload":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("parse multipart: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assertMultipartFile(t, r, "certificate", "certificate data")
			assertMultipartFile(t, r, "certificate_key", "key data")
			assertMultipartFile(t, r, "intermediate_certificate", "chain data")
			if r.URL.Path == "/api/nginx/certificates/42/upload" {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"id":42,"nice_name":"manual cert","provider":"other","domain_names":["example.com"],"expires_on":"2035-01-02 03:04:05","created_on":"2026-08-10 12:00:00","modified_on":"2026-08-10 12:01:00"}`)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/api/nginx/certificates":
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode create payload: %v", err)
			}
			if payload["nice_name"] != "manual cert" || payload["provider"] != "other" {
				t.Errorf("unexpected create payload: %#v", payload)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":42}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	id, err := CreateCert(server.URL, "token", Cert{
		Name:            "manual cert",
		CertPem:         []byte("certificate data"),
		KeyPem:          []byte("key data"),
		IntermediateCSR: []byte("chain data"),
	})
	if err != nil {
		t.Fatalf("CreateCert() error = %v", err)
	}
	if id != 42 {
		t.Fatalf("CreateCert() id = %d, want 42", id)
	}

	want := []string{
		"POST /api/nginx/certificates/validate",
		"POST /api/nginx/certificates",
		"POST /api/nginx/certificates/42/upload",
	}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("request sequence = %#v, want %#v", requests, want)
	}
}

func TestCreateCertDetailsReturnsUploadCertificate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/nginx/certificates/validate":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/nginx/certificates" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"id":21}`)
		case r.URL.Path == "/api/nginx/certificates/21/upload":
			fmt.Fprint(w, `{"id":21,"nice_name":"uploaded cert","domain_names":["one.example","two.example"],"expires_on":"2035-01-02 03:04:05","modified_on":"2026-08-10 12:01:00"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	created, err := CreateCertDetails(server.URL, "token", Cert{Name: "cert", CertPem: []byte("cert"), KeyPem: []byte("key")})
	if err != nil {
		t.Fatalf("CreateCertDetails() error = %v", err)
	}
	if created.ID != 21 || created.ExpiresOn != "2035-01-02 03:04:05" || !reflect.DeepEqual(created.DomainNames, []string{"one.example", "two.example"}) {
		t.Fatalf("CreateCertDetails() = %#v, want upload metadata", created)
	}
	if created.ModifiedOn != "2026-08-10 12:01:00" {
		t.Errorf("ModifiedOn = %q", created.ModifiedOn)
	}
}

func TestCreateCertDetailsEnrichesEmptyUploadResponse(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/api/nginx/certificates/validate":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/nginx/certificates" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"id":22}`)
		case r.URL.Path == "/api/nginx/certificates/22/upload":
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/nginx/certificates/22" && r.Method == http.MethodGet:
			fmt.Fprint(w, `{"id":22,"domain_names":["enriched.example"],"expires_on":"2036-02-03 04:05:06"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	created, err := CreateCertDetails(server.URL, "token", Cert{Name: "cert", CertPem: []byte("cert"), KeyPem: []byte("key")})
	if err != nil {
		t.Fatalf("CreateCertDetails() error = %v", err)
	}
	if created.ID != 22 || created.ExpiresOn != "2036-02-03 04:05:06" || !reflect.DeepEqual(created.DomainNames, []string{"enriched.example"}) {
		t.Fatalf("CreateCertDetails() = %#v, want enriched metadata", created)
	}
	want := []string{
		"POST /api/nginx/certificates/validate",
		"POST /api/nginx/certificates",
		"POST /api/nginx/certificates/22/upload",
		"GET /api/nginx/certificates/22",
	}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("request sequence = %#v, want %#v", requests, want)
	}
}

func TestCreateCertDetailsEnrichmentFailureReturnsIDWithoutRollback(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/api/nginx/certificates/validate":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/nginx/certificates" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"id":23}`)
		case r.URL.Path == "/api/nginx/certificates/23/upload":
			fmt.Fprint(w, `{}`)
		case r.URL.Path == "/api/nginx/certificates/23" && r.Method == http.MethodGet:
			http.Error(w, "not ready", http.StatusBadGateway)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	created, err := CreateCertDetails(server.URL, "token", Cert{Name: "cert", CertPem: []byte("cert"), KeyPem: []byte("key")})
	if err != nil {
		t.Fatalf("CreateCertDetails() error = %v", err)
	}
	if created.ID != 23 || created.ExpiresOn != "" || len(created.DomainNames) != 0 {
		t.Fatalf("CreateCertDetails() = %#v, want ID-only fallback", created)
	}
	want := []string{
		"POST /api/nginx/certificates/validate",
		"POST /api/nginx/certificates",
		"POST /api/nginx/certificates/23/upload",
		"GET /api/nginx/certificates/23",
	}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("request sequence = %#v, want %#v", requests, want)
	}
}

func TestCreateCertDetailsRejectsMismatchedCertificateIDs(t *testing.T) {
	for _, test := range []struct {
		name          string
		uploadBody    string
		getBody       string
		wantExpiresOn string
	}{
		{
			name:          "mismatched upload uses matching enrichment",
			uploadBody:    `{"id":999,"domain_names":["wrong-upload.example"],"expires_on":"2099-01-01 00:00:00"}`,
			getBody:       `{"id":25,"domain_names":["correct.example"],"expires_on":"2038-04-05 06:07:08"}`,
			wantExpiresOn: "2038-04-05 06:07:08",
		},
		{
			name:       "mismatched get falls back to created id",
			uploadBody: `{}`,
			getBody:    `{"id":998,"domain_names":["wrong-get.example"],"expires_on":"2099-01-01 00:00:00"}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				switch {
				case r.URL.Path == "/api/nginx/certificates/validate":
					w.WriteHeader(http.StatusOK)
				case r.URL.Path == "/api/nginx/certificates" && r.Method == http.MethodPost:
					fmt.Fprint(w, `{"id":25}`)
				case r.URL.Path == "/api/nginx/certificates/25/upload":
					fmt.Fprint(w, test.uploadBody)
				case r.URL.Path == "/api/nginx/certificates/25" && r.Method == http.MethodGet:
					fmt.Fprint(w, test.getBody)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			created, err := CreateCertDetails(server.URL, "token", Cert{Name: "cert", CertPem: []byte("cert"), KeyPem: []byte("key")})
			if err != nil {
				t.Fatalf("CreateCertDetails() error = %v", err)
			}
			if created.ID != 25 || created.ExpiresOn != test.wantExpiresOn {
				t.Fatalf("CreateCertDetails() = %#v, want id 25 and expires_on %q", created, test.wantExpiresOn)
			}
			if len(created.DomainNames) > 0 && created.DomainNames[0] != "correct.example" {
				t.Fatalf("CreateCertDetails() returned unrelated metadata: %#v", created)
			}
			want := []string{
				"POST /api/nginx/certificates/validate",
				"POST /api/nginx/certificates",
				"POST /api/nginx/certificates/25/upload",
				"GET /api/nginx/certificates/25",
			}
			if !reflect.DeepEqual(requests, want) {
				t.Fatalf("request sequence = %#v, want %#v", requests, want)
			}
		})
	}
}

func TestCreateCertDetailsUploadBodyReadFailureUsesEnrichment(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/api/nginx/certificates/validate":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/nginx/certificates" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"id":26}`)
		case r.URL.Path == "/api/nginx/certificates/26/upload":
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("response writer does not support hijacking")
				return
			}
			conn, rw, err := hijacker.Hijack()
			if err != nil {
				t.Errorf("hijack upload response: %v", err)
				return
			}
			fmt.Fprint(rw, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 100\r\nConnection: close\r\n\r\n{\"id\":26")
			if err := rw.Flush(); err != nil {
				t.Errorf("flush partial upload response: %v", err)
			}
			conn.Close()
		case r.URL.Path == "/api/nginx/certificates/26" && r.Method == http.MethodGet:
			fmt.Fprint(w, `{"id":26,"domain_names":["read-failure.example"],"expires_on":"2039-05-06 07:08:09"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	created, err := CreateCertDetails(server.URL, "token", Cert{Name: "cert", CertPem: []byte("cert"), KeyPem: []byte("key")})
	if err != nil {
		t.Fatalf("CreateCertDetails() error = %v", err)
	}
	if created.ID != 26 || created.ExpiresOn != "2039-05-06 07:08:09" {
		t.Fatalf("CreateCertDetails() = %#v, want matching enrichment", created)
	}
	want := []string{
		"POST /api/nginx/certificates/validate",
		"POST /api/nginx/certificates",
		"POST /api/nginx/certificates/26/upload",
		"GET /api/nginx/certificates/26",
	}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("request sequence = %#v, want %#v", requests, want)
	}
}

func TestCertificateModifiedOnCompatibility(t *testing.T) {
	var cert Certificate
	if err := json.Unmarshal([]byte(`{"id":27,"modified_on":"2040-06-07 08:09:10"}`), &cert); err != nil {
		t.Fatalf("unmarshal Certificate: %v", err)
	}
	if cert.UpdatedOn != "2040-06-07 08:09:10" {
		t.Fatalf("UpdatedOn = %q, want modified_on value", cert.UpdatedOn)
	}
	var legacy Certificate
	if err := json.Unmarshal([]byte(`{"id":27,"updated_on":"legacy-value"}`), &legacy); err != nil {
		t.Fatalf("unmarshal legacy Certificate: %v", err)
	}
	if legacy.UpdatedOn != "legacy-value" {
		t.Fatalf("legacy UpdatedOn = %q", legacy.UpdatedOn)
	}

	body, err := json.Marshal(cert)
	if err != nil {
		t.Fatalf("marshal Certificate: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("decode marshaled Certificate: %v", err)
	}
	if _, ok := fields["modified_on"]; ok {
		t.Fatalf("legacy Certificate unexpectedly changed JSON shape: %s", body)
	}
	if got := string(fields["updated_on"]); got != `"2040-06-07 08:09:10"` {
		t.Fatalf("updated_on = %s, want compatibility value", got)
	}
	if _, ok := fields["expires_on"]; !ok {
		t.Fatalf("legacy Certificate omitted pre-existing field: %s", body)
	}

	details := newCertificateDetails(cert)
	detailBody, err := json.Marshal(details)
	if err != nil {
		t.Fatalf("marshal CertificateDetails: %v", err)
	}
	var detailFields map[string]json.RawMessage
	if err := json.Unmarshal(detailBody, &detailFields); err != nil {
		t.Fatalf("decode CertificateDetails: %v", err)
	}
	if got := string(detailFields["modified_on"]); got != `"2040-06-07 08:09:10"` {
		t.Fatalf("detail modified_on = %s, want NPM field", got)
	}
	if _, ok := detailFields["expires_on"]; ok {
		t.Fatalf("detail response fabricated expires_on: %s", detailBody)
	}
}

func TestCreateCertRollsBackFailedUpload(t *testing.T) {
	for _, test := range []struct {
		name           string
		rollbackStatus int
		wantError      []string
	}{
		{name: "rollback succeeds", rollbackStatus: http.StatusNoContent, wantError: []string{"upload certificate", "upload rejected", "rolled back"}},
		{name: "rollback fails", rollbackStatus: http.StatusConflict, wantError: []string{"upload certificate", "upload rejected", "rollback certificate 7", "rollback rejected"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				switch r.URL.Path {
				case "/api/nginx/certificates/validate":
					w.WriteHeader(http.StatusOK)
				case "/api/nginx/certificates":
					fmt.Fprint(w, `{"id":7}`)
				case "/api/nginx/certificates/7/upload":
					http.Error(w, "upload rejected", http.StatusBadRequest)
				case "/api/nginx/certificates/7":
					w.WriteHeader(test.rollbackStatus)
					if test.rollbackStatus != http.StatusNoContent {
						fmt.Fprint(w, "rollback rejected")
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			id, err := CreateCert(server.URL, "token", Cert{Name: "cert", CertPem: []byte("cert"), KeyPem: []byte("key")})
			if err == nil {
				t.Fatal("CreateCert() error = nil")
			}
			if id != 0 {
				t.Errorf("CreateCert() id = %d, want 0", id)
			}
			for _, text := range test.wantError {
				if !strings.Contains(err.Error(), text) {
					t.Errorf("error %q does not contain %q", err, text)
				}
			}

			want := []string{
				"POST /api/nginx/certificates/validate",
				"POST /api/nginx/certificates",
				"POST /api/nginx/certificates/7/upload",
				"DELETE /api/nginx/certificates/7",
			}
			if !reflect.DeepEqual(requests, want) {
				t.Fatalf("request sequence = %#v, want %#v", requests, want)
			}
		})
	}
}

func TestCreateCertRejectsInvalidCreateID(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/api/nginx/certificates/validate" {
			w.WriteHeader(http.StatusOK)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()

	id, err := CreateCert(server.URL, "", Cert{Name: "cert", CertPem: []byte("cert"), KeyPem: []byte("key")})
	if err == nil || !strings.Contains(err.Error(), "invalid certificate id") {
		t.Fatalf("CreateCert() error = %v", err)
	}
	if id != 0 || requests != 2 {
		t.Fatalf("CreateCert() = (%d, %v), requests = %d; want id 0 and two requests", id, err, requests)
	}
}

func TestDeleteCertRejectsNonPositiveID(t *testing.T) {
	if err := DeleteCert("http://unused", "", 0); err == nil {
		t.Fatal("DeleteCert() error = nil")
	}
}

func TestDeleteCertRetainsUpstreamErrorForCaller(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream delete detail", http.StatusConflict)
	}))
	defer server.Close()

	err := DeleteCert(server.URL, "token", 8)
	if err == nil || !strings.Contains(err.Error(), "upstream delete detail") {
		t.Fatalf("DeleteCert() error = %v, want detailed upstream error", err)
	}
}

func assertMultipartFile(t *testing.T, r *http.Request, field, want string) {
	t.Helper()
	file, _, err := r.FormFile(field)
	if err != nil {
		t.Errorf("FormFile(%q): %v", field, err)
		return
	}
	defer file.Close()

	data := make([]byte, len(want))
	if _, err := file.Read(data); err != nil {
		t.Errorf("read %q: %v", field, err)
		return
	}
	if string(data) != want {
		t.Errorf("multipart field %q = %q, want %q", field, data, want)
	}
}
