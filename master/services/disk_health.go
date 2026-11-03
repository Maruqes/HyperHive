package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"512SvMan/btrfs"
	"512SvMan/db"
	"512SvMan/diskhealth"
	"512SvMan/nots"
	"512SvMan/protocol"
	"512SvMan/smartdisk"

	btrfsGrpc "github.com/Maruqes/512SvMan/api/proto/btrfs"
	smartdiskGrpc "github.com/Maruqes/512SvMan/api/proto/smartdisk"
	"github.com/Maruqes/512SvMan/logger"
)

const (
	// diskHealthStartDelay lets slaves register before the first poll.
	diskHealthStartDelay = 2 * time.Minute
	// diskHealthInterval is the SMART polling cadence.
	diskHealthInterval = 6 * time.Hour
	// diskHealthRetireAfter retires a serial that has not been seen for this long.
	diskHealthRetireAfter = 90 * 24 * time.Hour
	// diskHealthHostParallelism bounds how many hosts are polled at once.
	diskHealthHostParallelism = 4
	// diskHealthMaxDays bounds the history request range.
	diskHealthMaxDays = 3650
)

// DiskHealthFetcher reads the disk list and SMART data of one host.
type DiskHealthFetcher interface {
	ListDisks(ctx context.Context, machine string) ([]*btrfsGrpc.MinDisk, error)
	GetSmartInfo(ctx context.Context, machine, device string) (*smartdiskGrpc.SmartDiskInfo, error)
}

type grpcDiskHealthFetcher struct{}

func (grpcDiskHealthFetcher) ListDisks(ctx context.Context, machine string) ([]*btrfsGrpc.MinDisk, error) {
	conn := protocol.GetConnectionByMachineName(machine)
	if conn == nil || conn.Connection == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machine)
	}
	res, err := btrfs.GetAllDisks(ctx, conn.Connection)
	if err != nil {
		return nil, err
	}
	return res.GetDisks(), nil
}

func (grpcDiskHealthFetcher) GetSmartInfo(ctx context.Context, machine, device string) (*smartdiskGrpc.SmartDiskInfo, error) {
	conn := protocol.GetConnectionByMachineName(machine)
	if conn == nil || conn.Connection == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machine)
	}
	return smartdisk.GetSmartInfo(ctx, conn.Connection, &smartdiskGrpc.SmartInfoRequest{Device: device})
}

// DiskHealthService polls SMART on every host, stores history in
// disk_health.db, scores each disk and notifies when a disk gets worse.
type DiskHealthService struct {
	Fetcher  DiskHealthFetcher
	Notify   func(ctx context.Context, n nots.Notification) error
	Machines func() []string
	Now      func() time.Time

	running atomic.Bool
}

// NewDiskHealthService wires the service to the live slaves and notifications.
func NewDiskHealthService() *DiskHealthService {
	return &DiskHealthService{
		Fetcher:  grpcDiskHealthFetcher{},
		Notify:   nots.Notify,
		Machines: protocol.GetAllMachineNames,
		Now:      time.Now,
	}
}

// DiskHealthEnabled reports whether disk_health.db is open.
func DiskHealthEnabled() bool {
	return db.DiskHealthDB != nil
}

// RunLoop polls 2 minutes after start and then every 6 hours until ctx ends.
func (s *DiskHealthService) RunLoop(ctx context.Context) {
	if !DiskHealthEnabled() {
		logger.Warn("disk health: database unavailable, polling disabled")
		return
	}
	timer := time.NewTimer(diskHealthStartDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.PollOnce(ctx)
			timer.Reset(diskHealthInterval)
		}
	}
}

