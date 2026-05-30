package api

import (
	"strings"
	"testing"
)

func TestDecodeCPUPinningAPIRequestAcceptsCamelCase(t *testing.T) {
	req, err := decodeCPUPinningAPIRequest(strings.NewReader(`{
		"rangeStart": 1,
		"rangeEnd": 5,
		"hyperThreading": true,
		"socketId": 1
	}`))
	if err != nil {
		t.Fatalf("decodeCPUPinningAPIRequest() error = %v", err)
	}

	if req.RangeStart != 1 || req.RangeEnd != 5 || !req.HyperThreading || req.SocketID != 1 {
		t.Fatalf("decoded request = %+v", req)
	}
}

func TestDecodeCPUPinningAPIRequestAcceptsSnakeCase(t *testing.T) {
	req, err := decodeCPUPinningAPIRequest(strings.NewReader(`{
		"range_start": 1,
		"range_end": 5,
		"hyper_threading": true,
		"socket_id": 1
	}`))
	if err != nil {
		t.Fatalf("decodeCPUPinningAPIRequest() error = %v", err)
	}

	if req.RangeStart != 1 || req.RangeEnd != 5 || !req.HyperThreading || req.SocketID != 1 {
		t.Fatalf("decoded request = %+v", req)
	}
}
