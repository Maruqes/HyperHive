package domain

import (
	"errors"
	"strings"
	"testing"
)

type fakeConditionData map[string]*AppView

func (data fakeConditionData) AppView(name, instance string) (*AppView, error) {
	view, ok := data[strings.ToLower(name)+"|"+instance]
	if !ok {
		return nil, errors.New("unknown App " + name)
	}
	return view, nil
}

func TestEvaluateCondition(t *testing.T) {
	data := fakeConditionData{
		"db|":   {ID: "id-db", Label: "db", Fields: map[string]any{"health": "healthy", "connectivity": "online"}, Streams: map[string]any{"cpu": 97.0, "disk.used": 10.0}},
		"db|i1": {ID: "id-db", Label: "db[i1]", Fields: map[string]any{"health": "degraded"}, Streams: map[string]any{"cpu": 5.0}},
		"web|":  {ID: "id-web", Label: "web", Fields: map[string]any{"connectivity": "offline"}, Streams: map[string]any{}},
	}
	cases := []struct {
		source string
		value  bool
		err    string
	}{
		{"app('db').cpu >= 95", true, ""},
		{"app('DB').cpu >= 99", false, ""},
		{"app('db')['disk.used'] < 50 && app('db').health == 'healthy'", true, ""},
		{"app('db', 'i1').cpu > 1 && app('db','i1').health == 'degraded'", true, ""},
		{"app('db').streams['cpu'] > 1", true, ""},
		{"app('web').connectivity == 'offline'", true, ""},
		{"app('web').cpu > 1", false, "no current value"},
		{"app('nope').cpu > 1", false, "unknown App"},
		{"app('db').cpu", false, "bool"},
	}
	for _, test := range cases {
		result, deps := EvaluateCondition(test.source, data)
		if result.Value == nil || *result.Value != test.value || !strings.Contains(result.Error, test.err) || (test.err == "" && result.Error != "") {
			t.Fatalf("%s: value=%v error=%q", test.source, result.Value, result.Error)
		}
		if test.err == "" && len(deps) == 0 {
			t.Fatalf("%s: no deps", test.source)
		}
	}
	result, _ := EvaluateCondition("app('db').cpu >= 95", data)
	if result.Snapshot["db.cpu"] != 97.0 {
		t.Fatalf("snapshot: %v", result.Snapshot)
	}
}

func TestValidateAutomationRejectsBadCondition(t *testing.T) {
	automation := Automation{Name: "x", Timeout: 1, MaxRunsPerHour: 1, Condition: "app('db').cpu >="}
	if err := ValidateAutomation(automation); err == nil {
		t.Fatal("expected syntax error")
	}
	automation.Condition = "1 + 1"
	if err := ValidateAutomation(automation); err == nil {
		t.Fatal("expected non-boolean error")
	}
	automation.Condition = "app('db').cpu >= 95 || app('web', 'a').health == 'down'"
	if err := ValidateAutomation(automation); err != nil {
		t.Fatal(err)
	}
}
