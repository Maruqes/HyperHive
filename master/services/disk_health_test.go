package services

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"512SvMan/db"
	"512SvMan/nots"

	btrfsGrpc "github.com/Maruqes/512SvMan/api/proto/btrfs"
	smartdiskGrpc "github.com/Maruqes/512SvMan/api/proto/smartdisk"
)

type fakeDiskFetcher struct {
	mu        sync.Mutex
	disks     map[string][]*btrfsGrpc.MinDisk
	listErr   map[string]error
	smart     map[string]*smartdiskGrpc.SmartDiskInfo
	smartErr  map[string]error
	smartRead []string
}

func (f *fakeDiskFetcher) ListDisks(_ context.Context, machine string) ([]*btrfsGrpc.MinDisk, error) {
	if err := f.listErr[machine]; err != nil {
		return nil, err
	}
	return f.disks[machine], nil
}

func (f *fakeDiskFetcher) GetSmartInfo(_ context.Context, machine, device string) (*smartdiskGrpc.SmartDiskInfo, error) {
	f.mu.Lock()
	f.smartRead = append(f.smartRead, machine+":"+device)
	info := f.smart[machine+":"+device]
	err := f.smartErr[machine+":"+device]
	f.mu.Unlock()
	return info, err
}

type diskHealthHarness struct {
	svc     *DiskHealthService
	fetcher *fakeDiskFetcher
	now     time.Time
	sent    []nots.Notification
}

func newDiskHealthHarness(t *testing.T) *diskHealthHarness {
	t.Helper()
	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	previous := db.DiskHealthDB
	db.DiskHealthDB = conn
	t.Cleanup(func() { db.DiskHealthDB = previous; conn.Close() })
	if err := db.CreateDiskHealthTables(context.Background()); err != nil {
		t.Fatal(err)
	}

	h := &diskHealthHarness{
		fetcher: &fakeDiskFetcher{
			disks:    map[string][]*btrfsGrpc.MinDisk{},
			listErr:  map[string]error{},
			smart:    map[string]*smartdiskGrpc.SmartDiskInfo{},
			smartErr: map[string]error{},
		},
		now: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC),
	}
	var mu sync.Mutex
	h.svc = &DiskHealthService{
		Fetcher: h.fetcher,
		Notify: func(_ context.Context, n nots.Notification) error {
			mu.Lock()
			defer mu.Unlock()
			h.sent = append(h.sent, n)
			return nil
		},
		Machines: func() []string {
			names := make([]string, 0, len(h.fetcher.disks)+len(h.fetcher.listErr))
			for name := range h.fetcher.disks {
				names = append(names, name)
			}
			for name := range h.fetcher.listErr {
				names = append(names, name)
			}
			return names
		},
		Now: func() time.Time { return h.now },
	}
	return h
}

// poll runs one pass and returns the notifications it sent.
func (h *diskHealthHarness) poll(t *testing.T) []nots.Notification {
	t.Helper()
	before := len(h.sent)
	h.svc.PollOnce(context.Background())
	h.now = h.now.Add(6 * time.Hour)
	return h.sent[before:]
}

func (h *diskHealthHarness) addDisk(machine, path, serial, transport string) *smartdiskGrpc.SmartDiskInfo {
	h.fetcher.disks[machine] = append(h.fetcher.disks[machine], &btrfsGrpc.MinDisk{
		Path: path, Serial: serial, Model: "Disk " + serial, Transport: transport, Rotational: true, SizeGb: 1000,
	})
	info := &smartdiskGrpc.SmartDiskInfo{PowerOnHours: 1000, TemperatureC: 35, SmartPassed: true}
	h.fetcher.smart[machine+":"+path] = info
	return info
}

func TestDiskHealthAlerts(t *testing.T) {
	h := newDiskHealthHarness(t)
	info := h.addDisk("a", "/dev/sda", "S1", "sata")

	// Healthy first sample: silent.
	if sent := h.poll(t); len(sent) != 0 {
		t.Fatalf("healthy disk notified: %+v", sent)
	}

	// Pending sectors appear (25 + 15 for 7-day growth): band watch and a
	// counter increase in the same poll give one notification.
	info.PendingSectors = 3
	sent := h.poll(t)
	if len(sent) != 1 {
		t.Fatalf("expected one notification, got %+v", sent)
	}
	n := sent[0]
	if n.Event != "health.disk_health" || n.Severity != nots.SeverityWarning || n.Machine != "a" || n.URL != "/smartdisk/S1" ||
		!strings.Contains(n.Title, "a disk Disk S1 (S1): watch") || !strings.Contains(n.Body, "3 pending sectors") || n.DedupKey != "" {
		t.Fatalf("unexpected notification %+v", n)
	}

	// Same values: silent.
	if sent := h.poll(t); len(sent) != 0 {
		t.Fatalf("repeat poll notified: %+v", sent)
	}

	// Counter increase in the same band: warning at least.
	info.PendingSectors = 4
	if sent := h.poll(t); len(sent) != 1 || sent[0].Severity != nots.SeverityWarning {
		t.Fatalf("counter increase: %+v", sent)
	}

	// Improvement: silent.
	info.PendingSectors = 0
	info.ReallocatedSectors = 0
	if sent := h.poll(t); len(sent) != 0 {
		t.Fatalf("improvement notified: %+v", sent)
	}

	// Band drop after improvement alerts again.
	info.SmartPassed = false
	sent = h.poll(t)
	if len(sent) != 1 || sent[0].Severity != nots.SeverityCritical {
		t.Fatalf("band drop: %+v", sent)
	}

	disk, err := db.GetDisk(context.Background(), "S1")
	if err != nil {
		t.Fatal(err)
	}
	if disk.AlertBand != "critical" || disk.Score == nil || *disk.Score != 10 {
		t.Fatalf("stored state %+v", disk)
	}
}

