package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"512SvMan/db"

	"github.com/Maruqes/512SvMan/logger"
)

const (
	// Under networkTrafficMaxGap, or every sample would lose its baseline.
	networkHistoryInterval      = 10 * time.Second
	networkHistoryCleanInterval = time.Hour
	networkHistoryWriteTimeout  = time.Second
	networkHistoryMaxKeys       = 64
)

// ErrInvalidNetworkHistory marks a request the caller has to change.
var ErrInvalidNetworkHistory = errors.New("invalid network history request")

// Step is at least the resolution; 30d merges 15-minute buckets to keep the chart near 720 points.
var networkHistoryRanges = map[string]struct {
	Span       time.Duration
	Resolution int64
	Step       int64
}{
	"1d":  {24 * time.Hour, 60, 60},
	"7d":  {7 * 24 * time.Hour, 900, 900},
	"30d": {30 * 24 * time.Hour, 900, 3600},
	"3m":  {90 * 24 * time.Hour, 21600, 21600},
	"6m":  {180 * 24 * time.Hour, 21600, 21600},
	"1y":  {365 * 24 * time.Hour, 21600, 21600},
}

// NetworkHistoryPoint holds bytes per second. Nil rates mean nothing was sampled then.
type NetworkHistoryPoint struct {
	At           time.Time `json:"at"`
	Sent         *float64  `json:"sent"`
	Received     *float64  `json:"received"`
	PeakSent     *float64  `json:"peakSent"`
	PeakReceived *float64  `json:"peakReceived"`
}

// NetworkHistory covers one range. Averages count only sampled time.
type NetworkHistory struct {
	Range           string                `json:"range"`
	StepSeconds     int64                 `json:"stepSeconds"`
	Points          []NetworkHistoryPoint `json:"points"`
	SentBytes       uint64                `json:"sentBytes"`
	ReceivedBytes   uint64                `json:"receivedBytes"`
	AverageSent     float64               `json:"averageSent"`
	AverageReceived float64               `json:"averageReceived"`
	PeakSent        float64               `json:"peakSent"`
	PeakReceived    float64               `json:"peakReceived"`
}

// RunHistoryLoop keeps sampling with no page open. Traffic records every sample, so
// polls from an open map add detail to the same buckets instead of competing with this.
func (s *NetworkGraphService) RunHistoryLoop(ctx context.Context) {
	sample := time.NewTicker(networkHistoryInterval)
	defer sample.Stop()
	clean := time.NewTicker(networkHistoryCleanInterval)
	defer clean.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sample.C:
			s.Traffic(ctx)
		case <-clean.C:
			if err := db.CleanNetworkHistory(ctx, time.Now()); err != nil {
				logger.Errorf("network history cleanup: %v", err)
			}
		}
	}
}

// recordNetworkHistory ignores a sample with no valid baseline: it measured nothing,
// so the time it covers must stay a gap.
func recordNetworkHistory(previous time.Time, traffic NetworkTraffic) {
	elapsed := traffic.At.Sub(previous)
	if previous.IsZero() || elapsed <= 0 || elapsed > networkTrafficMaxGap {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), networkHistoryWriteTimeout)
	defer cancel()
	if err := db.RecordNetworkHistory(ctx, traffic.At, networkHistorySamples(traffic, elapsed.Seconds())); err != nil {
		logger.Errorf("network history: %v", err)
	}
}

func networkHistorySamples(traffic NetworkTraffic, seconds float64) map[string]db.NetworkHistorySample {
	out := make(map[string]db.NetworkHistorySample, len(traffic.Endpoints)+len(traffic.Nodes))
	for _, rates := range []map[string]NetworkRate{traffic.Endpoints, traffic.Nodes} {
		for key, rate := range rates {
			out[key] = db.NetworkHistorySample{
				Sent:         uint64(math.Round(rate.Sent * seconds)),
				Received:     uint64(math.Round(rate.Received * seconds)),
				PeakSent:     rate.Sent,
				PeakReceived: rate.Received,
			}
		}
	}
	return out
}

// History sums keys, which is how a line with several endpoints gets one chart.
func (s *NetworkGraphService) History(ctx context.Context, rangeName string, keys []string) (NetworkHistory, error) {
	spec, ok := networkHistoryRanges[rangeName]
	if !ok {
		return NetworkHistory{}, fmt.Errorf("%w: unknown range %q", ErrInvalidNetworkHistory, rangeName)
	}
	if len(keys) == 0 || len(keys) > networkHistoryMaxKeys {
		return NetworkHistory{}, fmt.Errorf("%w: need 1 to %d keys", ErrInvalidNetworkHistory, networkHistoryMaxKeys)
	}
	for _, key := range keys {
		if key == "" {
			return NetworkHistory{}, fmt.Errorf("%w: empty key", ErrInvalidNetworkHistory)
		}
	}
	slots := int64(spec.Span.Seconds()) / spec.Step
	now := time.Now().Unix()
	from := now - now%spec.Step - (slots-1)*spec.Step
	buckets, err := db.GetNetworkHistory(ctx, keys, spec.Resolution, from)
	if err != nil {
		return NetworkHistory{}, err
	}
	out := networkHistorySeries(buckets, from, slots, spec.Step)
	out.Range = rangeName
	return out, nil
}

// networkHistorySeries lays buckets on a fixed grid, so a gap is a point and not a jump
// between its neighbours.
// shortcut: a bucket counts as fully sampled, so the newest one and those beside an outage read low; weigh by coverage if that misleads.
func networkHistorySeries(buckets []db.NetworkHistoryBucket, from, slots, step int64) NetworkHistory {
	merged := make([]*db.NetworkHistorySample, slots)
	out := NetworkHistory{StepSeconds: step, Points: make([]NetworkHistoryPoint, slots)}
	for _, bucket := range buckets {
		slot := (bucket.Bucket - from) / step
		if bucket.Bucket < from || slot >= slots {
			continue
		}
		if merged[slot] == nil {
			merged[slot] = &db.NetworkHistorySample{}
		}
		sample := merged[slot]
		sample.Sent += bucket.Sent
		sample.Received += bucket.Received
		sample.PeakSent = math.Max(sample.PeakSent, bucket.PeakSent)
		sample.PeakReceived = math.Max(sample.PeakReceived, bucket.PeakReceived)
	}
	covered := 0
	for slot, sample := range merged {
		point := &out.Points[slot]
		point.At = time.Unix(from+int64(slot)*step, 0).UTC()
		if sample == nil {
			continue
		}
		covered++
		sent, received := float64(sample.Sent)/float64(step), float64(sample.Received)/float64(step)
		point.Sent, point.Received = &sent, &received
		point.PeakSent, point.PeakReceived = &sample.PeakSent, &sample.PeakReceived
		out.SentBytes += sample.Sent
		out.ReceivedBytes += sample.Received
		out.PeakSent = math.Max(out.PeakSent, sample.PeakSent)
		out.PeakReceived = math.Max(out.PeakReceived, sample.PeakReceived)
	}
	if covered > 0 {
		seconds := float64(int64(covered) * step)
		out.AverageSent, out.AverageReceived = float64(out.SentBytes)/seconds, float64(out.ReceivedBytes)/seconds
	}
	return out
}
