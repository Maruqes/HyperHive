package info

type DiskInfoStruct struct{}

var DiskInfo DiskInfoStruct

func (d *DiskInfoStruct) GetDisks() ([]string, error) {
	return []string{}, nil
}

// % of used/free
func (d *DiskInfoStruct) GetDiskUsage() ([]float64, error) {
	return []float64{}, nil
}

func (d *DiskInfoStruct) GetDiskIOUsage() ([]float64, error) {
	return []float64{}, nil
}
