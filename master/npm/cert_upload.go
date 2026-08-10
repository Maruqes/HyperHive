package npm

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"strings"
	"time"
)

var ErrCertValidation = errors.New("invalid certificate upload")

type certValidationError struct{ reason string }

func (e *certValidationError) Error() string     { return e.reason }
func (e *certValidationError) Unwrap() error     { return ErrCertValidation }
func newCertValidationError(reason string) error { return &certValidationError{reason: reason} }

type certOperationError struct {
	stage, category string
	status          int
	cause           error
}

func (e *certOperationError) Error() string {
	if e.status != 0 {
		return fmt.Sprintf("certificate %s failed (%s, status %d)", e.stage, e.category, e.status)
	}
	return fmt.Sprintf("certificate %s failed (%s)", e.stage, e.category)
}
func (e *certOperationError) Unwrap() error { return e.cause }
func newCertOperationError(stage string, status int, category string, cause error) error {
	return &certOperationError{stage: stage, status: status, category: category, cause: cause}
}

// CertFailureInfo returns fields safe to log. It never returns response or PEM data.
func CertFailureInfo(err error) (stage, category string, status int) {
	if errors.Is(err, ErrCertValidation) {
		return "local-validation", "invalid-input", 0
	}
	var operation *certOperationError
	if errors.As(err, &operation) {
		return operation.stage, operation.category, operation.status
	}
	return "create", "internal", 0
}

type uploadedCertificatePEM struct {
	Certificate             *string `json:"certificate"`
	CertificateKey          *string `json:"certificate_key"`
	IntermediateCertificate *string `json:"intermediate_certificate"`
}

func decodeSingleJSON(r io.Reader, target any) error {
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func optionalUploadedPEMMatches(got *string, want []byte) bool {
	if len(want) == 0 {
		return got == nil || *got == ""
	}
	return got != nil && bytes.Equal([]byte(*got), want)
}

func parseCertificates(data []byte, required bool) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := data
	for {
		rest = bytes.TrimSpace(rest)
		if len(rest) == 0 {
			break
		}
		if !bytes.HasPrefix(rest, []byte("-----BEGIN CERTIFICATE-----")) {
			return nil, newCertValidationError("malformed certificate PEM")
		}
		block, remaining := pem.Decode(rest)
		if block == nil {
			return nil, newCertValidationError("malformed certificate PEM")
		}
		if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, newCertValidationError("malformed certificate PEM")
		}
		parsed, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, newCertValidationError("malformed certificate PEM")
		}
		certs = append(certs, parsed)
		rest = remaining
	}
	if required && len(certs) == 0 {
		return nil, newCertValidationError("certificate is required")
	}
	return certs, nil
}

func parsePrivateKey(data []byte) (crypto.Signer, []byte, error) {
	block, rest := pem.Decode(data)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 || len(block.Headers) != 0 {
		return nil, nil, newCertValidationError("malformed private key PEM")
	}
	if x509.IsEncryptedPEMBlock(block) || strings.Contains(block.Type, "ENCRYPTED") {
		return nil, nil, newCertValidationError("encrypted private keys are not supported")
	}
	var key any
	var der []byte
	var err error
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err == nil {
			der = x509.MarshalPKCS1PrivateKey(key.(*rsa.PrivateKey))
		}
	case "PRIVATE KEY":
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		if err == nil {
			der, err = x509.MarshalPKCS8PrivateKey(key)
		}
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
		if err == nil {
			der, err = x509.MarshalECPrivateKey(key.(*ecdsa.PrivateKey))
		}
	default:
		return nil, nil, newCertValidationError("unsupported private key format")
	}
	if err != nil {
		return nil, nil, newCertValidationError("malformed private key PEM")
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, nil, newCertValidationError("unsupported private key type")
	}
	return signer, pem.EncodeToMemory(&pem.Block{Type: block.Type, Bytes: der}), nil
}

