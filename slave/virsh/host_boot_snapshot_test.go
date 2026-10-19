package virsh

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const bootInfoFixture = "index=0\nkernel=\"/boot/vmlinuz-a\"\nargs=\"ro quiet isolcpus=1\"\ntitle=\"A\"\nindex=1\nkernel=\"/boot/vmlinuz-b\"\nargs=\"ro nohz_full=2\"\ntitle=\"B\"\n"

func TestHostBootSnapshotReadOnly(t *testing.T) {
	var commands []string
	command := func(_ context.Context, name string, args ...string) ([]byte, error) {
		text := name + " " + strings.Join(args, " ")
		commands = append(commands, text)
		switch text {
		case "grubby --info=ALL":
			return []byte(bootInfoFixture), nil
		case "grubby --default-kernel":
			return []byte("/boot/vmlinuz-a\n"), nil
		default:
			t.Fatalf("unexpected command %s", text)
			return nil, nil
		}
	}
	snapshot, err := readHostBootSnapshot(context.Background(), command, func(path string) ([]byte, error) {
		if path != "/proc/cmdline" {
			t.Fatal("unexpected file")
		}
		return []byte("ro isolcpus=1\n"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.GrubbyInfo != bootInfoFixture || snapshot.CurrentCmdline != "ro isolcpus=1\n" || len(snapshot.Entries) != 2 || snapshot.Entries[1].Args != "ro nohz_full=2" || len(commands) != 2 {
		t.Fatal("incomplete actual boot snapshot")
	}
}
func TestHostBootSnapshotFailsClosed(t *testing.T) {
	for _, bad := range []string{"", "index=0\nkernel=\"a\"\n", "index=0\nkernel=\"a\"\nargs=\"x", strings.Replace(bootInfoFixture, "index=1", "index=0", 1), strings.Replace(bootInfoFixture, "index=0", "index=0.5", 1), "args=\"x\"\n", strings.Replace(bootInfoFixture, "args=\"ro nohz_full=2\"", "args=nohz_full=2", 1)} {
		if entries, err := parseHostBootEntries(bad); err == nil || entries != nil {
			t.Fatalf("accepted invalid snapshot %q", bad)
		}
	}
	for _, stage := range []string{"info", "default", "file", "unknown-default"} {
		command := func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if args[0] == "--info=ALL" {
				if stage == "info" {
					return nil, errors.New("PRIVATE")
				}
				return []byte(bootInfoFixture), nil
			}
			if stage == "default" {
				return nil, errors.New("PRIVATE")
			}
			if stage == "unknown-default" {
				return []byte("missing"), nil
			}
			return []byte("/boot/vmlinuz-a\n"), nil
		}
		snapshot, err := readHostBootSnapshot(context.Background(), command, func(string) ([]byte, error) {
			if stage == "file" {
				return nil, errors.New("PRIVATE")
			}
			return []byte("ro\n"), nil
		})
		if snapshot != nil || err == nil || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatalf("%s did not fail closed", stage)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readHostBootSnapshot(ctx, func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("canceled command executed")
		return nil, nil
	}, nil); err == nil {
		t.Fatal("accepted canceled snapshot")
	}
}
