// Package diskhealth turns stored SMART samples into a 0-100 health score,
// a band and plain-language reasons. It is pure logic with no I/O.
package diskhealth

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Bands, from best to worst.
const (
	BandGood     = "good"
	BandWatch    = "watch"
	BandAtRisk   = "at_risk"
	BandCritical = "critical"
)

const (
	// RawWindow is how long raw samples are kept and the span trends and the wear
	// projection look at.
	RawWindow = 90 * 24 * time.Hour
	// DailyWindow is how long compacted daily rows are kept.
	DailyWindow = 2 * 365 * 24 * time.Hour

	shortTrend = 7 * 24 * time.Hour
	longTrend  = 30 * 24 * time.Hour

	// wearProjectionMinSpan is the shortest sample span the wear projection trusts.
	wearProjectionMinSpan = 7 * 24 * time.Hour
	// wearSoonWindow adds a penalty when rated wear is projected inside it.
	wearSoonWindow = 90 * 24 * time.Hour
	// failedSmartCap is the highest score a disk with a failed SMART verdict can get.
	failedSmartCap = 10
)

// Counters are the SMART counters whose growth means the medium is degrading.
type Counters struct {
	PendingSectors        int64 `json:"pendingSectors"`
	OfflineUncorrectable  int64 `json:"offlineUncorrectable"`
	ReallocatedSectors    int64 `json:"reallocatedSectors"`
	ReportedUncorrectable int64 `json:"reportedUncorrectable"`
	MediaErrors           int64 `json:"mediaErrors"`
}

// Increased reports the names of counters that are higher than in prev.
func (c Counters) Increased(prev Counters) []string {
	var names []string
	for _, f := range c.fields() {
		if f.value > f.of(prev) {
			names = append(names, f.label)
		}
	}
	return names
}

type counterField struct {
	label string
	value int64
	of    func(Counters) int64
}

func (c Counters) fields() []counterField {
	return []counterField{
		{"pending sectors", c.PendingSectors, func(x Counters) int64 { return x.PendingSectors }},
		{"offline uncorrectable sectors", c.OfflineUncorrectable, func(x Counters) int64 { return x.OfflineUncorrectable }},
		{"reallocated sectors", c.ReallocatedSectors, func(x Counters) int64 { return x.ReallocatedSectors }},
		{"reported uncorrectable errors", c.ReportedUncorrectable, func(x Counters) int64 { return x.ReportedUncorrectable }},
		{"media errors", c.MediaErrors, func(x Counters) int64 { return x.MediaErrors }},
	}
}

// Sample is one SMART reading reduced to the values the scorer needs.
type Sample struct {
	CapturedAt time.Time
	Counters
	CRCErrorCount           int64
	PercentageUsed          int64
	AvailableSpare          int64
	AvailableSpareThreshold int64
	CriticalWarning         int64
	SmartPassed             bool
	SelfTestFailed          bool
}

// Input is the current reading plus the stored history of the same disk.
type Input struct {
	Current Sample
	// History holds earlier samples of the disk, in any order. Samples outside
	// RawWindow or after Current are ignored.
	History []Sample
	// Rotational disks (HDDs) get no remaining-life estimate.
	Rotational bool
}

// Result is the score of one reading.
type Result struct {
	Score   int
	Band    string
	Reasons []string
	// WearOutAt is the projected date percentage_used reaches 100%. It is an
	// estimate and is nil when there is not enough data or no upward trend.
	WearOutAt *time.Time
	// PastRatedEndurance is true when percentage_used is already 100% or more.
	PastRatedEndurance bool
}

// BandFor maps a score to its band.
func BandFor(score int) string {
	switch {
	case score >= 80:
		return BandGood
	case score >= 60:
		return BandWatch
	case score >= 30:
		return BandAtRisk
	default:
		return BandCritical
	}
}

// BandRank orders bands so a higher rank is worse. Unknown bands rank as good.
func BandRank(band string) int {
	switch band {
	case BandWatch:
		return 1
	case BandAtRisk:
		return 2
	case BandCritical:
		return 3
	default:
		return 0
	}
}

// BandLabel is the human label of a band.
func BandLabel(band string) string {
	if band == BandAtRisk {
		return "at risk"
	}
	return band
}

// SelfTestFailed reports whether the latest self-test entry finished with an
// error. Running, aborted and interrupted tests do not count as failures.
func SelfTestFailed(status string, remainingPercent int64) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	if s == "" || remainingPercent != 0 {
		return false
	}
	for _, neutral := range []string{"without error", "in progress", "running", "abort", "interrupt", "never"} {
		if strings.Contains(s, neutral) {
			return false
		}
	}
	return true
}

