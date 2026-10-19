package virsh

import (
	"context"
	"errors"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func parseHostBootEntries(raw string) ([]*grpcVirsh.HostBootEntry, error) {
	bad := errors.New("host boot snapshot unavailable")
	if raw == "" || !strings.HasSuffix(raw, "\n") {
		return nil, bad
	}
	entries := []*grpcVirsh.HostBootEntry{}
	var entry *grpcVirsh.HostBootEntry
	fields := map[string]bool{}
	seen := map[int32]bool{}
	finish := func() bool {
		return entry != nil && fields["index"] && fields["kernel"] && fields["args"] && entry.Kernel != ""
	}
	for _, line := range strings.Split(strings.TrimSuffix(raw, "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" {
			return nil, bad
		}
		if key == "index" {
			if entry != nil && !finish() {
				return nil, bad
			}
			number, err := strconv.ParseInt(value, 10, 32)
			if err != nil || number < 0 || seen[int32(number)] {
				return nil, bad
			}
			seen[int32(number)] = true
			entry = &grpcVirsh.HostBootEntry{Index: int32(number)}
			entries = append(entries, entry)
			fields = map[string]bool{}
		}
		if entry == nil || fields[key] {
			return nil, bad
		}
		fields[key] = true
		if key == "kernel" || key == "args" {
			if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
				return nil, bad
			}
			value = value[1 : len(value)-1]
			if strings.ContainsAny(value, "\x00\r\n") {
				return nil, bad
			}
			if key == "kernel" {
				entry.Kernel = value
			} else {
				entry.Args = value
			}
		}
	}
	if !finish() || len(entries) == 0 {
		return nil, bad
	}
	return entries, nil
}

// Fixed read-only commands and file paths are injected for disposable tests.
// Neither command text nor a path is accepted from API callers.
func readHostBootSnapshot(ctx context.Context, command func(context.Context, string, ...string) ([]byte, error), readFile func(string) ([]byte, error)) (*grpcVirsh.HostBootSnapshotResponse, error) {
	bad := errors.New("host boot snapshot unavailable")
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := command(ctx, "grubby", "--info=ALL")
	if err != nil {
		return nil, bad
	}
	entries, err := parseHostBootEntries(string(raw))
	if err != nil {
		return nil, bad
	}
	kernel, err := command(ctx, "grubby", "--default-kernel")
	if err != nil {
		return nil, bad
	}
	defaultKernel := strings.TrimSpace(string(kernel))
	if defaultKernel == "" || strings.ContainsAny(defaultKernel, "\x00\r\n") {
		return nil, bad
	}
	found := false
	for _, entry := range entries {
		if entry.Kernel == defaultKernel {
			found = true
		}
	}
	if !found {
		return nil, bad
	}
	cmdline, err := readFile("/proc/cmdline")
	if err != nil || strings.TrimSpace(string(cmdline)) == "" || strings.Contains(string(cmdline), "\x00") {
		return nil, bad
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &grpcVirsh.HostBootSnapshotResponse{GrubbyInfo: string(raw), CurrentCmdline: string(cmdline), DefaultKernel: defaultKernel, Entries: entries}, nil
}
func (s *SlaveVirshService) GetHostBootSnapshot(ctx context.Context, _ *grpcVirsh.Empty) (*grpcVirsh.HostBootSnapshotResponse, error) {
	return readHostBootSnapshot(ctx, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	}, os.ReadFile)
}
