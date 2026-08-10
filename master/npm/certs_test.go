package npm

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type certFixture struct {
	leaf, chain, key []byte
	leafCert         *x509.Certificate
	keyRSA           *rsa.PrivateKey
}

func newCertFixture(t *testing.T, notBefore, notAfter time.Time) certFixture {
	t.Helper()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test CA"}, NotBefore: notBefore.Add(-time.Hour), NotAfter: notAfter.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "example.test"}, DNSNames: []string{"example.test"}, NotBefore: notBefore, NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatal(err)
	}
	return certFixture{
		leaf:     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		chain:    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		key:      pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)}),
		leafCert: leaf, keyRSA: leafKey,
	}
}

func validFixture(t *testing.T) certFixture {
	return newCertFixture(t, time.Now().Add(-time.Hour), time.Now().Add(48*time.Hour).Truncate(time.Second))
}

func TestNormalizeCertificateLeafChainAndDedup(t *testing.T) {
	f := validFixture(t)
	for _, test := range []struct {
		name        string
		cert, chain []byte
		wantChain   bool
	}{
		{"leaf only", f.leaf, nil, false},
		{"leaf and chain", f.leaf, f.chain, true},
		{"fullchain and duplicate separate chain", append(append([]byte{}, f.leaf...), f.chain...), f.chain, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			n, leaf, err := normalizeCertificate(Cert{Name: "x", CertPem: test.cert, KeyPem: f.key, IntermediateCSR: test.chain})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(n.CertPem, f.leaf) || leaf.SerialNumber.Cmp(f.leafCert.SerialNumber) != 0 {
				t.Fatal("leaf was not normalized")
			}
			if test.wantChain && !bytes.Equal(n.IntermediateCSR, f.chain) {
				t.Fatal("chain was not exactly deduplicated")
			}
			if !test.wantChain && len(n.IntermediateCSR) != 0 {
				t.Fatal("unexpected chain")
			}
		})
	}
}

func TestNormalizeCertificateKeyFormatsAndValidation(t *testing.T) {
	f := validFixture(t)
	pkcs8, _ := x509.MarshalPKCS8PrivateKey(f.keyRSA)
	for _, key := range [][]byte{f.key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})} {
		if _, _, err := normalizeCertificate(Cert{Name: "x", CertPem: f.leaf, KeyPem: key}); err != nil {
			t.Fatalf("supported key: %v", err)
		}
	}
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ecDER, _ := x509.MarshalECPrivateKey(ecKey)
	ecTemplate := &x509.Certificate{SerialNumber: big.NewInt(3), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	ecCertDER, _ := x509.CreateCertificate(rand.Reader, ecTemplate, ecTemplate, &ecKey.PublicKey, ecKey)
	if _, _, err := normalizeCertificate(Cert{Name: "x", CertPem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ecCertDER}), KeyPem: pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: ecDER})}); err != nil {
		t.Fatalf("SEC1: %v", err)
	}

	other := validFixture(t)
	cases := []Cert{
		{Name: "x", CertPem: []byte("bad"), KeyPem: f.key},
		{Name: "x", CertPem: f.leaf, KeyPem: other.key},
		{Name: "x", CertPem: f.leaf, KeyPem: []byte("bad")},
		{Name: "x", CertPem: f.leaf, KeyPem: pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte("bad")})},
		{Name: "x", CertPem: f.leaf, KeyPem: f.key, IntermediateCSR: []byte("bad")},
	}
	for _, input := range cases {
		if _, _, err := normalizeCertificate(input); !errors.Is(err, ErrCertValidation) {
			t.Fatalf("error = %v, want validation classification", err)
		}
	}
	for _, dates := range [][2]time.Time{{time.Now().Add(-48 * time.Hour), time.Now().Add(-time.Hour)}, {time.Now().Add(time.Hour), time.Now().Add(48 * time.Hour)}} {
		bad := newCertFixture(t, dates[0], dates[1])
		if _, _, err := normalizeCertificate(Cert{Name: "x", CertPem: bad.leaf, KeyPem: bad.key}); !errors.Is(err, ErrCertValidation) {
			t.Fatalf("validity error = %v", err)
		}
	}
}

func multipartValue(t *testing.T, r *http.Request, field, filename string, want []byte) {
	t.Helper()
	reader, err := r.MultipartReader()
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(part)
		found[part.FormName()] = true
		if part.FormName() == field {
			if part.FileName() != filename || part.Header.Get("Content-Type") != "application/x-pem-file" || !bytes.Equal(data, want) {
				t.Errorf("bad multipart %s filename=%q mime=%q bytes=%d", field, part.FileName(), part.Header.Get("Content-Type"), len(data))
			}
			return
		}
	}
	if !found[field] {
		t.Errorf("missing multipart %s", field)
	}
}