// Score computes the health of the current reading using its history.
func Score(in Input) Result {
	now := in.Current.CapturedAt
	history := window(in.History, now)
	cur := in.Current

	penalty := 0
	var reasons []string
	add := func(points int, reason string) {
		penalty += points
		reasons = append(reasons, reason)
	}

	base7 := oldestSince(history, now.Add(-shortTrend))
	base30 := oldestSince(history, now.Add(-longTrend))

	levels := map[string]func(int64) int{
		"pending sectors":               tiered(25, 40),
		"offline uncorrectable sectors": tiered(20, 35),
		"reallocated sectors":           reallocatedPenalty,
		"reported uncorrectable errors": func(int64) int { return 15 },
		"media errors":                  tiered(20, 35),
	}
	for _, f := range cur.Counters.fields() {
		if f.value <= 0 {
			continue
		}
		points := levels[f.label](f.value)
		reason := fmt.Sprintf("%d %s", f.value, f.label)
		switch {
		case base7 != nil && f.value > f.of(base7.Counters):
			points += 15
			reason += fmt.Sprintf(" (+%d in 7 days)", f.value-f.of(base7.Counters))
		case base30 != nil && f.value > f.of(base30.Counters):
			points += 5
			reason += fmt.Sprintf(" (+%d in 30 days)", f.value-f.of(base30.Counters))
		}
		add(points, reason)
	}

	if cur.CriticalWarning != 0 {
		add(40, fmt.Sprintf("NVMe critical warning flag set (0x%x)", cur.CriticalWarning))
	}
	if cur.AvailableSpareThreshold > 0 && cur.AvailableSpare < cur.AvailableSpareThreshold {
		add(40, fmt.Sprintf("Available spare %d%% is below the %d%% threshold", cur.AvailableSpare, cur.AvailableSpareThreshold))
	}
	if cur.CRCErrorCount > 0 {
		points := 5
		reason := fmt.Sprintf("%d CRC errors (usually a cable problem)", cur.CRCErrorCount)
		if base7 != nil && cur.CRCErrorCount > base7.CRCErrorCount {
			points += 5
			reason = fmt.Sprintf("%d CRC errors (+%d in 7 days, usually a cable problem)", cur.CRCErrorCount, cur.CRCErrorCount-base7.CRCErrorCount)
		}
		add(points, reason)
	}
	if cur.SelfTestFailed {
		add(30, "Latest SMART self-test failed")
	}

	result := Result{}
	if !in.Rotational {
		wearPenalty, wearReason := 0, ""
		switch {
		case cur.PercentageUsed >= 100:
			wearPenalty = 30
			result.PastRatedEndurance = true
			wearReason = fmt.Sprintf("Wear %d%%, past rated endurance", cur.PercentageUsed)
		case cur.PercentageUsed >= 90:
			wearPenalty = 20
		case cur.PercentageUsed >= 80:
			wearPenalty = 10
		}
		if !result.PastRatedEndurance {
			result.WearOutAt = projectWearOut(append(history, cur), now)
			soon := result.WearOutAt != nil && result.WearOutAt.Sub(now) <= wearSoonWindow
			if soon {
				wearPenalty += 15
			}
			if wearPenalty > 0 {
				wearReason = fmt.Sprintf("Wear %d%%", cur.PercentageUsed)
				if result.WearOutAt != nil {
					wearReason += ", " + remainingText(result.WearOutAt.Sub(now)) + " left (estimate)"
				}
			}
		}
		if wearPenalty > 0 {
			add(wearPenalty, wearReason)
		}
	}

	score := 100 - penalty
	if score < 0 {
		score = 0
	}
	if !cur.SmartPassed {
		reasons = append([]string{"SMART overall health check failed"}, reasons...)
		if score > failedSmartCap {
			score = failedSmartCap
		}
	}
	if len(reasons) == 0 {
		reasons = []string{"No problems found"}
	}
	result.Score = score
	result.Band = BandFor(score)
	result.Reasons = reasons
	return result
}

func tiered(low, high int) func(int64) int {
	return func(v int64) int {
		if v >= 10 {
			return high
		}
		return low
	}
}

func reallocatedPenalty(v int64) int {
	switch {
	case v >= 100:
		return 35
	case v >= 10:
		return 20
	default:
		return 10
	}
}

// window keeps the samples inside RawWindow that are not after now.
func window(samples []Sample, now time.Time) []Sample {
	cutoff := now.Add(-RawWindow)
	out := make([]Sample, 0, len(samples))
	for _, s := range samples {
		if s.CapturedAt.Before(cutoff) || s.CapturedAt.After(now) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// oldestSince returns the oldest sample captured at or after since.
func oldestSince(samples []Sample, since time.Time) *Sample {
	var oldest *Sample
	for i := range samples {
		s := &samples[i]
		if s.CapturedAt.Before(since) {
			continue
		}
		if oldest == nil || s.CapturedAt.Before(oldest.CapturedAt) {
			oldest = s
		}
	}
	return oldest
}

// projectWearOut fits a least-squares line to percentage_used and returns when
// it reaches 100%. It needs two samples at least seven days apart and a
// positive slope.
func projectWearOut(samples []Sample, now time.Time) *time.Time {
	if len(samples) < 2 {
		return nil
	}
	first, last := samples[0].CapturedAt, samples[0].CapturedAt
	for _, s := range samples {
		if s.CapturedAt.Before(first) {
			first = s.CapturedAt
		}
		if s.CapturedAt.After(last) {
			last = s.CapturedAt
		}
	}
	if last.Sub(first) < wearProjectionMinSpan {
		return nil
	}

	// x is hours since the first sample, y is percentage_used.
	n := float64(len(samples))
	var sumX, sumY, sumXX, sumXY float64
	for _, s := range samples {
		x := s.CapturedAt.Sub(first).Hours()
		y := float64(s.PercentageUsed)
		sumX += x
		sumY += y
		sumXX += x * x
		sumXY += x * y
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return nil
	}
	slope := (n*sumXY - sumX*sumY) / denom
	if slope <= 0 {
		return nil
	}
	intercept := (sumY - slope*sumX) / n
	hours := (100 - intercept) / slope
	if math.IsNaN(hours) || math.IsInf(hours, 0) || hours > float64(100*365*24) {
		return nil
	}
	at := first.Add(time.Duration(hours * float64(time.Hour)))
	if at.Before(now) {
		at = now
	}
	return &at
}

// remainingText renders a duration as "~N days", "~N months" or "~N years".
func remainingText(d time.Duration) string {
	days := int(math.Round(d.Hours() / 24))
	switch {
	case days < 60:
		return fmt.Sprintf("~%d days", days)
	case days < 730:
		return fmt.Sprintf("~%d months", int(math.Round(float64(days)/30.4)))
	default:
		return fmt.Sprintf("~%d years", int(math.Round(float64(days)/365)))
	}
}