// PollOnce runs one pass over all hosts, then retires stale disks and compacts
// history. A pass that is already running makes this a no-op.
func (s *DiskHealthService) PollOnce(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		logger.Warn("disk health: previous poll still running, skipping")
		return
	}
	defer s.running.Store(false)

	now := s.now()
	sem := make(chan struct{}, diskHealthHostParallelism)
	var wg sync.WaitGroup
	for _, machine := range s.Machines() {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(machine string) {
			defer wg.Done()
			defer func() { <-sem }()
			s.pollMachine(ctx, machine)
		}(machine)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return
	}

	if n, err := db.RetireStaleDisks(ctx, now.Add(-diskHealthRetireAfter), now); err != nil {
		logger.Errorf("disk health: %v", err)
	} else if n > 0 {
		logger.Infof("disk health: retired %d disks not seen for 90 days", n)
	}
	if err := db.CompactDiskHealth(ctx, now); err != nil {
		logger.Errorf("disk health: %v", err)
	}
}

// pollMachine reads disks one at a time, because a slow drive can make smartctl
// take a long time and parallel reads would pile up. Each disk is timestamped
// when it is read, so a long pass does not stamp later disks with the pass
// start time.
func (s *DiskHealthService) pollMachine(ctx context.Context, machine string) {
	disks, err := s.Fetcher.ListDisks(ctx, machine)
	if err != nil {
		logger.Errorf("disk health: list disks on %s: %v", machine, err)
		return
	}
	for _, disk := range disks {
		if ctx.Err() != nil {
			return
		}
		if err := s.recordDisk(ctx, machine, disk, s.now()); err != nil {
			logger.Errorf("disk health: %s %s: %v", machine, disk.GetPath(), err)
		}
	}
}

func (s *DiskHealthService) recordDisk(ctx context.Context, machine string, disk *btrfsGrpc.MinDisk, now time.Time) error {
	serial := strings.TrimSpace(disk.GetSerial())
	seen := db.DiskSeen{
		Serial:      serial,
		Model:       strings.TrimSpace(disk.GetModel()),
		MachineName: machine,
		Device:      disk.GetPath(),
		Transport:   disk.GetTransport(),
		Rotational:  disk.GetRotational(),
		// SizeGb is GiB (sizeBytes / 1024^3); convert back to bytes.
		CapacityBytes: int64(disk.GetSizeGb() * 1024 * 1024 * 1024),
		SeenAt:        now,
	}

	if strings.EqualFold(disk.GetTransport(), "virtio") {
		if serial == "" {
			return nil
		}
		if err := db.UpsertDiskSeen(ctx, seen); err != nil {
			return err
		}
		return db.UpdateDiskScore(ctx, serial, db.DiskScoreUpdate{Reason: "Virtual disk without SMART"})
	}

	info, smartErr := s.Fetcher.GetSmartInfo(ctx, machine, disk.GetPath())
	if info != nil {
		if seen.Serial == "" {
			seen.Serial = strings.TrimSpace(info.GetSerial())
		}
		if seen.Model == "" {
			seen.Model = strings.TrimSpace(info.GetModel())
		}
		if info.GetCapacityBytes() > 0 {
			seen.CapacityBytes = info.GetCapacityBytes()
		}
	}
	if seen.Serial == "" {
		return nil
	}
	if err := db.UpsertDiskSeen(ctx, seen); err != nil {
		return err
	}

	if smartErr != nil || info == nil || (info.GetPowerOnHours() <= 0 && info.GetTemperatureC() <= 0) {
		reason := "No SMART data"
		if smartErr != nil {
			reason = "SMART read failed: " + smartErr.Error()
		}
		return db.UpdateDiskScore(ctx, seen.Serial, db.DiskScoreUpdate{Reason: reason})
	}

	history, err := db.GetDiskSamplesSince(ctx, seen.Serial, now.Add(-diskhealth.RawWindow))
	if err != nil {
		return err
	}
	sample := sampleFromSmart(seen.Serial, machine, disk.GetPath(), info, now)
	result := diskhealth.Score(diskhealth.Input{
		Current:    scoreSample(sample),
		History:    scoreSamples(history),
		Rotational: disk.GetRotational(),
	})
	sample.Score = &result.Score
	if err := db.InsertDiskSample(ctx, sample); err != nil {
		return err
	}
	if err := db.UpdateDiskScore(ctx, seen.Serial, db.DiskScoreUpdate{
		SmartAvailable: true,
		Score:          &result.Score,
		Band:           result.Band,
		Reason:         strings.Join(result.Reasons, "\n"),
		WearOutAt:      result.WearOutAt,
	}); err != nil {
		return err
	}
	return s.evaluateAlert(ctx, seen, result, scoreSample(sample).Counters)
}

