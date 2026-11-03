package btrfs

import (
	"encoding/json"
	"testing"
)

func TestParseBoolJSON(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"json true", `true`, true},
		{"json false", `false`, false},
		{"number 1", `1`, true},
		{"number 0", `0`, false},
		{"string true", `"true"`, true},
		{"string false", `"false"`, false},
		{"string 1", `"1"`, true},
		{"string 0", `"0"`, false},
		{"string yes", `" yes "`, true},
		{"empty", ``, false},
		{"null", `null`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseBoolJSON(json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("parseBoolJSON(%s) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
