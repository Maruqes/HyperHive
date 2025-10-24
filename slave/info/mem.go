package info

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/shirou/gopsutil/v4/mem"
)

type MemInfoStruct struct{}

var MemInfo MemInfoStruct

func (m *MemInfoStruct) GetMemUsage() (usedPerc float64, freePerc float64, err error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return 0, 0, err
	}
	usedPerc = v.UsedPercent
	freePerc = 100 - usedPerc
	return usedPerc, freePerc, nil
}

func (m *MemInfoStruct) GetMemTotalMB() (int, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	return int(v.Total / 1024 / 1024), nil
}

func (m *MemInfoStruct) GetMemUsedMB() (int, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	return int(v.Used / 1024 / 1024), nil
}

func (m *MemInfoStruct) GetMemFreeMB() (int, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	return int(v.Free / 1024 / 1024), nil
}

// MemSummary groups the main memory metrics together.
type MemSummary struct {
	UsedPercent float64 `json:"used_percent"`
	FreePercent float64 `json:"free_percent"`
	TotalMB     int     `json:"total_mb"`
	UsedMB      int     `json:"used_mb"`
	FreeMB      int     `json:"free_mb"`
}

// GetMemSummary returns a consolidated view of memory usage by calling the
// individual getters. It returns the first error encountered, if any.
func (m *MemInfoStruct) GetMemSummary() (*MemSummary, error) {
	usedPerc, freePerc, err := m.GetMemUsage()
	if err != nil {
		return nil, err
	}

	total, err := m.GetMemTotalMB()
	if err != nil {
		return nil, err
	}

	used, err := m.GetMemUsedMB()
	if err != nil {
		return nil, err
	}

	free, err := m.GetMemFreeMB()
	if err != nil {
		return nil, err
	}

	return &MemSummary{
		UsedPercent: usedPerc,
		FreePercent: freePerc,
		TotalMB:     total,
		UsedMB:      used,
		FreeMB:      free,
	}, nil
}

// cleanBackspaces removes backspace characters and processes the string properly
func cleanBackspaces(s string) string {
	// Convert string to rune slice for proper character handling
	runes := []rune(s)
	result := make([]rune, 0, len(runes))

	for _, r := range runes {
		if r == '\b' {
			// Remove the last character if backspace is encountered
			if len(result) > 0 {
				result = result[:len(result)-1]
			}
		} else {
			result = append(result, r)
		}
	}

	return string(result)
}

// sudo memtester 48G 2  //number of G and number of passes
func (c *MemInfoStruct) SressTestMem(ctx context.Context, numOfGigs, numOfPasses int) (string, error) {

	args := []string{
		fmt.Sprintf("%dG", numOfGigs),
		fmt.Sprintf("%d", numOfPasses),
	}

	logger.Info("Started memTest")
	cmd := exec.CommandContext(ctx, "memtester", args...)

	//logs every 5 mins q esta a correr
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				logger.Info("memtester is still running...")
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	out, err := cmd.CombinedOutput()
	close(done) // Signal the goroutine to stop

	if ctx.Err() != nil {
		// Context was canceled or deadline exceeded; ensure we return that explicitly.
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("memtester failed: %w\n--- memtester output ---\n%s", err, string(out))
	}

	// Clean backspaces from output
	cleanedOutput := cleanBackspaces(string(out))

	logger.Info("Finished memTest")
	logger.Info(cleanedOutput)
	return cleanedOutput, nil
}
