package celld

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func smallProcessLog(t *testing.T) *processLog {
	t.Helper()
	w := newProcessLog(t.TempDir())
	w.maxSize, w.minFree = 8, 0
	w.freeSpace = func(string) (uint64, error) { return 1 << 30, nil }
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func assertLogBounds(t *testing.T, w *processLog) {
	t.Helper()
	files, err := os.ReadDir(filepath.Dir(w.path))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) > w.backups+1 {
		t.Fatalf("too many retained files: %d", len(files))
	}
	var total int64
	for _, file := range files {
		info, err := file.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > w.maxSize || info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe log file: %s, size %d, mode %o", file.Name(), info.Size(), info.Mode().Perm())
		}
		total += info.Size()
	}
	if total > w.maxSize*int64(w.backups+1) {
		t.Fatalf("log storage exceeded retention budget: %d", total)
	}
}

func TestProcessLogRotationAndReopen(t *testing.T) {
	w := smallProcessLog(t)
	payload := []byte("0000000011111111222222223333333344444444555")
	if n, err := w.Write(payload); err != nil || n != len(payload) {
		t.Fatalf("write: %d, %v", n, err)
	}
	for name, want := range map[string]string{"": "555", ".1": "44444444", ".2": "33333333", ".3": "22222222"} {
		data, err := os.ReadFile(w.path + name)
		if err != nil || string(data) != want {
			t.Fatalf("%s: got %q, want %q, error %v", name, data, want, err)
		}
	}
	assertLogBounds(t, w)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := newProcessLog(filepath.Dir(filepath.Dir(w.path)))
	reopened.maxSize, reopened.minFree = w.maxSize, 0
	t.Cleanup(func() { _ = reopened.Close() })
	if _, err := reopened.Write([]byte("666667")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(w.path + ".1")
	if err != nil || string(data) != "55566666" {
		t.Fatalf("restart did not append/rotate: %q, %v", data, err)
	}
	assertLogBounds(t, reopened)
}

func TestProcessLogLowSpaceDrainsAndRecovers(t *testing.T) {
	w := smallProcessLog(t)
	w.maxSize, w.minFree = 64, 100
	if _, err := w.Write([]byte("before")); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= w.backups; i++ {
		if err := os.WriteFile(fmt.Sprintf("%s.%d", w.path, i), []byte("archive"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var messages bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&messages)
	t.Cleanup(func() { log.SetOutput(previous) })
	w.freeSpace = func(string) (uint64, error) { return 10, nil }
	for i := 0; i < 20; i++ {
		if n, err := w.Write([]byte("discarded")); err != nil || n != 9 {
			t.Fatalf("disk pressure must not close the child pipe: %d, %v", n, err)
		}
	}
	for i := 1; i <= w.backups; i++ {
		if _, err := os.Stat(fmt.Sprintf("%s.%d", w.path, i)); !os.IsNotExist(err) {
			t.Fatalf("archive not reclaimed: %d, %v", i, err)
		}
	}
	data, err := os.ReadFile(w.path)
	if err != nil || string(data) != "before" {
		t.Fatalf("low space changed active log: %q, %v", data, err)
	}
	if count := strings.Count(messages.String(), "capture unavailable"); count != 1 {
		t.Fatalf("warning flood: %d messages", count)
	}
	w.freeSpace = func(string) (uint64, error) { return 1000, nil }
	if _, err := w.Write([]byte("after")); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(w.path)
	if err != nil || string(data) != "beforeafter" || !strings.Contains(messages.String(), "discarded 180 bytes") {
		t.Fatalf("capture did not recover: %q, %v, %s", data, err, messages.String())
	}
}

func TestProcessLogReclaimsOldestArchiveFirst(t *testing.T) {
	w := smallProcessLog(t)
	w.maxSize, w.minFree = 64, 100
	if _, err := w.Write([]byte("active")); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= w.backups; i++ {
		if err := os.WriteFile(fmt.Sprintf("%s.%d", w.path, i), []byte("archive"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	w.freeSpace = func(string) (uint64, error) {
		if _, err := os.Stat(w.path + ".3"); os.IsNotExist(err) {
			return 200, nil
		}
		return 10, nil
	}
	if _, err := w.Write([]byte("more")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.path + ".2"); err != nil {
		t.Fatal("newer archive was deleted unnecessarily", err)
	}
	data, err := os.ReadFile(w.path)
	if err != nil || string(data) != "activemore" {
		t.Fatalf("capture did not resume after reclamation: %q, %v", data, err)
	}
}

func TestProcessLogSpaceFluctuationsDoNotFloodAgentLogs(t *testing.T) {
	w := smallProcessLog(t)
	w.maxSize, w.minFree = 64, 100
	var messages bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&messages)
	t.Cleanup(func() { log.SetOutput(previous) })
	var available uint64
	w.freeSpace = func(string) (uint64, error) { return available, nil }
	for i := 0; i < 20; i++ {
		available = 10
		_, _ = w.Write([]byte("drop"))
		available = 1000
		_, _ = w.Write([]byte("ok"))
	}
	if strings.Count(messages.String(), "capture unavailable") != 1 || strings.Count(messages.String(), "capture resumed") != 1 {
		t.Fatalf("space fluctuations flooded the agent log: %s", messages.String())
	}
}

func TestProcessLogUnavailableDirectoryAndConcurrentWrites(t *testing.T) {
	w := smallProcessLog(t)
	dir := filepath.Dir(w.path)
	if err := os.WriteFile(dir, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if n, err := w.Write([]byte("drop")); n != 4 || err != nil {
		t.Fatalf("file error escaped into child pipe: %d, %v", n, err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if _, err := w.Write(bytes.Repeat([]byte("x"), 17)); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	assertLogBounds(t, w)
}
