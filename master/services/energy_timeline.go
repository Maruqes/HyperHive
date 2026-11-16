package services

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"512SvMan/db"
)

// ErrEnergyTimelineInput marks a bad range, kind or name; the API answers 400.
var ErrEnergyTimelineInput = errors.New("invalid energy timeline request")

// EnergyTimeline is the /energy/timeline response. Every series has Count
// values; index i is the bucket starting at Start + i*StepSeconds.
type EnergyTimeline struct {
	Start       int64                  `json:"start"`
	StepSeconds int64                  `json:"stepSeconds"`
	Count       int                    `json:"count"`
	Series      []EnergyTimelineSeries `json:"series"`
}

// EnergyTimelineSeries holds one host or VM. A null bucket means no data (the
// master or the host was down); a VM bucket of 0 means the VM was stopped.
type EnergyTimelineSeries struct {
	Kind     string     `json:"kind"`
	Machine  string     `json:"machine"`
	Name     string     `json:"name"`
	Wh       []*float64 `json:"wh"`
	RunningS []*float64 `json:"runningS"`
}

// Timeline returns stored energy buckets. With a name it is one series at the
// range's own step (name is the machine name when kind is "host"); without, it
// is every series at one hour. VM series are merged by name across machines.
func (s *EnergyService) Timeline(ctx context.Context, rangeKey, kind, machine, name string) (EnergyTimeline, error) {
	step, span := int64(0), int64(0)
	for _, tier := range energySampleTiers {
		if tier.key == rangeKey {
			step, span = tier.step, int64(tier.keep.Seconds())
		}
	}
	if step == 0 {
		return EnergyTimeline{}, fmt.Errorf("%w: range must be 30d, 90d or 6m", ErrEnergyTimelineInput)
	}
	if kind != "" && kind != energyKindHost && kind != energyKindVM {
		return EnergyTimeline{}, fmt.Errorf("%w: kind must be host or vm", ErrEnergyTimelineInput)
	}
	if name != "" && kind == "" {
		return EnergyTimeline{}, fmt.Errorf("%w: name needs kind", ErrEnergyTimelineInput)
	}
	if name == "" {
		step = energyHourStep
	}

	now := s.now().Unix()
	count := int(span / step)
	start := now - now%step - int64(count-1)*step
	timeline := EnergyTimeline{Start: start, StepSeconds: step, Count: count, Series: []EnergyTimelineSeries{}}

	// Host rows are always read: they tell a stopped VM from missing data.
	hostRows, err := db.GetEnergySamples(ctx, step, start, energyKindHost, "", "")
	if err != nil {
		return EnergyTimeline{}, fmt.Errorf("get host samples: %w", err)
	}
	var vmRows []db.EnergySample
	if kind != energyKindHost {
		if vmRows, err = db.GetEnergySamples(ctx, step, start, energyKindVM, machine, name); err != nil {
			return EnergyTimeline{}, fmt.Errorf("get vm samples: %w", err)
		}
	}

	series := map[string]*EnergyTimelineSeries{}
	add := func(kind, machine, name string, row db.EnergySample) {
		index := int((row.Ts - start) / step)
		if index < 0 || index >= count {
			return
		}
		key := kind + "\x00" + name
		entry := series[key]
		if entry == nil {
			entry = &EnergyTimelineSeries{Kind: kind, Machine: machine, Name: name, Wh: make([]*float64, count), RunningS: make([]*float64, count)}
			series[key] = entry
		}
		if entry.Wh[index] == nil {
			entry.Wh[index], entry.RunningS[index] = new(float64), new(float64)
		}
		*entry.Wh[index] += row.Wh
		*entry.RunningS[index] += row.RunningS
	}

	anyHost := make([]bool, count)
	for _, row := range hostRows {
		if index := int((row.Ts - start) / step); index >= 0 && index < count {
			anyHost[index] = true
		}
		wanted := name == "" || row.Machine == name
		if kind != energyKindVM && wanted && (machine == "" || row.Machine == machine) {
			add(energyKindHost, row.Machine, row.Machine, row)
		}
	}
	for _, row := range vmRows {
		add(energyKindVM, "", row.Name, row)
	}

	for _, entry := range series {
		if entry.Kind == energyKindVM {
			for i, seen := range anyHost {
				if seen && entry.Wh[i] == nil {
					entry.Wh[i], entry.RunningS[i] = new(float64), new(float64)
				}
			}
		}
		timeline.Series = append(timeline.Series, *entry)
	}
	sort.Slice(timeline.Series, func(i, j int) bool {
		a, b := timeline.Series[i], timeline.Series[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Name < b.Name
	})
	return timeline, nil
}
