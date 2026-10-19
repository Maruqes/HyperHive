package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type nginxDeadlineTransport func(*http.Request) (*http.Response, error)

func (fn nginxDeadlineTransport) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestCertificateTimeoutsPreserveOrdinaryRequestsAndCancellation(t *testing.T) {
	api, err := New(Config{BaseURL: "http://fixture.invalid/prefix/api", Token: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		call    func(context.Context) error
	}{
		{"renew", 11 * time.Minute, func(ctx context.Context) error { return api.RenewCertificate(ctx, NginxIDInput{42}) }},
		{"download", 3 * time.Minute, func(ctx context.Context) error { _, err := api.DownloadCertificate(ctx, NginxIDInput{42}); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api.http.Transport = nginxDeadlineTransport(func(req *http.Request) (*http.Response, error) {
				if req.Header.Get("Authorization") != "Bearer fixture-key" {
					t.Error("missing auth")
				}
				deadline, ok := req.Context().Deadline()
				remaining := time.Until(deadline)
				if !ok || remaining < tc.timeout-time.Second || remaining > tc.timeout {
					t.Errorf("long-operation deadline %v", remaining)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/zip"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			if err := tc.call(context.Background()); err != nil {
				t.Fatal(err)
			}
			if api.http.Timeout != 30*time.Second || api.http.CheckRedirect == nil {
				t.Fatal("ordinary transport defaults changed")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			api.http.Transport = nginxDeadlineTransport(func(req *http.Request) (*http.Response, error) {
				deadline, ok := req.Context().Deadline()
				if !ok || time.Until(deadline) > time.Second {
					t.Error("caller deadline was extended")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			if err := tc.call(ctx); err != nil {
				t.Fatal(err)
			}
			canceled, stop := context.WithCancel(context.Background())
			stop()
			api.http.Transport = nginxDeadlineTransport(func(req *http.Request) (*http.Response, error) { return nil, req.Context().Err() })
			if err := tc.call(canceled); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
		})
	}
}
