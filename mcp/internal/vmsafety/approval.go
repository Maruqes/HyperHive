// Package vmsafety enforces VM approval and pre-change recovery records.
package vmsafety

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Risk string

const (
	Low      Risk = "low"
	Medium   Risk = "medium"
	High     Risk = "high"
	VeryHigh Risk = "very high"
)

type pendingApproval struct {
	digest  [32]byte
	expires time.Time
}

// Gate belongs to one server registry. Approval is single-use, bounded in time,
// and tied to the complete tool name and canonical arguments, never a tool flag.
type Gate struct {
	mu      sync.Mutex
	pending map[string]pendingApproval
	serial  chan struct{}
}

// Lock serializes backup-and-mutation sequences within this registry. Call only
// after approval; never hold this lock while waiting for a human decision.
func (g *Gate) Lock(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	if g.serial == nil {
		g.serial = make(chan struct{}, 1)
	}
	serial := g.serial
	g.mu.Unlock()
	select {
	case serial <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-serial }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func approvalDigest(req *mcp.CallToolRequest) ([32]byte, error) {
	var args any
	dec := json.NewDecoder(bytes.NewReader(req.Params.Arguments))
	dec.UseNumber()
	if err := dec.Decode(&args); err != nil {
		return [32]byte{}, errors.New("invalid VM arguments")
	}
	canonical, err := json.Marshal(args)
	if err != nil {
		return [32]byte{}, errors.New("invalid VM arguments")
	}
	return sha256.Sum256(append(append([]byte(req.Params.Name), 0), canonical...)), nil
}

// Require returns a nonnil result when the call must wait for the harness's
// option picker. A nil result and nil error authorizes this invocation only.
// Old protocol stdio uses server elicitation; old stateless HTTP fails closed.
func (g *Gate) Require(ctx context.Context, req *mcp.CallToolRequest, risk Risk, summary string) (*mcp.CallToolResult, error) {
	if risk != Low && risk != Medium && risk != High && risk != VeryHigh {
		return nil, errors.New("VM tool has no valid risk classification")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil || req.Params == nil || req.Session == nil {
		return nil, errors.New("VM tool requires an MCP client session")
	}
	if risk == Low {
		return nil, nil
	}
	params := &mcp.ElicitParams{Mode: "form", Message: fmt.Sprintf("VM operation: %s\nRisk: %s\n%s\nApprove this exact invocation? Settings changes require a saved pre-change backup. Approval does not bypass backend restrictions.", req.Params.Name, risk, summary), RequestedSchema: map[string]any{
		"type": "object", "properties": map[string]any{"decision": map[string]any{"type": "string", "title": "VM operation approval", "enum": []string{"cancel", "approve"}}}, "required": []string{"decision"}, "additionalProperties": false,
	}}
	ip := req.Session.InitializeParams()
	if ip != nil && ip.ProtocolVersion >= "2026-07-28" {
		digest, err := approvalDigest(req)
		if err != nil {
			return nil, err
		}
		if req.Params.RequestState != "" || len(req.Params.InputResponses) != 0 {
			g.mu.Lock()
			pending, exists := g.pending[req.Params.RequestState]
			delete(g.pending, req.Params.RequestState)
			g.mu.Unlock()
			if !exists || time.Now().After(pending.expires) || pending.digest != digest {
				return nil, errors.New("VM approval is missing, expired, already used, or belongs to different arguments")
			}
			response, ok := req.Params.InputResponses["vm_approval"].(*mcp.ElicitResult)
			if len(req.Params.InputResponses) != 1 || !ok {
				return nil, approvalResponseError(nil)
			}
			if err := approvalResponseError(response); err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, nil
		}
		var nonce [32]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return nil, errors.New("could not prepare VM approval")
		}
		token := hex.EncodeToString(nonce[:])
		g.mu.Lock()
		if g.pending == nil {
			g.pending = make(map[string]pendingApproval)
		}
		now := time.Now()
		for key, pending := range g.pending {
			if now.After(pending.expires) {
				delete(g.pending, key)
			}
		}
		if len(g.pending) >= 1024 {
			g.mu.Unlock()
			return nil, errors.New("too many pending VM approvals; try again later")
		}
		g.pending[token] = pendingApproval{digest: digest, expires: now.Add(10 * time.Minute)}
		g.mu.Unlock()
		return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{"vm_approval": params}, RequestState: token}, nil
	}
	response, err := req.Session.Elicit(ctx, params)
	if err != nil {
		return nil, errors.New("VM approval picker is unavailable: this MCP connection could not complete form elicitation; use a client that displays MCP approval forms, then retry the exact invocation; no change made")
	}
	if err := approvalResponseError(response); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

// A cancellation/decline is a client protocol response, not evidence that a
// person saw a picker. Never echo arbitrary response content into errors.
func approvalResponseError(response *mcp.ElicitResult) error {
	reason := "MCP client returned no valid approval form response"
	if response != nil {
		switch response.Action {
		case "cancel":
			reason = "MCP client canceled the approval request"
		case "decline":
			reason = "MCP client declined the approval request"
		case "accept":
			decision, _ := response.Content["decision"].(string)
			if decision == "approve" {
				return nil
			}
			if decision == "cancel" {
				reason = "Approval form returned accept, but cancel was selected"
			} else {
				reason = "MCP client returned no explicit approve choice"
			}
		}
	}
	return errors.New(reason + "; no change made. If no picker appeared, the client did not display or complete MCP form elicitation. Retry using a client that displays approval forms; chat permission alone cannot satisfy this gate.")
}
