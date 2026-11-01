package domain

import (
	"errors"
	"fmt"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/vm"
)

// AppView is what app('name'[, 'instance']) exposes to a condition. Streams
// holds only current values: values whose sender is offline are left out.
type AppView struct {
	ID      string
	Label   string
	Fields  map[string]any
	Streams map[string]any
}

// ConditionData resolves an App by name (case-insensitive) or ID, optionally
// narrowed to one instance.
type ConditionData interface {
	AppView(nameOrID, instance string) (*AppView, error)
}

type conditionRun struct {
	data     ConditionData
	snapshot map[string]any
	deps     map[string]bool
}

// fieldAccess rewrites app(...).x and app(...)['x'] into field(app(...), 'x')
// so a missing or stale value is a clear error instead of a nil comparison.
type fieldAccess struct{}

func (fieldAccess) Visit(node *ast.Node) {
	member, ok := (*node).(*ast.MemberNode)
	if !ok {
		return
	}
	call, ok := member.Node.(*ast.CallNode)
	if !ok {
		return
	}
	if callee, ok := call.Callee.(*ast.IdentifierNode); !ok || callee.Value != "app" {
		return
	}
	ast.Patch(node, &ast.CallNode{Callee: &ast.IdentifierNode{Value: "field"}, Arguments: []ast.Node{member.Node, member.Property}})
}

func (run *conditionRun) app(params ...any) (any, error) {
	name, _ := params[0].(string)
	instance := ""
	if len(params) > 1 {
		instance, _ = params[1].(string)
	}
	if run.data == nil {
		return nil, errors.New("no data")
	}
	view, err := run.data.AppView(name, instance)
	if err != nil {
		return nil, err
	}
	run.deps[view.ID] = true
	return view, nil
}

func (run *conditionRun) field(params ...any) (any, error) {
	view, ok := params[0].(*AppView)
	key, isString := params[1].(string)
	if !ok || !isString {
		return nil, errors.New("app fields must be accessed by name")
	}
	var value any
	var found bool
	if key == "streams" {
		value, found = view.Streams, true
	} else if value, found = view.Fields[key]; !found {
		value, found = view.Streams[key]
	}
	if !found {
		return nil, fmt.Errorf("%s has no current value for %q (missing, or its sender is offline)", view.Label, key)
	}
	if key != "streams" {
		run.snapshot[view.Label+"."+key] = value
	}
	return value, nil
}

// CompileCondition parses and type-checks a condition against run's functions.
func CompileCondition(source string, run *conditionRun) (*vm.Program, error) {
	if run == nil {
		run = &conditionRun{snapshot: map[string]any{}, deps: map[string]bool{}}
	}
	return expr.Compile(source,
		expr.Env(map[string]any{}),
		expr.AsBool(),
		expr.Patch(fieldAccess{}),
		expr.Function("app", run.app, new(func(string) any), new(func(string, string) any)),
		expr.Function("field", run.field, new(func(any, string) any)),
	)
}

// EvaluateCondition runs source against data. Any error evaluates to false.
// It returns the result and the IDs of the Apps the condition read.
func EvaluateCondition(source string, data ConditionData) (ConditionResult, map[string]bool) {
	run := &conditionRun{data: data, snapshot: map[string]any{}, deps: map[string]bool{}}
	result := ConditionResult{Snapshot: run.snapshot}
	program, err := CompileCondition(source, run)
	if err == nil {
		var output any
		if output, err = expr.Run(program, map[string]any{}); err == nil {
			if value, ok := output.(bool); ok {
				result.Value = &value
				return result, run.deps
			}
			err = fmt.Errorf("condition must return a boolean, got %T", output)
		}
	}
	value := false
	result.Value = &value
	result.Error = err.Error()
	return result, run.deps
}
