package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"512SvMan/db"
)

func TestNetworkHistorySamplesTurnRatesIntoBytes(t *testing.T) {
	got := networkHistorySamples(NetworkTraffic{
		Endpoints: map[string]NetworkRate{"10.0.0.5:80": {100, 50}},
		Nodes:     map[string]NetworkRate{"vm:a:web": {10, 0}},
	}, 2)
	if got["10.0.0.5:80"] != (db.NetworkHistorySample{Sent: 200, Received: 100, PeakSent: 100, PeakReceived: 50}) {
		t.Fatalf("endpoint = %+v", got["10.0.0.5:80"])
	}
	if got["vm:a:web"].Sent != 20 || len(got) != 2 {
		t.Fatalf("samples = %+v", got)
	}
}

func TestNetworkHistorySeriesKeepsGapsAndMergesToStep(t *testing.T) {
	const from, step = int64(3600), int64(3600)
	buckets := []db.NetworkHistoryBucket{
		{Bucket: from - 900, NetworkHistorySample: db.NetworkHistorySample{Sent: 999}},
		{Bucket: from, NetworkHistorySample: db.NetworkHistorySample{Sent: 3600, PeakSent: 5}},
		{Bucket: from + 900, NetworkHistorySample: db.NetworkHistorySample{Sent: 3600, Received: 7200, PeakSent: 9}},
		// Sampled but idle: zero, not a gap.
		{Bucket: from + 2*step},
		{Bucket: from + 3*step, NetworkHistorySample: db.NetworkHistorySample{Sent: 999}},
	}
	got := networkHistorySeries(buckets, from, 3, step)
	if len(got.Points) != 3 {
		t.Fatalf("points = %d", len(got.Points))
	}
	first := got.Points[0]
	if first.Sent == nil || *first.Sent != 2 || *first.Received != 2 || *first.PeakSent != 9 {
		t.Fatalf("merged slot = %+v", first)
	}
	if got.Points[1].Sent != nil || !got.Points[1].At.Equal(time.Unix(from+step, 0)) {
		t.Fatalf("gap = %+v", got.Points[1])
	}
	if got.Points[2].Sent == nil || *got.Points[2].Sent != 0 {
		t.Fatalf("idle = %+v", got.Points[2])
	}
	// The gap does not dilute the average: two sampled hours moved 7200 bytes.
	if got.SentBytes != 7200 || got.AverageSent != 1 || got.PeakSent != 9 {
		t.Fatalf("summary = %+v", got)
	}
}

func TestNetworkHistoryRejectsBadRequests(t *testing.T) {
	service := &NetworkGraphService{}
	for name, call := range map[string]func() error{
		"range": func() error { _, err := service.History(context.Background(), "2d", []string{"k"}); return err },
		"none":  func() error { _, err := service.History(context.Background(), "1d", nil); return err },
		"empty": func() error { _, err := service.History(context.Background(), "1d", []string{""}); return err },
		"many": func() error {
			_, err := service.History(context.Background(), "1d", make([]string, networkHistoryMaxKeys+1))
			return err
		},
	} {
		if err := call(); !errors.Is(err, ErrInvalidNetworkHistory) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestNetworkHistoryRangesFitRetentionGrid(t *testing.T) {
	for name, spec := range networkHistoryRanges {
		if spec.Step%spec.Resolution != 0 || int64(spec.Span.Seconds())%spec.Step != 0 {
			t.Fatalf("%s does not divide evenly: %+v", name, spec)
		}
	}
}
