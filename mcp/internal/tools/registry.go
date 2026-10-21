package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Maruqes/HyperHive/mcp/internal/safety"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Registry owns policy state for one server, shared by all feature areas.
// Registration is completed before the server starts accepting requests.
type Registry struct {
	server  *mcp.Server
	gate    *safety.Gate
	reviews *safety.ReviewStore
	state   *registrationState
	area    string
}

type registrationState struct {
	names map[string]bool
	err   error
}

func NewRegistry(server *mcp.Server, reviewDir string) *Registry {
	return &Registry{server: server, gate: &safety.Gate{}, reviews: &safety.ReviewStore{Dir: reviewDir}, state: &registrationState{names: map[string]bool{}}}
}

// Area returns a scoped view without copying approval or review state.
func (r *Registry) Area(name string) *Registry { scoped := *r; scoped.area = name; return &scoped }
func (r *Registry) Err() error                 { return r.state.err }

func (r *Registry) fail(err error) error {
	if r.state.err == nil {
		r.state.err = err
	}
	return err
}

// ToolDefinition prepares schemas and a typed handler without registering it or
// assigning safety policy. Only the area's Register function selects the level.
type ToolDefinition struct {
	name string
	bind func(*Registry, safety.Level) error
}

func defineTool[In, Out any](tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) ToolDefinition {
	if tool == nil || handler == nil {
		return ToolDefinition{}
	}
	return ToolDefinition{name: tool.Name, bind: func(r *Registry, level safety.Level) error {
		return registerTypedTool(r, tool, handler, level)
	}}
}

// Recovery handlers need the selected level in their retained records. Defer
// that binding until the area catalog registers the definition.
func defineToolWithLevel[In, Out any](tool *mcp.Tool, handler func(safety.Level) mcp.ToolHandlerFor[In, Out]) ToolDefinition {
	if tool == nil || handler == nil {
		return ToolDefinition{}
	}
	return ToolDefinition{name: tool.Name, bind: func(r *Registry, level safety.Level) error {
		return registerTypedTool(r, tool, handler(level), level)
	}}
}

// RegisterTool keeps every tool name and its single safety declaration together
// in RegisterVMs, RegisterCelld, RegisterDNS, and the other area catalogs.
func RegisterTool(r *Registry, name string, definition ToolDefinition, level safety.Level) error {
	if r == nil || r.state == nil {
		return errors.New("MCP registry is unavailable")
	}
	if r.Err() != nil {
		return r.Err()
	}
	if name == "" || name != definition.name || definition.bind == nil || !level.Valid() {
		return r.fail(errors.New("MCP registration requires a matching tool definition and explicit safety level"))
	}
	return definition.bind(r, level)
}

// registerTypedTool binds the prepared typed handler to the common SDK and
// execution-policy wrapper. It is called only by the definition binders above.
func registerTypedTool[In, Out any](r *Registry, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out], level safety.Level) error {
	if r == nil || r.server == nil || r.state == nil {
		return errors.New("MCP registry is unavailable")
	}
	if r.Err() != nil {
		return r.Err()
	}
	if tool == nil || tool.Name == "" || handler == nil || r.area == "" || !level.Valid() {
		return r.fail(errors.New("MCP registration requires a named tool, area, handler and explicit safety level"))
	}
	if r.state.names[tool.Name] {
		return r.fail(fmt.Errorf("duplicate MCP tool: %s", tool.Name))
	}
	if tool.Meta == nil {
		tool.Meta = mcp.Meta{}
	}
	endpoint, ok := tool.Meta["hyperhive/endpoint"].(map[string]any)
	method, methodOK := endpoint["method"].(string)
	path, pathOK := endpoint["path"].(string)
	if !ok || !methodOK || !pathOK || method == "" || !strings.HasPrefix(path, "/") {
		return r.fail(fmt.Errorf("MCP tool %s requires endpoint metadata", tool.Name))
	}
	if endpoint["method"] == "DELETE" {
		return r.fail(fmt.Errorf("MCP deletion tool %s is excluded", tool.Name))
	}
	r.state.names[tool.Name] = true
	tool.Meta["hyperhive/risk"] = string(level)
	tool.Meta["hyperhive/area"] = r.area
	tool.Meta["hyperhive/required_action"] = level.RequiredAction()
	tool.Description = "Risk: " + string(level) + ". " + level.Instruction() + " " + tool.Description
	// Capture immutable policy data, rather than trusting caller-supplied metadata.
	name, impact := tool.Name, tool.Description
	err := addExactTool(r.server, tool, func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		var zero Out
		if err := ctx.Err(); err != nil {
			return nil, zero, err
		}
		if err := validateRegisteredInput(req, input, r.area, level); err != nil {
			return nil, zero, err
		}
		summary, target, digest, err := safety.Summarize(req.Params.Arguments)
		if err != nil {
			return nil, zero, err
		}
		pending, err := r.gate.Require(ctx, req, level, impact+"\nTarget: "+target+"\nArguments: "+summary+"\nArguments SHA-256: "+digest)
		if pending != nil || err != nil {
			return pending, zero, err
		}
		var review string
		if level == safety.Medium {
			review, err = r.reviews.Save(ctx, safety.Review{Tool: name, Area: r.area, Level: level, Target: target, ArgumentsSHA256: digest, Arguments: summary, ExpectedImpact: impact, Checks: []string{"schema and exact argument types", "domain input constraints", "target identifiers", "declared operation impact"}})
			if err != nil {
				return nil, zero, err
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, zero, err
		}
		result, out, err := handler(ctx, req, input)
		if review != "" {
			outcome := "completed"
			if err != nil || result != nil && result.IsError {
				outcome = "failed_or_partial"
			}
			// Never turn an executed mutation into a retryable error if recording
			// its final outcome fails. The durable preflight already exists.
			r.reviews.Complete(review, outcome)
		}
		return result, out, err
	})
	if err != nil {
		return r.fail(err)
	}
	return nil
}

// API is the complete, explicit production capability set. Feature functions
// retain narrow interfaces so tests can select individual areas deliberately.
type API interface {
	ProxyAPI
	CelldAPI
	NginxAPI
	UpstreamsAPI
	DNSAPI
	NotesAPI
	SPAAPI
	ISOsAPI
	VMsAPI
}

func RegisterAll(r *Registry, api API, backupDir string) error {
	RegisterProxies(r, api)
	RegisterNginx(r, api)
	RegisterUpstreams(r, api)
	RegisterCelld(r, api)
	RegisterDNS(r, api)
	RegisterNotes(r, api)
	RegisterSPA(r, api)
	RegisterISOs(r, api)
	RegisterVMs(r, api, backupDir)
	return r.Err()
}

// isVMWrite distinguishes VM recovery-backed inputs from read-only inputs.
func isVMWrite(input any, level safety.Level) bool {
	return level != safety.Low && strings.HasPrefix(fmt.Sprintf("%T", input), "client.VM")
}
