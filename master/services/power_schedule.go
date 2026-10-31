package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"512SvMan/db"
	"512SvMan/extra"
	"512SvMan/k8s"
	"512SvMan/nots"
	"512SvMan/protocol"

	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
	"github.com/Maruqes/512SvMan/logger"
)

const (
	// powerScheduleMasterDelay lets slaves power off before the master host.
	powerScheduleMasterDelay = 3 * time.Minute
	// powerScheduleMinOff keeps the off window longer than the master delay.
	powerScheduleMinOff = 10 * time.Minute
	// powerScheduleLoopInterval is how often the daily re-apply checks online hosts.
	powerScheduleLoopInterval = 10 * time.Minute
	// powerScheduleReapplyGrace skips hosts that may still be shutting down after stop.
	powerScheduleReapplyGrace = 15 * time.Minute
)

// PowerScheduleService applies the cluster-wide power schedule to every host.
type PowerScheduleService struct{}

// powerScheduleMu serializes applies so connect, save and the loop never race on one host.
var powerScheduleMu sync.Mutex

// PowerScheduleWindow is the next off window in UTC seconds.
type PowerScheduleWindow struct {
	StopUnix       int64 `json:"stopUnix"`
	MasterStopUnix int64 `json:"masterStopUnix"`
	WakeUnix       int64 `json:"wakeUnix"`
}

type PowerScheduleHost struct {
	db.PowerScheduleStatus
	Connected bool `json:"connected"`
}

type PowerScheduleView struct {
	Schedule   db.PowerSchedule     `json:"schedule"`
	MasterTZ   string               `json:"masterTz"`
	MasterTime time.Time            `json:"masterTime"`
	Next       *PowerScheduleWindow `json:"next,omitempty"`
	Hosts      []PowerScheduleHost  `json:"hosts"`
}

// parseHHMM returns minutes since midnight for "HH:MM".
func parseHHMM(v string) (int, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(v), ":")
	if !ok || len(h) != 2 || len(m) != 2 {
		return 0, fmt.Errorf("time %q must be HH:MM", v)
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("time %q must be HH:MM", v)
	}
	return hh*60 + mm, nil
}

// ValidatePowerSchedule checks the HH:MM values and that the off window is long enough.
func ValidatePowerSchedule(start, stop string) error {
	startMin, err := parseHHMM(start)
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}
	stopMin, err := parseHHMM(stop)
	if err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	if startMin == stopMin {
		return fmt.Errorf("start and stop must differ")
	}
	off := (startMin - stopMin + 24*60) % (24 * 60)
	if time.Duration(off)*time.Minute < powerScheduleMinOff {
		return fmt.Errorf("off window must be at least %s", powerScheduleMinOff)
	}
	return nil
}

// atLocalMinute is the wall-clock minute on the day of t in loc; Go normalizes DST gaps forward.
func atLocalMinute(t time.Time, dayOffset, minute int, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d+dayOffset, minute/60, minute%60, 0, 0, loc)
}

// nextWindow returns the next stop strictly after now and the first start after
// that stop. A host inside the off window gets tomorrow's stop, so it is never
// powered off immediately.
func nextWindow(now time.Time, start, stop string, loc *time.Location) (time.Time, time.Time, error) {
	startMin, err := parseHHMM(start)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	stopMin, err := parseHHMM(stop)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if startMin == stopMin {
		return time.Time{}, time.Time{}, fmt.Errorf("start and stop must differ")
	}

	stopAt := atLocalMinute(now, 0, stopMin, loc)
	if !stopAt.After(now) {
		stopAt = atLocalMinute(now, 1, stopMin, loc)
	}
	wakeAt := atLocalMinute(stopAt, 0, startMin, loc)
	if !wakeAt.After(stopAt) {
		wakeAt = atLocalMinute(stopAt, 1, startMin, loc)
	}
	return stopAt, wakeAt, nil
}

// masterTZName is the master's IANA zone for display; calculations always use time.Local.
func masterTZName() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return strings.TrimPrefix(tz, ":")
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, name, ok := strings.Cut(filepath.ToSlash(target), "zoneinfo/"); ok {
			return name
		}
	}
	return time.Local.String()
}