func TestDiskHealthFirstDegradedSampleNotifiesOnce(t *testing.T) {
	h := newDiskHealthHarness(t)
	info := h.addDisk("a", "/dev/sda", "S1", "sata")
	info.ReallocatedSectors = 5

	sent := h.poll(t)
	if len(sent) != 1 || sent[0].Severity != nots.SeverityWarning {
		t.Fatalf("first degraded sample: %+v", sent)
	}
	if sent := h.poll(t); len(sent) != 0 {
		t.Fatalf("second poll notified: %+v", sent)
	}
}

func TestDiskHealthNotifyFailureRetries(t *testing.T) {
	h := newDiskHealthHarness(t)
	info := h.addDisk("a", "/dev/sda", "S1", "sata")
	info.PendingSectors = 2
	fail := true
	send := h.svc.Notify
	h.svc.Notify = func(ctx context.Context, n nots.Notification) error {
		if fail {
			return errors.New("push down")
		}
		return send(ctx, n)
	}
	h.poll(t)
	fail = false
	if sent := h.poll(t); len(sent) != 1 {
		t.Fatalf("failed notification not retried: %+v", sent)
	}
}

func TestDiskHealthSkipsAndFailures(t *testing.T) {
	h := newDiskHealthHarness(t)
	h.addDisk("a", "/dev/vda", "", "virtio")
	h.addDisk("a", "/dev/vdb", "VIRT1", "virtio")
	noSerial := h.addDisk("a", "/dev/sdc", "", "usb")
	noSerial.Serial = ""
	fallback := h.addDisk("a", "/dev/sdd", "", "sata")
	fallback.Serial = "SMARTSERIAL"
	h.addDisk("a", "/dev/sde", "BROKEN", "sata")
	h.fetcher.smartErr["a:/dev/sde"] = errors.New("smartctl timeout")
	h.fetcher.smart["a:/dev/sde"] = nil
	empty := h.addDisk("a", "/dev/sdf", "EMPTY", "usb")
	empty.PowerOnHours, empty.TemperatureC = 0, 0
	h.fetcher.listErr["down"] = errors.New("host offline")
	h.addDisk("b", "/dev/sda", "B1", "nvme")

	h.poll(t)

	for _, read := range h.fetcher.smartRead {
		if strings.Contains(read, "/dev/vd") {
			t.Fatalf("SMART read on virtio disk %s", read)
		}
	}
	list, err := h.svc.List(context.Background(), "all")
	if err != nil {
		t.Fatal(err)
	}
	bySerial := map[string]DiskHealthView{}
	for _, d := range list.Disks {
		bySerial[d.Serial] = d
	}
	if len(bySerial) != 5 {
		t.Fatalf("unexpected disks %v", bySerial)
	}
	if d := bySerial["VIRT1"]; d.SmartAvailable || d.Score != nil {
		t.Fatalf("virtio disk scored: %+v", d)
	}
	if d := bySerial["SMARTSERIAL"]; !d.SmartAvailable || d.Score == nil || d.Latest == nil {
		t.Fatalf("smartctl serial fallback: %+v", d)
	}
	if d := bySerial["BROKEN"]; d.SmartAvailable || d.Score != nil || !strings.Contains(d.Reasons[0], "smartctl timeout") {
		t.Fatalf("failed SMART read: %+v", d)
	}
	if d := bySerial["EMPTY"]; d.SmartAvailable || d.Score != nil {
		t.Fatalf("empty SMART scored: %+v", d)
	}
	if d := bySerial["B1"]; !d.SmartAvailable || d.Band != "good" {
		t.Fatalf("other host skipped after failure: %+v", d)
	}
	if list.LastPollAt == nil {
		t.Fatal("no last poll time")
	}
}

func TestDiskHealthHistoryAndPurge(t *testing.T) {
	h := newDiskHealthHarness(t)
	h.addDisk("a", "/dev/sda", "S1", "sata")
	start := h.now
	h.poll(t)
	h.poll(t)

	history, err := h.svc.History(context.Background(), "S1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Points) != 2 || history.Points[0].Resolution != "6h" || !history.Points[0].At.Equal(start) || history.Disk.Latest == nil {
		t.Fatalf("history %+v", history)
	}
	if _, err := h.svc.History(context.Background(), "S1", -1); !errors.Is(err, ErrInvalidDiskHealthQuery) {
		t.Fatalf("negative days: %v", err)
	}
	if _, err := h.svc.History(context.Background(), "nope", 0); !errors.Is(err, db.ErrDiskNotFound) {
		t.Fatalf("unknown serial: %v", err)
	}
	if err := h.svc.Purge(context.Background(), "S1"); !errors.Is(err, db.ErrDiskNotRetired) {
		t.Fatalf("purge active disk: %v", err)
	}

	// The disk disappears for more than 90 days, then old samples become daily rows.
	h.fetcher.disks["a"] = nil
	h.now = h.now.Add(91 * 24 * time.Hour)
	h.poll(t)
	retired, err := h.svc.List(context.Background(), "retired")
	if err != nil {
		t.Fatal(err)
	}
	if len(retired.Disks) != 1 || retired.Disks[0].RetiredAt == nil {
		t.Fatalf("disk not retired: %+v", retired)
	}
	history, err = h.svc.History(context.Background(), "S1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Points) != 1 || history.Points[0].Resolution != "daily" || history.Points[0].Band != "good" {
		t.Fatalf("compacted history %+v", history.Points)
	}
	if err := h.svc.Purge(context.Background(), "S1"); err != nil {
		t.Fatal(err)
	}
}
