package client

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

const DiskHealthPath = "/diskhealth"

// DiskHealthSample is one stored SMART reading.
type DiskHealthSample struct {
	CapturedAt              string `json:"capturedAt"`
	MachineName             string `json:"machineName"`
	Device                  string `json:"device"`
	Score                   *int   `json:"score"`
	TemperatureC            int64  `json:"temperatureC"`
	PowerOnHours            int64  `json:"powerOnHours"`
	PendingSectors          int64  `json:"pendingSectors"`
	OfflineUncorrectable    int64  `json:"offlineUncorrectable"`
	ReallocatedSectors      int64  `json:"reallocatedSectors"`
	ReportedUncorrectable   int64  `json:"reportedUncorrectable"`
	MediaErrors             int64  `json:"mediaErrors"`
	CRCErrorCount           int64  `json:"crcErrorCount"`
	PercentageUsed          int64  `json:"percentageUsed"`
	AvailableSpare          int64  `json:"availableSpare"`
	AvailableSpareThreshold int64  `json:"availableSpareThreshold"`
	CriticalWarning         int64  `json:"criticalWarning"`
	SmartPassed             bool   `json:"smartPassed"`
	SelfTestFailed          bool   `json:"selfTestFailed"`
	HealthStatus            string `json:"healthStatus"`
	SmartctlError           string `json:"smartctlError"`
}

// DiskHealthDisk is one physical disk keyed by serial, with its latest score.
type DiskHealthDisk struct {
	Serial             string            `json:"serial"`
	Model              string            `json:"model"`
	MachineName        string            `json:"machineName"`
	Device             string            `json:"device"`
	Transport          string            `json:"transport"`
	Rotational         bool              `json:"rotational"`
	CapacityBytes      int64             `json:"capacityBytes"`
	FirstSeen          string            `json:"firstSeen"`
	LastSeen           string            `json:"lastSeen"`
	RetiredAt          *string           `json:"retiredAt"`
	SmartAvailable     bool              `json:"smartAvailable"`
	Score              *int              `json:"score"`
	Band               string            `json:"band"`
	WearOutAt          *string           `json:"wearOutAt"`
	Reasons            []string          `json:"reasons"`
	PastRatedEndurance bool              `json:"pastRatedEndurance"`
	Latest             *DiskHealthSample `json:"latest,omitempty"`
}

type DiskHealthList struct {
	LastPollAt *string          `json:"lastPollAt"`
	Disks      []DiskHealthDisk `json:"disks"`
}

// DiskHealthPoint is a raw 6-hourly sample ("6h") or a compacted day ("daily").
type DiskHealthPoint struct {
	At                    string `json:"at"`
	Resolution            string `json:"resolution"`
	Score                 *int   `json:"score"`
	Band                  string `json:"band"`
	TemperatureC          int64  `json:"temperatureC"`
	PowerOnHours          int64  `json:"powerOnHours"`
	PendingSectors        int64  `json:"pendingSectors"`
	OfflineUncorrectable  int64  `json:"offlineUncorrectable"`
	ReallocatedSectors    int64  `json:"reallocatedSectors"`
	ReportedUncorrectable int64  `json:"reportedUncorrectable"`
	MediaErrors           int64  `json:"mediaErrors"`
	CRCErrorCount         int64  `json:"crcErrorCount"`
	PercentageUsed        int64  `json:"percentageUsed"`
	SmartPassed           bool   `json:"smartPassed"`
}

type DiskHealthHistory struct {
	Disk   DiskHealthDisk    `json:"disk"`
	Points []DiskHealthPoint `json:"points"`
}

type DiskHealthListInput struct {
	State string `json:"state,omitempty" jsonschema:"Which disks to list: active, retired or all (default all)"`
}

type DiskHealthHistoryInput struct {
	Serial string `json:"serial" jsonschema:"Disk serial number, as returned by list_disk_health"`
	Days   int    `json:"days,omitempty" jsonschema:"Only return points from the last N days (1-3650); omit for all history"`
}

func (c *Client) ListDiskHealth(ctx context.Context, input DiskHealthListInput) (DiskHealthList, error) {
	path := DiskHealthPath + "/"
	switch input.State {
	case "":
	case "active", "retired", "all":
		path += "?state=" + input.State
	default:
		return DiskHealthList{}, errors.New("state must be active, retired or all")
	}
	var list DiskHealthList
	if err := c.getJSON(ctx, path, &list); err != nil {
		return DiskHealthList{}, err
	}
	if list.Disks == nil {
		return DiskHealthList{}, errors.New("HyperHive API returned an invalid disk health list")
	}
	return list, nil
}

func (c *Client) GetDiskHealthHistory(ctx context.Context, input DiskHealthHistoryInput) (DiskHealthHistory, error) {
	serial := strings.TrimSpace(input.Serial)
	if serial == "" {
		return DiskHealthHistory{}, errors.New("serial is required")
	}
	if input.Days < 0 || input.Days > 3650 {
		return DiskHealthHistory{}, errors.New("days must be 1..3650")
	}
	path := DiskHealthPath + "/" + url.PathEscape(serial)
	if input.Days > 0 {
		path += "?days=" + strconv.Itoa(input.Days)
	}
	var history DiskHealthHistory
	if err := c.getJSON(ctx, path, &history); err != nil {
		return DiskHealthHistory{}, err
	}
	if history.Disk.Serial == "" || history.Points == nil {
		return DiskHealthHistory{}, errors.New("HyperHive API returned an invalid disk health history")
	}
	return history, nil
}
