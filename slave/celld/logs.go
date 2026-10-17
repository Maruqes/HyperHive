package celld

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const (
	logMaxSize = 10 << 20
	logBackups = 3
	logMinFree = 256 << 20
)

// processLog consumes both output streams even when logging cannot use the disk.
type processLog struct {
	mu          sync.Mutex
	path        string
	file        *os.File
	size        int64
	maxSize     int64
	backups     int
	minFree     uint64
	freeSpace   func(string) (uint64, error)
	closed      bool
	dropped     uint64
	nextWarning time.Time
	warned      bool
}

func newProcessLog(dir string) *processLog {
	return &processLog{path: filepath.Join(dir, "logs", "celld.log"), maxSize: logMaxSize, backups: logBackups, minFree: logMinFree, freeSpace: freeLogSpace}
}

func freeLogSpace(dir string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

func (w *processLog) open() error {
	dir := filepath.Dir(w.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err == nil {
		err = f.Chmod(0600)
	}
	if err != nil {
		f.Close()
		return err
	}
	if info.Size() > w.maxSize {
		if err = f.Truncate(w.maxSize); err != nil {
			f.Close()
			return err
		}
		w.size = w.maxSize
	} else {
		w.size = info.Size()
	}
	w.file = f
	return nil
}

func (w *processLog) rotate() error {
	err := w.file.Close()
	w.file = nil
	if err != nil {
		return err
	}
	for i := w.backups; i >= 1; i-- {
		to := fmt.Sprintf("%s.%d", w.path, i)
		if i == w.backups {
			if err := os.Remove(to); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		from := w.path
		if i > 1 {
			from = fmt.Sprintf("%s.%d", w.path, i-1)
		}
		if err := os.Rename(from, to); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return w.open()
}

func (w *processLog) reserve(bytes int) error {
	dir := filepath.Dir(w.path)
	free, err := w.freeSpace(dir)
	if err != nil {
		return err
	}
	needed := w.minFree + uint64(bytes)
	if free >= needed {
		return nil
	}
	for i := w.backups; i >= 1; i-- {
		if err := os.Remove(fmt.Sprintf("%s.%d", w.path, i)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		free, err = w.freeSpace(dir)
		if err != nil {
			return err
		}
		if free >= needed {
			return nil
		}
	}
	return fmt.Errorf("only %d bytes available; preserving %d bytes of free space", free, w.minFree)
}

func (w *processLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	total := len(p)
	if w.closed {
		return total, nil
	}
	for len(p) > 0 {
		var err error
		if w.file == nil {
			err = w.open()
		}
		if err == nil && w.size == w.maxSize {
			err = w.rotate()
		}
		n := min(len(p), int(w.maxSize-w.size))
		if err == nil {
			err = w.reserve(n)
		}
		if err == nil {
			written, writeErr := w.file.Write(p[:n])
			w.size += int64(written)
			p = p[written:]
			err = writeErr
			if err == nil && written != n {
				err = io.ErrShortWrite
			}
		}
		if err != nil {
			if w.file != nil {
				_ = w.file.Close()
				w.file = nil
			}
			w.dropped += uint64(len(p))
			if now := time.Now(); !now.Before(w.nextWarning) {
				log.Printf("celld log capture unavailable (%s); discarding output: %v", w.path, err)
				w.nextWarning = now.Add(time.Minute)
				w.warned = true
			}
			// An io.Copy error would close the child's output pipe and can kill it.
			return total, nil
		}
	}
	if w.dropped > 0 {
		if w.warned {
			log.Printf("celld log capture resumed (%s); discarded %d bytes", w.path, w.dropped)
			w.warned = false
		}
		w.dropped = 0
	}
	return total, nil
}

func (w *processLog) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}