func (s *PowerScheduleService) Get(ctx context.Context) (PowerScheduleView, error) {
	schedule, err := db.GetPowerSchedule(ctx)
	if err != nil {
		return PowerScheduleView{}, fmt.Errorf("get power schedule: %w", err)
	}
	statuses, err := db.GetPowerScheduleStatuses(ctx)
	if err != nil {
		return PowerScheduleView{}, fmt.Errorf("get power schedule status: %w", err)
	}

	now := time.Now()
	view := PowerScheduleView{Schedule: schedule, MasterTZ: masterTZName(), MasterTime: now}
	if schedule.Enabled {
		if stopAt, wakeAt, err := nextWindow(now, schedule.Start, schedule.Stop, time.Local); err == nil {
			view.Next = &PowerScheduleWindow{
				StopUnix:       stopAt.Unix(),
				MasterStopUnix: stopAt.Add(powerScheduleMasterDelay).Unix(),
				WakeUnix:       wakeAt.Unix(),
			}
		}
	}

	connected := map[string]bool{}
	for _, name := range protocol.GetAllMachineNames() {
		connected[name] = true
	}
	view.Hosts = make([]PowerScheduleHost, 0, len(statuses)+len(connected))
	for _, st := range statuses {
		view.Hosts = append(view.Hosts, PowerScheduleHost{PowerScheduleStatus: st, Connected: connected[st.Machine]})
		delete(connected, st.Machine)
	}
	for name := range connected {
		view.Hosts = append(view.Hosts, PowerScheduleHost{PowerScheduleStatus: db.PowerScheduleStatus{Machine: name}, Connected: true})
	}
	return view, nil
}

// Save stores the schedule and starts applying it to all connected hosts in the
// background (slaves first, then the master host). Progress is visible through Get.
func (s *PowerScheduleService) Save(ctx context.Context, enabled bool, start, stop string) (db.PowerSchedule, error) {
	start, stop = strings.TrimSpace(start), strings.TrimSpace(stop)
	if err := ValidatePowerSchedule(start, stop); err != nil {
		return db.PowerSchedule{}, err
	}
	schedule := db.PowerSchedule{Enabled: enabled, Start: start, Stop: stop, TZ: masterTZName(), UpdatedAt: time.Now().UTC()}
	if err := db.SavePowerSchedule(ctx, schedule); err != nil {
		return db.PowerSchedule{}, fmt.Errorf("save power schedule: %w", err)
	}
	go s.ApplyAll(context.Background())
	return schedule, nil
}

// ApplyAll applies the schedule to every connected host, slaves before the master host.
func (s *PowerScheduleService) ApplyAll(ctx context.Context) {
	var masterHost []string
	for _, name := range protocol.GetAllMachineNames() {
		if s.isMasterHost(ctx, name) {
			masterHost = append(masterHost, name)
			continue
		}
		s.ApplyMachine(ctx, name)
	}
	for _, name := range masterHost {
		s.ApplyMachine(ctx, name)
	}
}

func (s *PowerScheduleService) isMasterHost(ctx context.Context, machine string) bool {
	conn := protocol.GetConnectionByMachineName(machine)
	if conn == nil || conn.Connection == nil {
		return false
	}
	isMaster, err := k8s.IsMasterSlaveCtx(ctx, conn.Connection)
	if err != nil {
		logger.Warnf("power schedule: detect master host %s: %v", machine, err)
		return false
	}
	return isMaster
}

// ApplyMachine sends the current schedule (or a disable) to one host and records the result.
func (s *PowerScheduleService) ApplyMachine(ctx context.Context, machine string) {
	powerScheduleMu.Lock()
	defer powerScheduleMu.Unlock()

	if err := s.applyMachineLocked(ctx, machine); err != nil {
		logger.Errorf("power schedule: apply %s: %v", machine, err)
		now := time.Now()
		if dbErr := db.RecordPowerScheduleFailure(ctx, machine, err.Error(), now); dbErr != nil {
			logger.Errorf("power schedule: record failure for %s: %v", machine, dbErr)
		}
		if notifyErr := nots.Notify(ctx, nots.Notification{
			Event:    "system.power_schedule",
			Severity: nots.SeverityError,
			Machine:  machine,
			Title:    "Power schedule not applied",
			Body:     fmt.Sprintf("%s: %v", machine, err),
			URL:      "/dashboard/" + machine,
			DedupKey: "power_schedule:" + machine,
		}); notifyErr != nil {
			logger.Errorf("power schedule: notify %s: %v", machine, notifyErr)
		}
	}
}

