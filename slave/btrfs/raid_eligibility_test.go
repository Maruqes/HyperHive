package btrfs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	btrfsGrpc "github.com/Maruqes/512SvMan/api/proto/btrfs"
)

func TestRaidRPCRejectsUnavailableDevices(t *testing.T) {
	bin := t.TempDir()
	fixture := `#!/bin/sh
printf '%s\n' '{"blockdevices":[{"name":"sdb","path":"/dev/sdb","type":"disk","size":1073741824,"fstype":null,"mountpoint":null},{"name":"zram0","path":"/dev/zram0","type":"disk","size":1073741824,"fstype":null,"mountpoint":null}]}'
`
	if err := os.WriteFile(filepath.Join(bin, "lsblk"), []byte(fixture), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	s := &BTRFSService{}
	all, err := s.GetAllDisks(context.Background(), &btrfsGrpc.Empty{})
	if err != nil || len(all.Disks) != 1 || all.Disks[0].Path != "/dev/sdb" {
		t.Fatalf("unexpected inventory: %v, %v", all, err)
	}
	if _, err := s.CreateRaid(context.Background(), &btrfsGrpc.CreateRaidReq{Raid: "single", Disks: []string{"/dev/zram0"}}); err == nil {
		t.Fatal("create accepted zram")
	}
	if _, err := s.AddDiskToRaid(context.Background(), &btrfsGrpc.AddDiskToRaidReq{DiskPath: "/dev/zram0"}); err == nil {
		t.Fatal("add accepted zram")
	}
	if _, err := s.ReplaceDiskInRaid(context.Background(), &btrfsGrpc.ReplaceDiskToRaidReq{NewDiskPath: "/dev/zram0"}); err == nil {
		t.Fatal("replace accepted zram")
	}
	for _, paths := range [][]string{nil, {"/dev/sdb", "/dev/sdb"}} {
		if _, err := s.CreateRaid(context.Background(), &btrfsGrpc.CreateRaidReq{Name: "fixture", Raid: "single", Disks: paths}); err == nil {
			t.Fatalf("create accepted invalid disks: %v", paths)
		}
	}
}

func TestValidateRaidDevices(t *testing.T) {
	disks := []MinDisk{{Path: "/dev/sdb", SizeGB: 100}, {Path: "/dev/sdc", SizeGB: 100, Mounted: true}, {Path: "/dev/sdd"}}
	for _, tc := range []struct {
		name  string
		paths []string
		valid bool
	}{
		{"free disk", []string{"/dev/sdb"}, true},
		{"empty list", nil, false},
		{"duplicate", []string{"/dev/sdb", "/dev/sdb"}, false},
		{"mounted disk", []string{"/dev/sdc"}, false},
		{"zero size", []string{"/dev/sdd"}, false},
		{"zram", []string{"/dev/zram0"}, false},
		{"partition", []string{"/dev/sdb1"}, false},
		{"missing path", []string{""}, false},
		{"mixed devices", []string{"/dev/sdb", "/dev/zram0"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateRaidDevices(tc.paths, disks); (err == nil) != tc.valid {
				t.Fatalf("validation error = %v, valid = %v", err, tc.valid)
			}
		})
	}
}

func TestInventoryUsageDiscoveryFailureIsUnavailable(t *testing.T) {
	bin := t.TempDir()
	fixture := `#!/bin/sh
case "$*" in *"NAME,PATH,MODEL"*) printf '%s\n' '{"blockdevices":[{"name":"sdb","path":"/dev/sdb","type":"disk","size":1073741824}]}' ;; *) exit 1 ;; esac
`
	if err := os.WriteFile(filepath.Join(bin, "lsblk"), []byte(fixture), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	disks, err := GetAllDisks(false)
	if err != nil || len(disks) != 1 || !disks[0].Mounted {
		t.Fatalf("expected visible unavailable disk: %v, %v", disks, err)
	}
	if err := validateRaidDevices([]string{"/dev/sdb"}, disks); err == nil {
		t.Fatal("accepted disk with unknown usage")
	}
}

func TestCreateRaidRejectsInvalidRequestBeforeWipe(t *testing.T) {
	for _, tc := range []struct {
		name  string
		raid  *raidType
		disks []string
	}{
		{"valid", nil, []string{"/dev/sdb"}},
		{"", &Single, []string{"/dev/sdb"}},
		{" ", &Single, []string{"/dev/sdb"}},
		{strings.Repeat("a", 256), &Single, []string{"/dev/sdb"}},
		{"valid", &Single, nil},
	} {
		if _, err := CreateRaid(tc.name, tc.raid, tc.disks...); err == nil {
			t.Fatalf("accepted invalid request: %+v", tc)
		}
	}
}

func TestDiskTreeInUse(t *testing.T) {
	swap, btrfs := "[SWAP]", "btrfs"
	for _, tc := range []struct {
		name string
		disk lsblkDevice
		used bool
	}{
		{"empty disk", lsblkDevice{Type: "disk"}, false},
		{"empty partition", lsblkDevice{Type: "disk", Children: []lsblkDevice{{Type: "part"}}}, false},
		{"swap partition", lsblkDevice{Type: "disk", Children: []lsblkDevice{{Type: "part", MountPoint: &swap}}}, true},
		{"unmounted btrfs", lsblkDevice{Type: "disk", Children: []lsblkDevice{{Type: "part", FSType: &btrfs}}}, true},
		{"mapped device", lsblkDevice{Type: "disk", Children: []lsblkDevice{{Type: "part", Children: []lsblkDevice{{Type: "crypt"}}}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := diskTreeInUse(tc.disk, true); got != tc.used {
				t.Fatalf("got %v, want %v", got, tc.used)
			}
		})
	}
}