func sampleFromSmart(serial, machine, device string, info *smartdiskGrpc.SmartDiskInfo, now time.Time) db.DiskHealthSample {
	selfTestFailed := false
	// self_tests[0] is the latest entry: the current ATA self-test status, or the
	// newest NVMe log entry.
	if tests := info.GetSelfTests(); len(tests) > 0 {
		selfTestFailed = diskhealth.SelfTestFailed(tests[0].GetStatus(), tests[0].GetRemainingPercent())
	}
	return db.DiskHealthSample{
		Serial:                  serial,
		CapturedAt:              now,
		MachineName:             machine,
		Device:                  device,
		TemperatureC:            info.GetTemperatureC(),
		PowerOnHours:            info.GetPowerOnHours(),
		PendingSectors:          info.GetPendingSectors(),
		OfflineUncorrectable:    info.GetOfflineUncorrectable(),
		ReallocatedSectors:      info.GetReallocatedSectors(),
		ReportedUncorrectable:   info.GetReportedUncorrectable(),
		MediaErrors:             info.GetMediaErrors(),
		CRCErrorCount:           info.GetCrcErrorCount(),
		PercentageUsed:          info.GetPercentageUsed(),
		AvailableSpare:          info.GetAvailableSpare(),
		AvailableSpareThreshold: info.GetAvailableSpareThreshold(),
		CriticalWarning:         info.GetCriticalWarning(),
		SmartPassed:             info.GetSmartPassed(),
		SelfTestFailed:          selfTestFailed,
		HealthStatus:            info.GetHealthStatus(),
		SmartctlError:           info.GetSmartctlError(),
	}
}

func scoreSample(s db.DiskHealthSample) diskhealth.Sample {
	return diskhealth.Sample{
		CapturedAt: s.CapturedAt,
		Counters: diskhealth.Counters{
			PendingSectors:        s.PendingSectors,
			OfflineUncorrectable:  s.OfflineUncorrectable,
			ReallocatedSectors:    s.ReallocatedSectors,
			ReportedUncorrectable: s.ReportedUncorrectable,
			MediaErrors:           s.MediaErrors,
		},
		CRCErrorCount:           s.CRCErrorCount,
		PercentageUsed:          s.PercentageUsed,
		AvailableSpare:          s.AvailableSpare,
		AvailableSpareThreshold: s.AvailableSpareThreshold,
		CriticalWarning:         s.CriticalWarning,
		SmartPassed:             s.SmartPassed,
		SelfTestFailed:          s.SelfTestFailed,
	}
}

func scoreSamples(samples []db.DiskHealthSample) []diskhealth.Sample {
	out := make([]diskhealth.Sample, len(samples))
	for i, s := range samples {
		out[i] = scoreSample(s)
	}
	return out
}

var diskHealthBandSeverity = map[string]nots.Severity{
	diskhealth.BandWatch:    nots.SeverityWarning,
	diskhealth.BandAtRisk:   nots.SeverityError,
	diskhealth.BandCritical: nots.SeverityCritical,
}

