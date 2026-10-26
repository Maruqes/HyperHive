package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthcheck(test *testing.T) {
	for _, testcase := range []struct {
		name   string
		status int
		body   string
		valid  bool
	}{
		{"healthy", 200, `{"status":"ok"}`, true},
		{"database unavailable", 503, `{"status":"ok"}`, false},
		{"wrong service", 200, `{"status":"other"}`, false},
		{"invalid response", 200, `not json`, false},
		{"oversized response", 200, strings.Repeat(" ", 1024) + `{"status":"ok"}`, false},
	} {
		test.Run(testcase.name, func(testing *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/healthz" {
					testing.Errorf("unexpected path %s", request.URL.Path)
				}
				writer.WriteHeader(testcase.status)
				fmt.Fprint(writer, testcase.body)
			}))
			defer server.Close()
			if err := healthcheck(strings.TrimPrefix(server.URL, "http://")); (err == nil) != testcase.valid {
				testing.Fatalf("healthcheck: %v", err)
			}
		})
	}
}
