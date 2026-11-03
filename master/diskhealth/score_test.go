package diskhealth

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

func healthy(at time.Time) Sample {
	return Sample{CapturedAt: at, SmartPassed: true}
}

func TestScoreRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Sample)
		score  int
		reason string
	}{
		{"healthy", func(*Sample) {}, 100, "No problems found"},
		{"pending low", func(s *Sample) { s.PendingSectors = 3 }, 75, "3 pending sectors"},
		{"pending high", func(s *Sample) { s.PendingSectors = 10 }, 60, "10 pending sectors"},
		{"offline low", func(s *Sample) { s.OfflineUncorrectable = 1 }, 80, "1 offline uncorrectable"},
		{"offline high", func(s *Sample) { s.OfflineUncorrectable = 12 }, 65, "12 offline"},
		{"realloc low", func(s *Sample) { s.ReallocatedSectors = 9 }, 90, "9 reallocated"},
		{"realloc mid", func(s *Sample) { s.ReallocatedSectors = 10 }, 80, "10 reallocated"},
		{"realloc high", func(s *Sample) { s.ReallocatedSectors = 100 }, 65, "100 reallocated"},
		{"reported uncorrectable", func(s *Sample) { s.ReportedUncorrectable = 2 }, 85, "2 reported uncorrectable"},
		{"media low", func(s *Sample) { s.MediaErrors = 1 }, 80, "1 media errors"},
		{"media high", func(s *Sample) { s.MediaErrors = 10 }, 65, "10 media errors"},
		{"critical warning", func(s *Sample) { s.CriticalWarning = 4 }, 60, "critical warning"},
		{"spare below threshold", func(s *Sample) { s.AvailableSpare = 5; s.AvailableSpareThreshold = 10 }, 60, "Available spare 5%"},
		{"spare without threshold", func(s *Sample) { s.AvailableSpare = 0 }, 100, "No problems found"},
		{"crc", func(s *Sample) { s.CRCErrorCount = 7 }, 95, "cable"},
		{"wear 80", func(s *Sample) { s.PercentageUsed = 80 }, 90, "Wear 80%"},
		{"wear 90", func(s *Sample) { s.PercentageUsed = 95 }, 80, "Wear 95%"},
		{"wear 100", func(s *Sample) { s.PercentageUsed = 101 }, 70, "past rated endurance"},
		{"self test failed", func(s *Sample) { s.SelfTestFailed = true }, 70, "self-test failed"},
		{"smart failed cap", func(s *Sample) { s.SmartPassed = false }, 10, "SMART overall health check failed"},
		{"clamped at zero", func(s *Sample) {
			s.PendingSectors, s.OfflineUncorrectable, s.ReallocatedSectors, s.MediaErrors = 50, 50, 500, 50
		}, 0, "pending"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cur := healthy(t0)
			tc.mutate(&cur)
			res := Score(Input{Current: cur})
			if res.Score != tc.score {
				t.Fatalf("score = %d, want %d (%v)", res.Score, tc.score, res.Reasons)
			}
			if res.Band != BandFor(tc.score) {
				t.Fatalf("band = %s, want %s", res.Band, BandFor(tc.score))
			}
			if !strings.Contains(strings.Join(res.Reasons, "|"), tc.reason) {
				t.Fatalf("reasons %v missing %q", res.Reasons, tc.reason)
			}
		})
	}
}

func TestBandEdges(t *testing.T) {
	for score, band := range map[int]string{100: BandGood, 80: BandGood, 79: BandWatch, 60: BandWatch, 59: BandAtRisk, 30: BandAtRisk, 29: BandCritical, 0: BandCritical} {
		if got := BandFor(score); got != band {
			t.Fatalf("BandFor(%d) = %s, want %s", score, got, band)
		}
	}
	if BandRank(BandCritical) <= BandRank(BandAtRisk) || BandRank(BandAtRisk) <= BandRank(BandWatch) || BandRank(BandWatch) <= BandRank("") {
		t.Fatal("band ranks are not ordered")
	}
}