// evaluateAlert notifies when the band is worse than the last evaluated band
// or a critical counter rose. The stored state always moves to the current
// values, so a later drop alerts again and an improvement stays silent. A
// failed notification keeps the old state so the next poll retries it.
func (s *DiskHealthService) evaluateAlert(ctx context.Context, disk db.DiskSeen, result diskhealth.Result, counters diskhealth.Counters) error {
	stored, err := db.GetDisk(ctx, disk.Serial)
	if err != nil {
		return err
	}
	var prev diskhealth.Counters
	if stored.AlertCounters != "" {
		if err := json.Unmarshal([]byte(stored.AlertCounters), &prev); err != nil {
			// A corrupt baseline is treated as zero counters, which can alert once more.
			logger.Warnf("disk health: %s alert counters unreadable, resetting: %v", disk.Serial, err)
			prev = diskhealth.Counters{}
		}
	}

	worse := diskhealth.BandRank(result.Band) > diskhealth.BandRank(stored.AlertBand)
	increased := counters.Increased(prev)
	if worse || len(increased) > 0 {
		severity := diskHealthBandSeverity[result.Band]
		if len(increased) > 0 && severity < nots.SeverityWarning {
			severity = nots.SeverityWarning
		}
		model := disk.Model
		if model == "" {
			model = "unknown model"
		}
		err := s.Notify(ctx, nots.Notification{
			Event:    "health.disk_health",
			Severity: severity,
			Title:    fmt.Sprintf("%s disk %s (%s): %s", disk.MachineName, model, disk.Serial, diskhealth.BandLabel(result.Band)),
			Body:     strings.Join(result.Reasons, "; "),
			URL:      "/smartdisk/" + url.PathEscape(disk.Serial),
			Machine:  disk.MachineName,
		})
		if err != nil {
			return fmt.Errorf("notify disk health: %w", err)
		}
	}

	raw, err := json.Marshal(counters)
	if err != nil {
		return fmt.Errorf("encode alert counters: %w", err)
	}
	return db.UpdateDiskAlertState(ctx, disk.Serial, result.Band, string(raw))
}

// DiskHealthView is a disk with its latest result and sample.
type DiskHealthView struct {
	db.DiskHealthDisk
	Reasons            []string             `json:"reasons"`
	PastRatedEndurance bool                 `json:"pastRatedEndurance"`
	Latest             *db.DiskHealthSample `json:"latest,omitempty"`
}

type DiskHealthList struct {
	LastPollAt *time.Time       `json:"lastPollAt"`
	Disks      []DiskHealthView `json:"disks"`
}

// DiskHealthPoint is one chart point: a raw 6-hourly sample or a compacted day.
type DiskHealthPoint struct {
	At                    time.Time `json:"at"`
	Resolution            string    `json:"resolution"`
	Score                 *int      `json:"score"`
	Band                  string    `json:"band"`
	TemperatureC          int64     `json:"temperatureC"`
	PowerOnHours          int64     `json:"powerOnHours"`
	PendingSectors        int64     `json:"pendingSectors"`
	OfflineUncorrectable  int64     `json:"offlineUncorrectable"`
	ReallocatedSectors    int64     `json:"reallocatedSectors"`
	ReportedUncorrectable int64     `json:"reportedUncorrectable"`
	MediaErrors           int64     `json:"mediaErrors"`
	CRCErrorCount         int64     `json:"crcErrorCount"`
	PercentageUsed        int64     `json:"percentageUsed"`
	SmartPassed           bool      `json:"smartPassed"`
}

type DiskHealthHistory struct {
	Disk   DiskHealthView    `json:"disk"`
	Points []DiskHealthPoint `json:"points"`
}

var ErrInvalidDiskHealthQuery = errors.New("invalid disk health query")

func toDiskHealthView(d db.DiskHealthDisk, latest *db.DiskHealthSample) DiskHealthView {
	view := DiskHealthView{DiskHealthDisk: d, Reasons: []string{}, Latest: latest}
	if d.Reason != "" {
		view.Reasons = strings.Split(d.Reason, "\n")
	}
	view.PastRatedEndurance = !d.Rotational && latest != nil && latest.PercentageUsed >= 100
	return view
}

func bandOf(score *int) string {
	if score == nil {
		return ""
	}
	return diskhealth.BandFor(*score)
}

// List returns disks by state ("active", "retired" or "all").
func (s *DiskHealthService) List(ctx context.Context, state string) (DiskHealthList, error) {
	switch state {
	case "", "all", "active", "retired":
	default:
		return DiskHealthList{}, fmt.Errorf("%w: state must be active, retired or all", ErrInvalidDiskHealthQuery)
	}
	disks, err := db.ListDisks(ctx, state)
	if err != nil {
		return DiskHealthList{}, err
	}
	latest, err := db.GetLatestDiskSamples(ctx)
	if err != nil {
		return DiskHealthList{}, err
	}
	lastPoll, err := db.LastDiskHealthPoll(ctx)
	if err != nil {
		return DiskHealthList{}, err
	}
	out := DiskHealthList{LastPollAt: lastPoll, Disks: make([]DiskHealthView, 0, len(disks))}
	for _, d := range disks {
		var l *db.DiskHealthSample
		if sample, ok := latest[d.Serial]; ok {
			l = &sample
		}
		out.Disks = append(out.Disks, toDiskHealthView(d, l))
	}
	return out, nil
}

