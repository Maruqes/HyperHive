package api

import (
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeStreamLogFile(t *testing.T, path string, lines []string, gzipped bool) {
	t.Helper()
	if gzipped {
		file, err := os.Create(path)
		if err != nil {
			t.Fatalf("create %s: %v", path, err)
		}
		gz := gzip.NewWriter(file)
		for _, line := range lines {
			if _, err := gz.Write([]byte(line + "\n")); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
		}
		if err := gz.Close(); err != nil {
			t.Fatalf("close gzip %s: %v", path, err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("close %s: %v", path, err)
		}
		return
	}
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestStreamLogFamilyReadsRotatedHistory(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "stream-proxy.log")

	// oldest history (rotated, compressed)
	writeStreamLogFile(t, base+".3.gz", []string{
		"10.0.0.1 [01/Jan/2026:10:00:00 +0000] TCP 200 100 50 1.5 [192.168.1.175:25565] -> 192.168.76.77:25565",
	}, true)
	// intermediate rotations
	writeStreamLogFile(t, base+".2.gz", []string{
		"10.0.0.2 [02/Jan/2026:11:00:00 +0000] TCP 200 200 60 2.5 [192.168.1.175:25565] -> 192.168.76.77:25565",
	}, true)
	writeStreamLogFile(t, base+".1", []string{
		"10.0.0.3 [03/Jan/2026:12:00:00 +0000] TCP 200 300 70 3.5 [192.168.1.175:25565] -> 192.168.76.77:25565",
	}, false)
	// live file (today only)
	writeStreamLogFile(t, base, []string{
		"10.0.0.4 [04/Jan/2026:13:00:00 +0000] TCP 200 400 80 4.5 [192.168.1.175:25565] -> 192.168.76.77:25565",
	}, false)

	entries, err := readStreamLogFamily(base, 10)
	if err != nil {
		t.Fatalf("readStreamLogFamily: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries across family, got %d", len(entries))
	}
	wantIPs := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"}
	for i, want := range wantIPs {
		if entries[i].ClientIP != want {
			t.Fatalf("entry %d: expected oldest-first %s, got %s", i, want, entries[i].ClientIP)
		}
	}
	if !entries[3].Time.Equal(time.Date(2026, 1, 4, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("live entry time not parsed: %v", entries[3].Time)
	}
}

func TestStreamLogFamilyMissingReturnsNotExist(t *testing.T) {
	base := filepath.Join(t.TempDir(), "stream-proxy.log")
	_, err := readStreamLogFamily(base, 10)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

func TestStreamLogFamilySurvivesWithoutLiveFile(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "stream-proxy.log")
	// only rotated history exists (e.g. between rotation and first new write)
	writeStreamLogFile(t, base+".1", []string{
		"10.0.0.9 [03/Jan/2026:12:00:00 +0000] TCP 200 300 70 3.5 [192.168.1.175:25565] -> 192.168.76.77:25565",
	}, false)
	entries, err := readStreamLogFamily(base, 10)
	if err != nil {
		t.Fatalf("expected history to load without live file: %v", err)
	}
	if len(entries) != 1 || entries[0].ClientIP != "10.0.0.9" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestStreamLogFamilyToleratesRotationGaps(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "stream-proxy.log")
	writeStreamLogFile(t, base+".5.gz", []string{
		"10.0.0.1 [01/Jan/2026:10:00:00 +0000] TCP 200 100 50 1.5 [192.168.1.175:25565] -> 192.168.76.77:25565",
	}, true)
	writeStreamLogFile(t, base+".1", []string{
		"10.0.0.3 [03/Jan/2026:12:00:00 +0000] TCP 200 300 70 3.5 [192.168.1.175:25565] -> 192.168.76.77:25565",
	}, false)
	writeStreamLogFile(t, base, []string{
		"10.0.0.4 [04/Jan/2026:13:00:00 +0000] TCP 200 400 80 4.5 [192.168.1.175:25565] -> 192.168.76.77:25565",
	}, false)

	entries, err := readStreamLogFamily(base, 10)
	if err != nil {
		t.Fatalf("readStreamLogFamily: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries with gap tolerance, got %d", len(entries))
	}
}

func TestStreamLogFamilyReturnsFreshMutableSlice(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "stream-proxy.log")
	line := "10.0.0.4 [04/Jan/2026:13:00:00 +0000] TCP 200 400 80 4.5 [192.168.1.175:25565] -> 192.168.76.77:25565"
	writeStreamLogFile(t, base, []string{line}, false)

	first, err := readStreamLogFamily(base, 10)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	first[0].Country = "Mutated"
	first[0].ClientIP = "mutated"

	second, err := readStreamLogFamily(base, 10)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if second[0].ClientIP != "10.0.0.4" || second[0].Country != "" {
		t.Fatalf("cache leaked caller mutations: %+v", second[0])
	}
}

func TestStreamLogFamilyIgnoresCorruptGzip(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "stream-proxy.log")
	writeStreamLogFile(t, base, []string{
		"10.0.0.4 [04/Jan/2026:13:00:00 +0000] TCP 200 400 80 4.5 [192.168.1.175:25565] -> 192.168.76.77:25565",
	}, false)
	if err := os.WriteFile(base+".1.gz", []byte("not really gzip"), 0o644); err != nil {
		t.Fatalf("write corrupt gzip: %v", err)
	}

	entries, err := readStreamLogFamily(base, 10)
	if err != nil {
		t.Fatalf("expected graceful degradation on corrupt gzip: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected live entry only, got %d", len(entries))
	}
}

func TestStreamLogFamilyCacheInvalidatesOnGrowth(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "stream-proxy.log")
	writeStreamLogFile(t, base, []string{
		"10.0.0.4 [04/Jan/2026:13:00:00 +0000] TCP 200 400 80 4.5 [192.168.1.175:25565] -> 192.168.76.77:25565",
	}, false)
	first, err := readStreamLogFamily(base, 10)
	if err != nil || len(first) != 1 {
		t.Fatalf("first read: %v %d", err, len(first))
	}

	// live file grows (new connection logged)
	writeStreamLogFile(t, base, []string{
		"10.0.0.4 [04/Jan/2026:13:00:00 +0000] TCP 200 400 80 4.5 [192.168.1.175:25565] -> 192.168.76.77:25565",
		"10.0.0.7 [04/Jan/2026:14:00:00 +0000] TCP 200 100 20 0.5 [192.168.1.175:25565] -> 192.168.76.77:25565",
	}, false)
	second, err := readStreamLogFamily(base, 10)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if len(second) != 2 {
		t.Fatalf("expected cache invalidation on file growth, got %d entries", len(second))
	}
}
