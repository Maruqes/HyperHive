package nots

import (
	"context"
	"sync"
	"time"
)

// AlertLevel is the state of a metric alert.
type AlertLevel int

const (
	AlertOK AlertLevel = iota
	AlertWarning
	AlertError
)

func (l AlertLevel) severity() Severity {
	if l == AlertError {
		return SeverityError
	}
	return SeverityWarning
}

// AlertSample is one observation of a metric, already classified by the caller.
type AlertSample struct {
	Key     string // one alert per key, e.g. "metrics.cpu_temperature:node1"
	Event   string
	Machine string
	Level   AlertLevel
	Title   string // describes the problem, e.g. "node1 CPU 88°C (max core)"
	Body    string
	URL     string
}

type alertState struct {
	level    AlertLevel
	pending  AlertLevel
	count    int
	lastSent time.Time
}

// AlertEngine turns repeated metric checks into state changes.
// It notifies when an alert starts or escalates, sends "resolved" on recovery and
// reminds only after a long cooldown. State is in memory for the process lifetime.
type AlertEngine struct {
	notify   func(context.Context, Notification) error
	now      func() time.Time
	sustain  int           // consecutive samples needed before a state change
	reminder time.Duration // repeat interval while an alert stays active

	mu     sync.Mutex
	states map[string]*alertState
}

func NewAlertEngine(notify func(context.Context, Notification) error, sustain int, reminder time.Duration) *AlertEngine {
	if sustain < 1 {
		sustain = 1
	}
	return &AlertEngine{notify: notify, now: time.Now, sustain: sustain, reminder: reminder, states: map[string]*alertState{}}
}

// Observe records a sample and sends a notification when the alert state changes.
func (e *AlertEngine) Observe(ctx context.Context, s AlertSample) error {
	n, send := e.step(s)
	if !send {
		return nil
	}
	return e.notify(ctx, n)
}

func (e *AlertEngine) step(s AlertSample) (Notification, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	st := e.states[s.Key]
	if st == nil {
		st = &alertState{}
		e.states[s.Key] = st
	}
	n := Notification{Event: s.Event, Machine: s.Machine, Title: s.Title, Body: s.Body, URL: s.URL}

	if s.Level == st.level {
		st.count = 0
		if st.level != AlertOK && e.reminder > 0 && now.Sub(st.lastSent) >= e.reminder {
			st.lastSent = now
			n.Severity = st.level.severity()
			n.Title = "Still active: " + s.Title
			return n, true
		}
		return n, false
	}

	if s.Level == st.pending {
		st.count++
	} else {
		st.pending, st.count = s.Level, 1
	}
	if st.count < e.sustain {
		return n, false
	}

	previous := st.level
	st.level, st.count = s.Level, 0
	switch {
	case s.Level == AlertOK:
		st.lastSent = now
		n.Severity = SeverityInfo
		n.Title = "Resolved: " + s.Title
		return n, true
	case s.Level > previous:
		st.lastSent = now
		n.Severity = s.Level.severity()
		return n, true
	default:
		// Dropping from error to warning is not news; the reminder clock keeps running.
		return n, false
	}
}