// History returns a disk and its chart points, oldest first. days <= 0 means
// all history. Raw samples cover the last 90 days; daily rows cover the days
// before the oldest raw sample.
func (s *DiskHealthService) History(ctx context.Context, serial string, days int) (DiskHealthHistory, error) {
	if days < 0 || days > diskHealthMaxDays {
		return DiskHealthHistory{}, fmt.Errorf("%w: days must be 1..%d", ErrInvalidDiskHealthQuery, diskHealthMaxDays)
	}
	disk, err := db.GetDisk(ctx, serial)
	if err != nil {
		return DiskHealthHistory{}, err
	}
	since := time.Time{}
	if days > 0 {
		since = s.now().Add(-time.Duration(days) * 24 * time.Hour)
	}
	samples, err := db.GetDiskSamplesSince(ctx, serial, since)
	if err != nil {
		return DiskHealthHistory{}, err
	}
	daily, err := db.GetDiskDailySince(ctx, serial, since)
	if err != nil {
		return DiskHealthHistory{}, err
	}

	firstRawDay := ""
	if len(samples) > 0 {
		firstRawDay = samples[0].CapturedAt.UTC().Format("2006-01-02")
	}
	points := make([]DiskHealthPoint, 0, len(daily)+len(samples))
	for _, d := range daily {
		if firstRawDay != "" && d.Day >= firstRawDay {
			continue
		}
		at, err := time.Parse("2006-01-02", d.Day)
		if err != nil {
			return DiskHealthHistory{}, fmt.Errorf("parse daily row day %q: %w", d.Day, err)
		}
		points = append(points, DiskHealthPoint{
			At: at, Resolution: "daily", Score: d.Score, Band: bandOf(d.Score),
			TemperatureC: d.MaxTemperatureC, PowerOnHours: d.PowerOnHours,
			PendingSectors: d.PendingSectors, OfflineUncorrectable: d.OfflineUncorrectable,
			ReallocatedSectors: d.ReallocatedSectors, ReportedUncorrectable: d.ReportedUncorrectable,
			MediaErrors: d.MediaErrors, CRCErrorCount: d.CRCErrorCount,
			PercentageUsed: d.PercentageUsed, SmartPassed: d.SmartPassed,
		})
	}
	for _, r := range samples {
		points = append(points, DiskHealthPoint{
			At: r.CapturedAt, Resolution: "6h", Score: r.Score, Band: bandOf(r.Score),
			TemperatureC: r.TemperatureC, PowerOnHours: r.PowerOnHours,
			PendingSectors: r.PendingSectors, OfflineUncorrectable: r.OfflineUncorrectable,
			ReallocatedSectors: r.ReallocatedSectors, ReportedUncorrectable: r.ReportedUncorrectable,
			MediaErrors: r.MediaErrors, CRCErrorCount: r.CRCErrorCount,
			PercentageUsed: r.PercentageUsed, SmartPassed: r.SmartPassed,
		})
	}

	var latest *db.DiskHealthSample
	if len(samples) > 0 {
		latest = &samples[len(samples)-1]
	} else if l, err := db.GetLatestDiskSamples(ctx); err != nil {
		return DiskHealthHistory{}, err
	} else if sample, ok := l[serial]; ok {
		latest = &sample
	}
	return DiskHealthHistory{Disk: toDiskHealthView(disk, latest), Points: points}, nil
}

// Purge deletes a retired disk and all its history.
func (s *DiskHealthService) Purge(ctx context.Context, serial string) error {
	return db.PurgeRetiredDisk(ctx, serial)
}

func (s *DiskHealthService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
