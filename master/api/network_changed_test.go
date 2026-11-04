package api

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const testNetworkChangedDelay = 40 * time.Millisecond

func useTestNetworkChanged(t *testing.T) *atomic.Int32 {
	t.Helper()
	sent := &atomic.Int32{}
	oldDelay, oldSend := networkChangedDelay, networkChangedSend
	networkChangedDelay = testNetworkChangedDelay
	networkChangedSend = func() { sent.Add(1) }
	t.Cleanup(func() { networkChangedDelay, networkChangedSend = oldDelay, oldSend })
	return sent
}

func serveNetworkChanged(method string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	broadcastNetworkChanged(handler).ServeHTTP(rec, httptest.NewRequest(method, "/x", nil))
	return rec
}

func waitNetworkChanged() { time.Sleep(3 * testNetworkChangedDelay) }

func noContent(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

func TestBroadcastNetworkChangedSuccessfulPostSendsOnce(t *testing.T) {
	sent := useTestNetworkChanged(t)
	serveNetworkChanged(http.MethodPost, noContent)
	if got := sent.Load(); got != 0 {
		t.Fatalf("sent %d before the delay", got)
	}
	waitNetworkChanged()
	if got := sent.Load(); got != 1 {
		t.Fatalf("sent %d, want 1", got)
	}
}

func TestBroadcastNetworkChangedImplicitStatusCountsAsSuccess(t *testing.T) {
	sent := useTestNetworkChanged(t)
	serveNetworkChanged(http.MethodPost, func(http.ResponseWriter, *http.Request) {})
	waitNetworkChanged()
	if got := sent.Load(); got != 1 {
		t.Fatalf("sent %d, want 1", got)
	}
}

func TestBroadcastNetworkChangedBurstSharesSendAndLaterWriteSendsAgain(t *testing.T) {
	sent := useTestNetworkChanged(t)
	for range 5 {
		serveNetworkChanged(http.MethodPost, noContent)
	}
	waitNetworkChanged()
	if got := sent.Load(); got != 1 {
		t.Fatalf("burst sent %d, want 1", got)
	}
	serveNetworkChanged(http.MethodPost, noContent)
	waitNetworkChanged()
	if got := sent.Load(); got != 2 {
		t.Fatalf("sent %d after a later write, want 2", got)
	}
}

func TestBroadcastNetworkChangedIgnoresReadsAndFailures(t *testing.T) {
	sent := useTestNetworkChanged(t)
	serveNetworkChanged(http.MethodGet, noContent)
	serveNetworkChanged(http.MethodPost, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	})
	waitNetworkChanged()
	if got := sent.Load(); got != 0 {
		t.Fatalf("sent %d, want 0", got)
	}
}

func TestBroadcastNetworkChangedKeepsFlusher(t *testing.T) {
	useTestNetworkChanged(t)
	rec := serveNetworkChanged(http.MethodPost, func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush through wrapper: %v", err)
		}
	})
	if !rec.Flushed {
		t.Fatal("flush did not reach the underlying writer")
	}
	waitNetworkChanged()
}