func successfulServer(t *testing.T, f certFixture, staleCount *atomic.Int32) *httptest.Server {
	t.Helper()
	n, _, err := normalizeCertificate(Cert{Name: "manual", CertPem: append(append([]byte{}, f.leaf...), f.chain...), KeyPem: f.key, IntermediateCSR: f.chain})
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/nginx/certificates/validate":
			multipartValue(t, r, "certificate", "certificate.pem", n.CertPem)
			fmt.Fprint(w, `{"certificate":{"cn":"example.test"},"certificate_key":true}`)
		case r.URL.Path == "/api/nginx/certificates" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"id":42}`)
		case r.URL.Path == "/api/nginx/certificates/42/upload":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatal(err)
			}
			values := map[string]string{}
			filenames := map[string]string{"certificate": "certificate.pem", "certificate_key": "certificate_key.pem", "intermediate_certificate": "intermediate_certificate.pem"}
			for _, field := range []string{"certificate", "certificate_key", "intermediate_certificate"} {
				file, h, err := r.FormFile(field)
				if err != nil {
					t.Errorf("%s: %v", field, err)
					continue
				}
				if h.Header.Get("Content-Type") != "application/x-pem-file" || h.Filename != filenames[field] {
					t.Errorf("metadata %s filename=%q mime=%q", field, h.Filename, h.Header.Get("Content-Type"))
				}
				b, _ := io.ReadAll(file)
				file.Close()
				values[field] = string(b)
			}
			json.NewEncoder(w).Encode(values)
		case r.URL.Path == "/api/nginx/certificates/42" && r.Method == http.MethodGet:
			if staleCount != nil && staleCount.Add(-1) >= 0 {
				fmt.Fprint(w, `{"id":42,"domain_names":[],"created_on":"2026-01-01 00:00:00","expires_on":"2026-01-01 00:00:00"}`)
				return
			}
			fmt.Fprintf(w, `{"id":42,"domain_names":["example.test"],"created_on":"2026-01-01 00:00:00","expires_on":%q}`, f.leafCert.NotAfter.Format("2006-01-02 15:04:05"))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestCreateCertContractsAndCompatibility(t *testing.T) {
	f := validFixture(t)
	var stale atomic.Int32
	stale.Store(1)
	server := successfulServer(t, f, &stale)
	defer server.Close()
	details, err := CreateCertDetails(server.URL, "token", Cert{Name: "manual", CertPem: append(append([]byte{}, f.leaf...), f.chain...), KeyPem: f.key, IntermediateCSR: f.chain})
	if err != nil {
		t.Fatal(err)
	}
	if details.ID != 42 || len(details.DomainNames) != 1 {
		t.Fatalf("details=%#v", details)
	}
	server2 := successfulServer(t, f, nil)
	defer server2.Close()
	id, err := CreateCert(server2.URL, "token", Cert{Name: "manual", CertPem: f.leaf, KeyPem: f.key, IntermediateCSR: f.chain})
	if err != nil || id != 42 {
		t.Fatalf("CreateCert=(%d,%v)", id, err)
	}
}

func TestValidateRequiresExactContract(t *testing.T) {
	f := validFixture(t)
	for _, tc := range []struct {
		status int
		body   string
	}{{201, `{"certificate":{"cn":"x"},"certificate_key":true}`}, {200, `{}`}, {200, `{"certificate":{"cn":"x"},"certificate_key":false}`}, {200, `not-json`}} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
		_, err := CreateCert(s.URL, "", Cert{Name: "x", CertPem: f.leaf, KeyPem: f.key})
		s.Close()
		if err == nil {
			t.Fatalf("accepted status/body %d %s", tc.status, tc.body)
		}
	}
}

func TestUploadFailuresRollbackIncludingRollbackFailure(t *testing.T) {
	f := validFixture(t)
	n, _, _ := normalizeCertificate(Cert{Name: "x", CertPem: f.leaf, KeyPem: f.key})
	for _, tc := range []struct {
		name                       string
		uploadStatus, deleteStatus int
		upload                     func(http.ResponseWriter)
	}{
		{"non-200", 500, 204, nil},
		{"missing content", 200, 204, func(w http.ResponseWriter) { fmt.Fprint(w, `{}`) }},
		{"mismatched content", 200, 204, func(w http.ResponseWriter) {
			json.NewEncoder(w).Encode(map[string]string{"certificate": "wrong", "certificate_key": string(n.KeyPem)})
		}},
		{"rollback failure", 500, 409, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var deleted bool
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/validate"):
					fmt.Fprint(w, `{"certificate":{"cn":"x"},"certificate_key":true}`)
				case r.URL.Path == "/api/nginx/certificates":
					fmt.Fprint(w, `{"id":7}`)
				case strings.HasSuffix(r.URL.Path, "/upload"):
					w.WriteHeader(tc.uploadStatus)
					if tc.upload != nil {
						tc.upload(w)
					}
				case r.Method == http.MethodDelete:
					deleted = true
					w.WriteHeader(tc.deleteStatus)
				}
			}))
			defer s.Close()
			_, err := CreateCert(s.URL, "", Cert{Name: "x", CertPem: f.leaf, KeyPem: f.key})
			if err == nil || !deleted {
				t.Fatalf("err=%v deleted=%v", err, deleted)
			}
			if tc.deleteStatus == 409 && !strings.Contains(err.Error(), "rollback certificate 7 failed") {
				t.Fatalf("missing rollback context: %v", err)
			}
		})
	}
}

func TestCreateResponseFailureAfterIDRollsBack(t *testing.T) {
	f := validFixture(t)
	for _, test := range []struct {
		name           string
		rollbackStatus int
	}{
		{name: "rollback succeeds", rollbackStatus: http.StatusNoContent},
		{name: "rollback fails", rollbackStatus: http.StatusConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			var uploads, deletes int
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/validate"):
					fmt.Fprint(w, `{"certificate":{"cn":"x"},"certificate_key":true}`)
				case r.URL.Path == "/api/nginx/certificates" && r.Method == http.MethodPost:
					fmt.Fprint(w, `{"id":71,"unfinished":`)
				case strings.HasSuffix(r.URL.Path, "/upload"):
					uploads++
				case r.Method == http.MethodDelete && r.URL.Path == "/api/nginx/certificates/71":
					deletes++
					w.WriteHeader(test.rollbackStatus)
				}
			}))
			defer s.Close()

			_, err := CreateCert(s.URL, "", Cert{Name: "x", CertPem: f.leaf, KeyPem: f.key})
			if err == nil || uploads != 0 || deletes != 1 {
				t.Fatalf("err=%v uploads=%d deletes=%d", err, uploads, deletes)
			}
			stage, category, status := CertFailureInfo(err)
			if stage != "create" || category != "response" || status != http.StatusOK {
				t.Fatalf("failure info=(%q,%q,%d)", stage, category, status)
			}
			if test.rollbackStatus == http.StatusConflict && !strings.Contains(err.Error(), "rollback certificate 71 failed") {
				t.Fatalf("missing rollback failure context: %v", err)
			}
		})
	}
}

func TestMalformedCreateResponseWithoutRecoverableIDDoesNotUploadOrRollback(t *testing.T) {
	f := validFixture(t)
	for _, body := range []string{`{"unfinished":`, `{}`, `{"id":"not-an-integer"}`} {
		var uploads, deletes int
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/validate"):
				fmt.Fprint(w, `{"certificate":{"cn":"x"},"certificate_key":true}`)
			case r.URL.Path == "/api/nginx/certificates" && r.Method == http.MethodPost:
				fmt.Fprint(w, body)
			case strings.HasSuffix(r.URL.Path, "/upload"):
				uploads++
			case r.Method == http.MethodDelete:
				deletes++
			}
		}))
		_, err := CreateCert(s.URL, "", Cert{Name: "x", CertPem: f.leaf, KeyPem: f.key})
		s.Close()
		if err == nil || uploads != 0 || deletes != 0 {
			t.Fatalf("body=%q err=%v uploads=%d deletes=%d", body, err, uploads, deletes)
		}
		stage, category, _ := CertFailureInfo(err)
		if stage != "create" || category != "response" {
			t.Fatalf("failure info=(%q,%q)", stage, category)
		}
	}
}

func TestCreateNonSuccessStatusIsSafeTypedError(t *testing.T) {
	f := validFixture(t)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/validate") {
			fmt.Fprint(w, `{"certificate":{"cn":"x"},"certificate_key":true}`)
			return
		}
		http.Error(w, "RAW_CREATE_RESPONSE_BODY", http.StatusBadGateway)
	}))
	defer s.Close()
	_, err := CreateCert(s.URL, "", Cert{Name: "x", CertPem: f.leaf, KeyPem: f.key})
	if err == nil || strings.Contains(err.Error(), "RAW_CREATE_RESPONSE_BODY") {
		t.Fatalf("unsafe error: %v", err)
	}
	stage, category, status := CertFailureInfo(err)
	if stage != "create" || category != "status" || status != http.StatusBadGateway {
		t.Fatalf("failure info=(%q,%q,%d)", stage, category, status)
	}
}

func TestPersistentStaleConfirmationRollsBack(t *testing.T) {
	f := validFixture(t)
	var deletes int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete:
			deletes++
			w.WriteHeader(204)
		case r.Method == http.MethodGet:
			fmt.Fprint(w, `{"id":8,"domain_names":[],"created_on":"same","expires_on":"same"}`)
		default:
			w.WriteHeader(500)
		}
	}))
	defer s.Close()
	_, err := confirmCertificateWithDelays(s.URL, "", 8, f.leafCert.NotAfter, []time.Duration{0, 0, 0})
	if err == nil {
		t.Fatal("confirmation succeeded")
	}
	_ = rollbackCreatedCertificate(s.URL, "", 8, err)
	if deletes != 1 {
		t.Fatalf("deletes=%d", deletes)
	}
}

func TestMalformedExpiryRetriesExhaustsAndRollsBack(t *testing.T) {
	f := validFixture(t)
	var gets, deletes int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			gets++
			fmt.Fprint(w, `{"id":8,"domain_names":["example.test"],"created_on":"2026-01-01 00:00:00","expires_on":"not-a-timestamp"}`)
		case http.MethodDelete:
			deletes++
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer s.Close()
	err := func() error {
		_, err := confirmCertificateWithDelays(s.URL, "", 8, f.leafCert.NotAfter, []time.Duration{0, 0, 0})
		if err == nil {
			return errors.New("malformed expiry was confirmed")
		}
		return rollbackCreatedCertificate(s.URL, "", 8, err)
	}()
	if err == nil || gets != 3 || deletes != 1 {
		t.Fatalf("err=%v gets=%d deletes=%d", err, gets, deletes)
	}
}

func TestConfirmationRejectsExpiryOutsideTimezoneTolerance(t *testing.T) {
	f := validFixture(t)
	cert := Certificate{
		ID:          9,
		DomainNames: []string{"example.test"},
		CreatedOn:   "2026-01-01 00:00:00",
		ExpiresOn:   f.leafCert.NotAfter.Add(24 * time.Hour).Format("2006-01-02 15:04:05"),
	}
	if certificateConfirmed(cert, 9, f.leafCert.NotAfter) {
		t.Fatal("expiry one day from leaf NotAfter was confirmed")
	}
}

func TestConfirmationRetriesTransientGETAndStaleThenSucceeds(t *testing.T) {
	f := validFixture(t)
	var gets int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets++
		switch gets {
		case 1:
			http.Error(w, "temporarily unavailable", http.StatusBadGateway)
		case 2:
			fmt.Fprint(w, `{"id":10,"domain_names":[],"created_on":"same","expires_on":"same"}`)
		default:
			fmt.Fprintf(w, `{"id":10,"domain_names":["example.test"],"created_on":"2026-01-01 00:00:00","expires_on":%q}`, f.leafCert.NotAfter.Format("2006-01-02 15:04:05"))
		}
	}))
	defer s.Close()
	confirmed, err := confirmCertificateWithDelays(s.URL, "", 10, f.leafCert.NotAfter, []time.Duration{0, 0, 0})
	if err != nil || confirmed.ID != 10 || gets != 3 {
		t.Fatalf("confirmed=%#v err=%v gets=%d", confirmed, err, gets)
	}
}

func TestCertificateModifiedOnCompatibility(t *testing.T) {
	var cert Certificate
	if err := json.Unmarshal([]byte(`{"id":1,"modified_on":"new"}`), &cert); err != nil {
		t.Fatal(err)
	}
	if cert.UpdatedOn != "new" {
		t.Fatal(cert.UpdatedOn)
	}
	details := newCertificateDetails(cert)
	if details.ModifiedOn != "new" {
		t.Fatal(details.ModifiedOn)
	}
}

func TestDeleteCertRejectsNonPositiveID(t *testing.T) {
	if DeleteCert("http://unused", "", 0) == nil {
		t.Fatal("expected error")
	}
}