func TestScoreTrends(t *testing.T) {
	old := healthy(t0.Add(-20 * 24 * time.Hour))
	old.PendingSectors = 1
	recent := healthy(t0.Add(-5 * 24 * time.Hour))
	recent.PendingSectors = 1
	cur := healthy(t0)
	cur.PendingSectors = 3

	// Growth inside 7 days: 25 + 15.
	res := Score(Input{Current: cur, History: []Sample{old, recent}})
	if res.Score != 60 || !strings.Contains(res.Reasons[0], "(+2 in 7 days)") {
		t.Fatalf("7d trend: %d %v", res.Score, res.Reasons)
	}

	// Growth only inside 30 days: 25 + 5.
	recent.PendingSectors = 3
	res = Score(Input{Current: cur, History: []Sample{old, recent}})
	if res.Score != 70 || !strings.Contains(res.Reasons[0], "(+2 in 30 days)") {
		t.Fatalf("30d trend: %d %v", res.Score, res.Reasons)
	}

	// Samples older than the raw window are ignored.
	ancient := healthy(t0.Add(-100 * 24 * time.Hour))
	res = Score(Input{Current: cur, History: []Sample{ancient}})
	if res.Score != 75 {
		t.Fatalf("ancient history used: %d %v", res.Score, res.Reasons)
	}

	// CRC growth within 7 days adds 5.
	crcOld := healthy(t0.Add(-2 * 24 * time.Hour))
	crcOld.CRCErrorCount = 1
	crcCur := healthy(t0)
	crcCur.CRCErrorCount = 4
	res = Score(Input{Current: crcCur, History: []Sample{crcOld}})
	if res.Score != 90 {
		t.Fatalf("crc trend: %d %v", res.Score, res.Reasons)
	}
}

func TestWearProjection(t *testing.T) {
	at := func(days int, used int64) Sample {
		s := healthy(t0.Add(-time.Duration(days) * 24 * time.Hour))
		s.PercentageUsed = used
		return s
	}

	// Too few samples.
	if res := Score(Input{Current: at(0, 50)}); res.WearOutAt != nil {
		t.Fatal("projection from one sample")
	}
	// Span shorter than seven days.
	if res := Score(Input{Current: at(0, 51), History: []Sample{at(3, 50)}}); res.WearOutAt != nil {
		t.Fatal("projection from a short span")
	}
	// Flat wear.
	if res := Score(Input{Current: at(0, 50), History: []Sample{at(30, 50), at(60, 50)}}); res.WearOutAt != nil {
		t.Fatal("projection from flat wear")
	}
	// Rising 1% per 10 days from 50%: 100% in about 500 days.
	res := Score(Input{Current: at(0, 50), History: []Sample{at(10, 49), at(20, 48)}})
	if res.WearOutAt == nil {
		t.Fatal("no projection for rising wear")
	}
	if days := res.WearOutAt.Sub(t0).Hours() / 24; days < 490 || days > 510 {
		t.Fatalf("projected %f days", days)
	}
	if res.Score != 100 {
		t.Fatalf("distant wear-out penalized: %d %v", res.Score, res.Reasons)
	}
	// Rising fast: 85% now, 1% per day, 100% in 15 days: 10 + 15.
	res = Score(Input{Current: at(0, 85), History: []Sample{at(10, 75), at(5, 80)}})
	if res.WearOutAt == nil || res.Score != 75 || !strings.Contains(strings.Join(res.Reasons, "|"), "left (estimate)") {
		t.Fatalf("soon wear-out: %d %v %v", res.Score, res.WearOutAt, res.Reasons)
	}
	// Past rated endurance gets no projection.
	res = Score(Input{Current: at(0, 100), History: []Sample{at(10, 90)}})
	if !res.PastRatedEndurance || res.WearOutAt != nil {
		t.Fatalf("past endurance: %+v", res)
	}
	// HDDs get no wear handling.
	res = Score(Input{Current: at(0, 95), History: []Sample{at(20, 50)}, Rotational: true})
	if res.WearOutAt != nil || res.Score != 100 {
		t.Fatalf("rotational wear: %+v", res)
	}
}

func TestSelfTestFailed(t *testing.T) {
	cases := map[string]bool{
		"Completed without error":       false,
		"Self-test routine in progress": false,
		"Aborted by host":               false,
		"Interrupted (host reset)":      false,
		"Completed: read failure":       true,
		"The previous self-test completed having the read element of the test failed.": true,
		"": false,
	}
	for status, want := range cases {
		if got := SelfTestFailed(status, 0); got != want {
			t.Fatalf("SelfTestFailed(%q) = %v, want %v", status, got, want)
		}
	}
	if SelfTestFailed("Completed: read failure", 90) {
		t.Fatal("running test counted as failed")
	}
}

func TestCountersIncreased(t *testing.T) {
	prev := Counters{PendingSectors: 2, MediaErrors: 1}
	cur := Counters{PendingSectors: 1, MediaErrors: 2, ReallocatedSectors: 1}
	got := cur.Increased(prev)
	if len(got) != 2 || got[0] != "reallocated sectors" || got[1] != "media errors" {
		t.Fatalf("Increased = %v", got)
	}
}
