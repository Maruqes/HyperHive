package info

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shirou/gopsutil/v4/cpu"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func makeTestDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func TestListPowerDisks(t *testing.T) {
	root := t.TempDir()

	writeTestFile(t, filepath.Join(root, "sda", "queue", "rotational"), "1\n")
	writeTestFile(t, filepath.Join(root, "sdb", "queue", "rotational"), "0\n")
	makeTestDir(t, filepath.Join(root, "nvme0n1"))

	for _, name := range []string{"loop0", "dm-0", "zram0", "sr0", "md0", "nbd0"} {
		makeTestDir(t, filepath.Join(root, name))
	}

	// No queue/rotational file: the disk must be skipped.
	makeTestDir(t, filepath.Join(root, "sdc"))

	disks, err := listPowerDisks(root)
	if err != nil {
		t.Fatalf("listPowerDisks: %v", err)
	}

	want := []*PowerDisk{
		{Name: "nvme0n1", Kind: "nvme"},
		{Name: "sda", Kind: "hdd"},
		{Name: "sdb", Kind: "ssd"},
	}
	if !reflect.DeepEqual(disks, want) {
		t.Fatalf("disks = %+v, want %+v", disks, want)
	}
}

func TestSocketsFromCPUInfo(t *testing.T) {
	// Two sockets with hyper-threading: each physical core appears twice and
	// ci.Cores is the per-socket core count.
	infos := []cpu.InfoStat{
		{PhysicalID: "0", CoreID: "0", ModelName: "Xeon", Cores: 2},
		{PhysicalID: "0", CoreID: "1", ModelName: "Xeon", Cores: 2},
		{PhysicalID: "0", CoreID: "0", ModelName: "Xeon", Cores: 2},
		{PhysicalID: "0", CoreID: "1", ModelName: "Xeon", Cores: 2},
		{PhysicalID: "1", CoreID: "0", ModelName: "Xeon", Cores: 2},
		{PhysicalID: "1", CoreID: "1", ModelName: "Xeon", Cores: 2},
		{PhysicalID: "1", CoreID: "0", ModelName: "Xeon", Cores: 2},
		{PhysicalID: "1", CoreID: "1", ModelName: "Xeon", Cores: 2},
	}

	got := socketsFromCPUInfo(infos)
	want := []*CpuSocket{
		{Model: "Xeon", Cores: 2},
		{Model: "Xeon", Cores: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sockets = %+v, want %+v", got, want)
	}
}

func TestSocketsFromCPUInfoEmptyCoreID(t *testing.T) {
	infos := []cpu.InfoStat{
		{PhysicalID: "0", CoreID: "", ModelName: "EPYC", Cores: 4},
		{PhysicalID: "0", CoreID: "", ModelName: "EPYC", Cores: 4},
		{PhysicalID: "0", CoreID: "", ModelName: "EPYC", Cores: 4},
	}

	got := socketsFromCPUInfo(infos)
	want := []*CpuSocket{{Model: "EPYC", Cores: 4}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sockets = %+v, want %+v", got, want)
	}
}
