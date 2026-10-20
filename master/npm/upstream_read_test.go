package npm

import (
	"context"
	"testing"
	"time"
)

func TestUpstreamReadsHonorCancellationWhileCoordinatorBusy(t *testing.T) {
	upstreamTestDB(t)
	upstreamCoordinator.Lock()
	defer upstreamCoordinator.Unlock()
	for _, read := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"list", func(ctx context.Context) error { _, err := ListUpstreams(ctx); return err }},
		{"capabilities", func(ctx context.Context) error { _, err := UpstreamCapabilities(ctx); return err }},
	} {
		t.Run(read.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- read.run(ctx) }()
			select {
			case err := <-done:
				status, body := UpstreamErrorDetails(err)
				if status != 503 || body["error"] != "upstream_read_timeout" {
					t.Fatalf("got %d %+v", status, body)
				}
			case <-time.After(time.Second):
				t.Fatal("read ignored cancellation while waiting for coordinator")
			}
		})
	}
}

type cancelledVerifyRuntime struct{ fakeUpstreamRuntime }

func (*cancelledVerifyRuntime) Verify(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }

func TestUpstreamCapabilitiesBoundsVerificationAndReleasesCoordinator(t *testing.T) {
	upstreamTestDB(t)
	upstreamCoordinator.runtime = &cancelledVerifyRuntime{}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	caps, err := UpstreamCapabilities(ctx)
	if err != nil || caps.Status != "unknown" {
		t.Fatalf("got %+v, %v", caps, err)
	}
	if !upstreamCoordinator.TryLock() {
		t.Fatal("capability check retained coordinator lock")
	}
	upstreamCoordinator.Unlock()
}
