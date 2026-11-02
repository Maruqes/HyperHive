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

// Shapes emitted by the frontend condition builder (rows joined left to right, see
// HyperHiveFront utils/condition-group.ts). Deps follow short-circuiting, and any error
// makes the whole condition false, so the result depends on row order.
func TestEvaluateConditionGroups(t *testing.T) {
	data := fakeConditionData{
		"db|":  {ID: "id-db", Label: "db", Fields: map[string]any{"health": "healthy"}, Streams: map[string]any{"cpu": 97.0}},
		"web|": {ID: "id-web", Label: "web", Fields: map[string]any{"connectivity": "offline"}, Streams: map[string]any{}},
	}
	const hot, low, down, up, missing = "(app('db').cpu > 90)", "(app('db').cpu < 50)", "(app('web').connectivity == 'offline')", "(app('web').connectivity == 'online')", "(app('db').missing > 1)"
	cases := []struct {
		source string
		value  bool
		err    string
		deps   []string
	}{
		{hot + " && " + down, true, "", []string{"id-db", "id-web"}},
		{low + " || " + down, true, "", []string{"id-db", "id-web"}},
		{low + " && " + down, false, "", []string{"id-db"}},
		{hot + " && !" + up, true, "", []string{"id-db", "id-web"}},
		{low + " || !" + up, true, "", []string{"id-db", "id-web"}},
		{hot + " != " + down, false, "", []string{"id-db", "id-web"}},
		{"!(" + hot + " && " + down + ")", false, "", []string{"id-db", "id-web"}},
		{"(" + hot + " && " + down + ") != !" + low, false, "", []string{"id-db", "id-web"}},
		{"((" + low + " || " + down + ") && !!" + up + ") || " + hot, true, "", []string{"id-db", "id-web"}},
		{missing + " || " + down, false, "missing", []string{"id-db"}},
		{down + " || " + missing, true, "", []string{"id-web"}},
	}
	for _, test := range cases {
		result, deps := EvaluateCondition(test.source, data)
		if result.Value == nil || *result.Value != test.value || !strings.Contains(result.Error, test.err) || (test.err == "" && result.Error != "") {
			t.Fatalf("%s: value=%v error=%q", test.source, result.Value, result.Error)
		}
		if len(deps) != len(test.deps) {
			t.Fatalf("%s: deps=%v want %v", test.source, deps, test.deps)
		}
		for _, id := range test.deps {
			if !deps[id] {
				t.Fatalf("%s: deps=%v want %v", test.source, deps, test.deps)
			}
		}
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
