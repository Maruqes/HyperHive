package client

import (
	"context"
	"errors"
)

const PowerSchedulePath = "/power-schedule"

// PowerSchedule is the cluster-wide schedule; Start and Stop are HH:MM in the master's zone.
type PowerSchedule struct {
	Enabled   bool   `json:"enabled"`
	Start     string `json:"start"`
	Stop      string `json:"stop"`
	TZ        string `json:"tz"`
	UpdatedAt string `json:"updatedAt"`
}

// PowerScheduleWindow is the next off window in UTC epoch seconds.
type PowerScheduleWindow struct {
	StopUnix       int64 `json:"stopUnix"`
	MasterStopUnix int64 `json:"masterStopUnix"`
	WakeUnix       int64 `json:"wakeUnix"`
}

// PowerScheduleHost is the last apply recorded for one machine.
type PowerScheduleHost struct {
	Machine     string `json:"machine"`
	Connected   bool   `json:"connected"`
	Enabled     bool   `json:"enabled"`
	StopUnix    int64  `json:"stopUnix"`
	WakeUnix    int64  `json:"wakeUnix"`
	JobID       string `json:"jobId"`
	AppliedAt   string `json:"appliedAt"`
	LastError   string `json:"lastError"`
	AttemptedAt string `json:"attemptedAt"`
}

type PowerScheduleView struct {
	Schedule   PowerSchedule        `json:"schedule"`
	MasterTZ   string               `json:"masterTz"`
	MasterTime string               `json:"masterTime"`
	Next       *PowerScheduleWindow `json:"next,omitempty"`
	Hosts      []PowerScheduleHost  `json:"hosts"`
}

func (c *Client) GetPowerSchedule(ctx context.Context) (PowerScheduleView, error) {
	var view PowerScheduleView
	if err := c.getJSON(ctx, PowerSchedulePath, &view); err != nil {
		return PowerScheduleView{}, err
	}
	if view.Hosts == nil {
		return PowerScheduleView{}, errors.New("HyperHive API returned an invalid power schedule")
	}
	return view, nil
}