func normalizeCertificate(cert Cert) (Cert, *x509.Certificate, error) {
	certs, err := parseCertificates(cert.CertPem, true)
	if err != nil {
		return Cert{}, nil, err
	}
	additional, err := parseCertificates(cert.IntermediateCSR, false)
	if err != nil {
		return Cert{}, nil, err
	}
	leaf := certs[0]
	now := time.Now()
	if now.Before(leaf.NotBefore) {
		return Cert{}, nil, newCertValidationError("certificate is not yet valid")
	}
	if !now.Before(leaf.NotAfter) {
		return Cert{}, nil, newCertValidationError("certificate has expired")
	}
	signer, keyPEM, err := parsePrivateKey(cert.KeyPem)
	if err != nil {
		return Cert{}, nil, err
	}
	leafPublic, err := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	if err != nil {
		return Cert{}, nil, newCertValidationError("unsupported certificate public key")
	}
	keyPublic, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil || !bytes.Equal(leafPublic, keyPublic) {
		return Cert{}, nil, newCertValidationError("private key does not match certificate")
	}

	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw})
	seen := map[string]bool{string(leaf.Raw): true}
	var chain bytes.Buffer
	for _, chainCert := range append(certs[1:], additional...) {
		key := string(chainCert.Raw)
		if seen[key] {
			continue
		}
		seen[key] = true
		_ = pem.Encode(&chain, &pem.Block{Type: "CERTIFICATE", Bytes: chainCert.Raw})
	}
	return Cert{Name: cert.Name, CertPem: leafPEM, KeyPem: keyPEM, IntermediateCSR: chain.Bytes()}, leaf, nil
}

func buildCertificateMultipart(cert Cert) (io.Reader, string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	parts := []struct {
		field, filename string
		data            []byte
	}{
		{"certificate", "certificate.pem", cert.CertPem},
		{"certificate_key", "certificate_key.pem", cert.KeyPem},
	}
	if len(cert.IntermediateCSR) != 0 {
		parts = append(parts, struct {
			field, filename string
			data            []byte
		}{"intermediate_certificate", "intermediate_certificate.pem", cert.IntermediateCSR})
	}
	for _, part := range parts {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, part.field, part.filename))
		h.Set("Content-Type", "application/x-pem-file")
		writer, err := w.CreatePart(h)
		if err != nil {
			return nil, "", err
		}
		if _, err := writer.Write(part.data); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return bytes.NewReader(body.Bytes()), w.FormDataContentType(), nil
}

func certificateConfirmed(cert Certificate, id int, notAfter time.Time) bool {
	if cert.ID != id || id <= 0 || len(cert.DomainNames) == 0 || strings.TrimSpace(cert.ExpiresOn) == "" || cert.ExpiresOn == cert.CreatedOn {
		return false
	}
	parsed, err := time.Parse("2006-01-02 15:04:05", cert.ExpiresOn)
	if err != nil {
		return false
	}
	// NPM's timestamp has no zone. Fifteen hours covers all current civil UTC
	// offsets (UTC-12 through UTC+14) plus sub-second formatting loss, without
	// accepting a date shifted by a full day.
	return parsed.Sub(notAfter).Abs() <= 15*time.Hour
}

func confirmCertificate(baseURL, token string, id int, notAfter time.Time) (Certificate, error) {
	return confirmCertificateWithDelays(baseURL, token, id, notAfter, []time.Duration{0, 50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, 1200 * time.Millisecond})
}

func confirmCertificateWithDelays(baseURL, token string, id int, notAfter time.Time, delays []time.Duration) (Certificate, error) {
	for _, delay := range delays {
		if delay != 0 {
			time.Sleep(delay)
		}
		cert, err := getCertificate(baseURL, token, id)
		if err == nil && certificateConfirmed(cert, id, notAfter) {
			return cert, nil
		}
	}
	return Certificate{}, newCertOperationError("confirm", 0, "exhausted", nil)
}

func rollbackCreatedCertificate(baseURL, token string, id int, cause error) error {
	if err := DeleteCert(baseURL, token, id); err != nil {
		return fmt.Errorf("%w; rollback certificate %d failed: %v", cause, id, err)
	}
	return fmt.Errorf("%w (created certificate %d rolled back)", cause, id)
}
