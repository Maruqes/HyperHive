package api

import (
	"512SvMan/db"
	"errors"
	pb "github.com/Maruqes/512SvMan/api/proto/vm_disk"
	"github.com/go-chi/chi/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type downloadFixture struct {
	grpc.ClientStream
	length        string
	chunks        [][]byte
	failure       error
	headerFailure error
}

func (f *downloadFixture) Header() (metadata.MD, error) {
	if f.headerFailure != nil {
		return nil, f.headerFailure
	}
	return metadata.Pairs("disk-length", f.length), nil
}
func (f *downloadFixture) Recv() (*pb.VMDiskChunk, error) {
	if len(f.chunks) == 0 {
		if f.failure != nil {
			return nil, f.failure
		}
		return nil, io.EOF
	}
	chunk := f.chunks[0]
	f.chunks = f.chunks[1:]
	return &pb.VMDiskChunk{Data: chunk}, nil
}
func TestDownloadFramingAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name, length string
		chunks       [][]byte
		failure      error
		wantStatus   int
		abort        bool
		body         string
	}{
		{name: "complete", length: "6", chunks: [][]byte{[]byte("abc"), []byte("def")}, wantStatus: 200, body: "abcdef"},
		{name: "before stream", length: "6", failure: errors.New("agent unavailable"), wantStatus: 502},
		{name: "short stream", length: "6", chunks: [][]byte{[]byte("abc")}, abort: true},
		{name: "last chunk then error", length: "6", chunks: [][]byte{[]byte("abc"), []byte("def")}, failure: errors.New("read failed"), abort: true, body: "abc"},
		{name: "old agent", length: "", wantStatus: 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			panicked := false
			func() {
				defer func() {
					if v := recover(); v != nil {
						if v != http.ErrAbortHandler {
							panic(v)
						}
						panicked = true
					}
				}()
				writeDiskDownload(w, &db.VMDisk{Name: "unsafe\r\nname", Format: "raw"}, &downloadFixture{length: tc.length, chunks: tc.chunks, failure: tc.failure})
			}()
			if panicked != tc.abort {
				t.Fatalf("abort=%v", panicked)
			}
			if tc.wantStatus != 0 && w.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.wantStatus == 200 || tc.abort {
				if w.Body.String() != tc.body {
					t.Fatalf("body=%q", w.Body.String())
				}
			}
			if tc.wantStatus == 200 && w.Header().Get("Content-Length") != "6" {
				t.Fatal("missing length")
			}
		})
	}
}
func TestDiskTransferRoutesRequireAuthentication(t *testing.T) {
	r := chi.NewRouter()
	r.Use(authMiddleware)
	setupVMDiskAPI(r)
	for _, endpoint := range []struct{ method, path string }{{"GET", "/vm-disk/1/download"}, {"PUT", "/vm-disk/import"}, {"POST", "/vm-disk/1/move"}, {"GET", "/vm-disk/operations"}, {"POST", "/vm-disk/operations/test/reconcile"}} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(endpoint.method, endpoint.path, nil))
		if w.Code != 401 {
			t.Fatalf("%s: %d", endpoint.path, w.Code)
		}
	}
}
