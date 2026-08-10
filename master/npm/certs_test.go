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
