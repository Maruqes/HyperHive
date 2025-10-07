package info

import "github.com/shirou/gopsutil/v4/mem"

type MemInfoStruct struct{}

var MemInfo MemInfoStruct

func (m *MemInfoStruct) GetMemUsage() (usedPerc float64, freePerc float64, err error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		panic(err)
	}
	usedPerc = v.UsedPercent
	freePerc = 100 - usedPerc
	return usedPerc, freePerc, nil
}

func (m *MemInfoStruct) GetMemTotalMB() (int, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		panic(err)
	}
	return int(v.Total / 1024 / 1024), nil
}

func (m *MemInfoStruct) GetMemUsedMB() (int, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		panic(err)
	}
	return int(v.Used / 1024 / 1024), nil
}

func (m *MemInfoStruct) GetMemFreeMB() (int, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		panic(err)
	}
	return int(v.Free / 1024 / 1024), nil
}
