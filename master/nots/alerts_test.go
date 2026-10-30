package nots

import (
	"context"
	"testing"
	"time"
)

func TestAlertEngineStateTransitions(t *testing.T) {
	now := time.Now()
	var sent []Notification
	e := NewAlertEngine(func(_ context.Context, n Notification) error { sent = append(sent, n); return nil }, 2, 6*time.Hour)
	e.now = func() time.Time { return now }
	observe := func(level AlertLevel) {
		_ = e.Observe(context.Background(), AlertSample{Key: "k", Event: "metrics.memory", Level: level, Title: "node1 memory"})
	}

	observe(AlertError) // not yet sustained
	if len(sent) != 0 {
		t.Fatal("alert before sustain")
	}
	observe(AlertError)
	if len(sent) != 1 || sent[0].Severity != SeverityError {
		t.Fatalf("alert start: %+v", sent)
	}
	for i := 0; i < 10; i++ {
		observe(AlertError) // steady state stays quiet until the reminder
	}
	if len(sent) != 1 {
		t.Fatal("repeated alert while active")
	}
	now = now.Add(6 * time.Hour)
	observe(AlertError)
	if len(sent) != 2 || sent[1].Title != "Still active: node1 memory" {
		t.Fatalf("reminder: %+v", sent)
	}
	observe(AlertWarning)
	observe(AlertWarning) // de-escalation is silent
	if len(sent) != 2 {
		t.Fatal("de-escalation notified")
	}
	observe(AlertOK)
	observe(AlertOK)
	if len(sent) != 3 || sent[2].Severity != SeverityInfo || sent[2].Title != "Resolved: node1 memory" {
		t.Fatalf("resolve: %+v", sent)
	}
}

func TestAlertEngineFlappingDoesNotNotify(t *testing.T) {
	var sent int
	e := NewAlertEngine(func(context.Context, Notification) error { sent++; return nil }, 2, time.Hour)
	for i := 0; i < 10; i++ {
		level := AlertOK
		if i%2 == 0 {
			level = AlertError
		}
		_ = e.Observe(context.Background(), AlertSample{Key: "k", Level: level})
	}
	if sent != 0 {
		t.Fatalf("flapping sent %d", sent)
	}
}
