package services

import (
	"testing"
	"time"

	"512SvMan/db"
)

func TestNextWindow(t *testing.T) {
	lisbon, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, lisbon) }

	cases := []struct {
		name        string
		now         time.Time
		start, stop string
		wantStop    time.Time
		wantWake    time.Time
	}{
		{"same day before stop", at(2026, 6, 10, 12, 0), "08:00", "23:00", at(2026, 6, 10, 23, 0), at(2026, 6, 11, 8, 0)},
		{"after stop uses tomorrow", at(2026, 6, 10, 23, 30), "08:00", "23:00", at(2026, 6, 11, 23, 0), at(2026, 6, 12, 8, 0)},
		{"exactly at stop uses tomorrow", at(2026, 6, 10, 23, 0), "08:00", "23:00", at(2026, 6, 11, 23, 0), at(2026, 6, 12, 8, 0)},
		{"stop after midnight", at(2026, 6, 10, 12, 0), "08:00", "01:00", at(2026, 6, 11, 1, 0), at(2026, 6, 11, 8, 0)},
		{"inside off window waits for next stop", at(2026, 6, 11, 2, 0), "08:00", "01:00", at(2026, 6, 12, 1, 0), at(2026, 6, 12, 8, 0)},
		{"day window", at(2026, 6, 10, 7, 0), "18:00", "09:00", at(2026, 6, 10, 9, 0), at(2026, 6, 10, 18, 0)},
		// Europe/Lisbon falls back at 02:00 on 2026-10-25: the off night is one hour longer.
		{"dst fall back", at(2026, 10, 24, 12, 0), "08:00", "23:00", at(2026, 10, 24, 23, 0), at(2026, 10, 25, 8, 0)},
		// Springs forward at 01:00 on 2026-03-29: 01:30 does not exist and normalizes to 02:30.
		{"dst spring forward gap", at(2026, 3, 28, 12, 0), "08:00", "01:30", time.Date(2026, 3, 29, 2, 30, 0, 0, lisbon), at(2026, 3, 29, 8, 0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stop, wake, err := nextWindow(tc.now, tc.start, tc.stop, lisbon)
			if err != nil {
				t.Fatalf("nextWindow: %v", err)
			}
			if !stop.Equal(tc.wantStop) || !wake.Equal(tc.wantWake) {
				t.Fatalf("got stop %s wake %s, want stop %s wake %s", stop, wake, tc.wantStop, tc.wantWake)
			}
			if !stop.After(tc.now) || !wake.After(stop) {
				t.Fatalf("window not ordered: now %s stop %s wake %s", tc.now, stop, wake)
			}
		})
	}

	if got := at(2026, 10, 25, 8, 0).Sub(at(2026, 10, 24, 23, 0)); got != 10*time.Hour {
		t.Fatalf("fall-back night = %s, want 10h", got)
	}
}

func TestNextWindowRejectsInvalid(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	for _, tc := range [][2]string{{"08:00", "08:00"}, {"8:00", "23:00"}, {"08:00", "24:00"}, {"", "23:00"}} {
		if _, _, err := nextWindow(now, tc[0], tc[1], time.UTC); err == nil {
			t.Fatalf("nextWindow(%q, %q) accepted", tc[0], tc[1])
		}
	}
}

func TestValidatePowerSchedule(t *testing.T) {
	valid := [][2]string{{"08:00", "23:00"}, {"08:00", "01:00"}, {"00:10", "00:00"}}
	for _, tc := range valid {
		if err := ValidatePowerSchedule(tc[0], tc[1]); err != nil {
			t.Fatalf("ValidatePowerSchedule(%q, %q): %v", tc[0], tc[1], err)
		}
	}
	invalid := [][2]string{{"08:00", "08:00"}, {"08:05", "08:00"}, {"00:05", "23:58"}, {"8:00", "23:00"}, {"08:60", "23:00"}, {"ab:cd", "23:00"}}
	for _, tc := range invalid {
		if err := ValidatePowerSchedule(tc[0], tc[1]); err == nil {
			t.Fatalf("ValidatePowerSchedule(%q, %q) accepted", tc[0], tc[1])
		}
	}
}

func TestNeedsReapply(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	enabled := db.PowerSchedule{Enabled: true, Start: "08:00", Stop: "23:00"}
	disabled := db.PowerSchedule{Enabled: false, Start: "08:00", Stop: "23:00"}
	future := now.Add(time.Hour).Unix()
	justPassed := now.Add(-5 * time.Minute).Unix()
	longPassed := now.Add(-time.Hour).Unix()

	cases := []struct {
		name     string
		schedule db.PowerSchedule
		status   *db.PowerScheduleStatus
		want     bool
	}{
		{"never applied", enabled, nil, true},
		{"never applied disabled", disabled, nil, true},
		{"applied future stop", enabled, &db.PowerScheduleStatus{Enabled: true, StopUnix: future}, false},
		{"stop passed within grace", enabled, &db.PowerScheduleStatus{Enabled: true, StopUnix: justPassed}, false},
		{"stop passed and still online", enabled, &db.PowerScheduleStatus{Enabled: true, StopUnix: longPassed}, true},
		{"last attempt failed", enabled, &db.PowerScheduleStatus{Enabled: true, StopUnix: future, LastError: "boom"}, true},
		{"enabled but host has disabled", enabled, &db.PowerScheduleStatus{Enabled: false}, true},
		{"disabled but host still enabled", disabled, &db.PowerScheduleStatus{Enabled: true, StopUnix: future}, true},
		{"disabled and clean", disabled, &db.PowerScheduleStatus{Enabled: false}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsReapply(tc.schedule, tc.status, now); got != tc.want {
				t.Fatalf("needsReapply = %v, want %v", got, tc.want)
			}
		})
	}
}