func (s *PowerScheduleService) applyMachineLocked(ctx context.Context, machine string) error {
	schedule, err := db.GetPowerSchedule(ctx)
	if err != nil {
		return fmt.Errorf("load schedule: %w", err)
	}
	conn := protocol.GetConnectionByMachineName(machine)
	if conn == nil || conn.Connection == nil {
		return fmt.Errorf("machine is not connected")
	}

	req := &extraGrpc.PowerScheduleRequest{Enabled: schedule.Enabled}
	if schedule.Enabled {
		stopAt, wakeAt, err := nextWindow(time.Now(), schedule.Start, schedule.Stop, time.Local)
		if err != nil {
			return fmt.Errorf("compute window: %w", err)
		}
		if s.isMasterHost(ctx, machine) {
			stopAt = stopAt.Add(powerScheduleMasterDelay)
		}
		req.StopUnix, req.WakeUnix = stopAt.Unix(), wakeAt.Unix()
	}

	res, err := extra.ApplyPowerSchedule(ctx, conn.Connection, req)
	if err != nil {
		return err
	}
	st := db.PowerScheduleStatus{
		Machine:   machine,
		Enabled:   schedule.Enabled,
		StopUnix:  res.GetStopUnix(),
		WakeUnix:  res.GetWakeUnix(),
		JobID:     res.GetJobId(),
		AppliedAt: time.Now(),
	}
	if err := db.RecordPowerScheduleApplied(ctx, st); err != nil {
		return fmt.Errorf("record status: %w", err)
	}
	if schedule.Enabled {
		logger.Infof("power schedule: %s powers off at %s and wakes at %s (job %s)", machine,
			time.Unix(st.StopUnix, 0).Format(time.RFC3339), time.Unix(st.WakeUnix, 0).Format(time.RFC3339), st.JobID)
	} else {
		logger.Infof("power schedule: disabled on %s", machine)
	}
	return nil
}

// needsReapply reports whether an online host should get the schedule again:
// never applied, last attempt failed, applied state differs, or its stop passed
// (it stayed on, e.g. booted manually or the job failed).
func needsReapply(schedule db.PowerSchedule, st *db.PowerScheduleStatus, now time.Time) bool {
	if st == nil {
		return true
	}
	if st.LastError != "" || st.Enabled != schedule.Enabled {
		return true
	}
	if !schedule.Enabled {
		return false
	}
	return now.After(time.Unix(st.StopUnix, 0).Add(powerScheduleReapplyGrace))
}

// RunLoop re-applies the schedule to online hosts that need it until ctx is canceled.
func (s *PowerScheduleService) RunLoop(ctx context.Context) {
	ticker := time.NewTicker(powerScheduleLoopInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reapplyDue(ctx)
		}
	}
}

func (s *PowerScheduleService) reapplyDue(ctx context.Context) {
	schedule, err := db.GetPowerSchedule(ctx)
	if err != nil {
		logger.Errorf("power schedule loop: %v", err)
		return
	}
	statuses, err := db.GetPowerScheduleStatuses(ctx)
	if err != nil {
		logger.Errorf("power schedule loop: %v", err)
		return
	}
	byMachine := make(map[string]*db.PowerScheduleStatus, len(statuses))
	for i := range statuses {
		byMachine[statuses[i].Machine] = &statuses[i]
	}
	now := time.Now()
	for _, name := range protocol.GetAllMachineNames() {
		if ctx.Err() != nil {
			return
		}
		if needsReapply(schedule, byMachine[name], now) {
			s.ApplyMachine(ctx, name)
		}
	}
}
