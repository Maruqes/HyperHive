package info

import (
	"context"
	"fmt"
	"os/exec"

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

// sudo memtester 48G 2  //number of G and number of passes
func (c *MemInfoStruct) SressTestMem(ctx context.Context, numOfGigs, numOfPasses int) (string, error) {

	args := []string{
		fmt.Sprintf("%dG", numOfGigs),
		fmt.Sprintf("%d", numOfPasses),
	}

	cmd := exec.CommandContext(ctx, "memtester", args...)

	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		// Context was canceled or deadline exceeded; ensure we return that explicitly.
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("memtester failed: %w\n--- memtester output ---\n%s", err, string(out))
	}

	return string(out), nil
}
