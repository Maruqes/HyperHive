package npm

import (
	"512SvMan/db"
	"context"
	"encoding/json"
	"fmt"
)

// Recovery never repeats a mutation whose outcome is unknown. It reads actual
// NPM state and compensates only resources carrying the journal's unique marker.
func recoverUpstreamOperation(ctx context.Context, op db.NPMUpstreamOperation) error {
	var before struct {
		Pools     []Upstream       `json:"pools"`
		Snapshot  upstreamSnapshot `json:"snapshot"`
		Consumer  upstreamConsumer `json:"consumer"`
		Operation string           `json:"operation"`
		Create    bool             `json:"create"`
		Remove    bool             `json:"remove"`
		Enabled   bool             `json:"enabled"`
	}
	if err := json.Unmarshal(op.Before, &before); err != nil {
		return err
	}
	if before.Operation == "" {
		var snapshot upstreamSnapshot
		if err := json.Unmarshal(op.Before, &snapshot); err != nil || snapshot.Pools == nil {
			return fmt.Errorf("unrecognized recovery record")
		}
		if err := verifyUpstreamReferences(ctx, snapshot); err != nil {
			return err
		}
		var candidate upstreamSnapshot
		if err := json.Unmarshal(op.After, &candidate); err != nil {
			return err
		}
		if len(candidate.Pools) > len(snapshot.Pools) {
			for i := range candidate.Pools {
				p := &candidate.Pools[i]
				if p.ID < 1 || p.Revision < 1 {
					return fmt.Errorf("invalid journaled pool identity")
				}
				if err := NormalizeUpstream(p); err != nil {
					return err
				}
				p.Generation = op.ID*2 + 1
				p.ApplyStatus = "applied"
				p.LastError = ""
				p.RecoveryID = 0
			}
			if err := upstreamCoordinator.runtime.Apply(ctx, op.ID*2+1, candidate.Pools); err != nil {
				return err
			}
			return commitUpstreamSnapshot(ctx, op.ID, candidate)
		}

		if err := upstreamCoordinator.runtime.Apply(ctx, op.ID*2+1, snapshot.Pools); err != nil {
			return err
		}
		for i := range snapshot.Pools {
			snapshot.Pools[i].Generation = op.ID*2 + 1
			snapshot.Pools[i].ApplyStatus = "applied"
		}
		return restoreUpstreamSnapshot(ctx, op.ID, snapshot)
	}
	credentials, ok := ctx.Value(upstreamNPMContextKey{}).(upstreamNPMCredentials)
	if !ok {
		token, err := ServiceToken(ctx, "npm-data")
		if err != nil {
			return err
		}
		credentials = upstreamNPMCredentials{Base: "http://127.0.0.1:81", Token: token}
	}
	var after struct {
		Consumer upstreamConsumer        `json:"consumer"`
		Bindings []db.NPMUpstreamBinding `json:"bindings"`
	}
	if before.Operation == "enable" {
		current, err := readUpstreamConsumer(credentials.Base, credentials.Token, before.Consumer.Kind, before.Consumer.id())
		if err != nil {
			return err
		}
		var expected upstreamConsumer
		_ = json.Unmarshal(before.Consumer.raw(), &expected)
		if expected.Proxy != nil {
			expected.Proxy.Enabled = before.Enabled
		} else {
			expected.Stream.Enabled = before.Enabled
		}
		if !consumerMatches(before.Consumer.raw(), current) && !consumerMatches(expected.raw(), current) {
			return upstreamError(409, "upstream_consumer_drift")
		}
		oldEnabled := false
		if before.Consumer.Proxy != nil {
			oldEnabled = before.Consumer.Proxy.Enabled
		} else {
			oldEnabled = before.Consumer.Stream.Enabled
		}
		if before.Consumer.Kind == "proxy" {
			if oldEnabled {
				err = rawEnableProxy(credentials.Base, credentials.Token, current.id())
			} else {
				err = rawDisableProxy(credentials.Base, credentials.Token, current.id())
			}
		} else {
			if oldEnabled {
				err = rawEnableStream(credentials.Base, credentials.Token, current.id())
			} else {
				err = rawDisableStream(credentials.Base, credentials.Token, current.id())
			}
		}
		if err != nil {
			return err
		}
		return restoreUpstreamSnapshot(ctx, op.ID, before.Snapshot)
	}
	if err := json.Unmarshal(op.After, &after); err != nil {
		return err
	}
	if before.Create {
		// The marker was persisted before POST. An absent marker leaves the outcome
		// unknown, so retain recovery-required state rather than repeating POST.
		var resources []upstreamConsumer
		if after.Consumer.Kind == "proxy" {
			ps, err := rawGetAllProxys(credentials.Base, credentials.Token)
			if err != nil {
				return err
			}
			for i := range ps {
				resources = append(resources, upstreamConsumer{Kind: "proxy", Proxy: &ps[i]})
			}
		} else {
			ps, err := rawListStreams(credentials.Base, credentials.Token)
			if err != nil {
				return err
			}
			for i := range ps {
				resources = append(resources, upstreamConsumer{Kind: "stream", Stream: &ps[i]})
			}
		}
		var matched *upstreamConsumer
		for i, c := range resources {
			meta := map[string]any{}
			if c.Proxy != nil {
				meta = c.Proxy.Meta
			} else {
				meta = c.Stream.Meta
			}
			if marker, ok := meta["hyperhive_upstream_operation"].(float64); ok && int64(marker) == op.ID {
				if matched != nil {
					return upstreamError(409, "duplicate_consumer_operation_marker")
				}
				if !consumerIntentMatches(after.Consumer, c) {
					return upstreamError(409, "upstream_consumer_drift")
				}
				if online, ok := meta["nginx_online"].(bool); !ok || !online {
					return upstreamError(503, "consumer_activation_requires_reconciliation")
				}
				matched = &resources[i]
			}
		}
		if matched == nil {
			return upstreamError(503, "consumer_create_outcome_unknown")
		}
		if len(after.Bindings) == 0 {
			return fmt.Errorf("missing journaled create bindings")
		}
		state := before.Snapshot
		for _, binding := range after.Bindings {
			binding.ID = matched.id()
			binding.Expected = matched.raw()
			state.Bindings = append(state.Bindings, binding)
		}
		return commitUpstreamSnapshot(ctx, op.ID, state)

	} else if before.Remove {
		_, err := readUpstreamConsumer(credentials.Base, credentials.Token, before.Consumer.Kind, before.Consumer.id())
		if typed, ok := err.(*UpstreamError); ok && typed.Status == 404 {
			state := before.Snapshot
			state.Bindings = nil
			for _, b := range before.Snapshot.Bindings {
				if b.ID == before.Consumer.id() && (b.Consumer == before.Consumer.Kind || before.Consumer.Kind == "proxy" && b.Consumer == "location") {
					continue
				}
				state.Bindings = append(state.Bindings, b)
			}
			// Deletion completed: commit the observed absence and release references.
			return commitUpstreamSnapshot(ctx, op.ID, state)
		}
		if err != nil {
			return err
		}
	} else {
		current, err := readUpstreamConsumer(credentials.Base, credentials.Token, before.Consumer.Kind, before.Consumer.id())
		if err != nil {
			return err
		}
		if !consumerMatches(before.Consumer.raw(), current) && !consumerMatches(after.Consumer.raw(), current) {
			return upstreamError(409, "upstream_consumer_drift")
		}
		if !consumerMatches(before.Consumer.raw(), current) {
			if _, err := writeConsumer(credentials.Base, credentials.Token, before.Consumer, false); err != nil {
				return err
			}
		}
	}
	return restoreUpstreamSnapshot(ctx, op.ID, before.Snapshot)
}

func consumerIntentMatches(desired, current upstreamConsumer) bool {
	var copy upstreamConsumer
	_ = json.Unmarshal(desired.raw(), &copy)
	if copy.Proxy != nil && current.Proxy != nil {
		copy.Proxy.ID = current.Proxy.ID
		copy.Proxy.Enabled = current.Proxy.Enabled
		if len(copy.Proxy.Locations) == 0 && len(current.Proxy.Locations) == 0 {
			copy.Proxy.Locations = current.Proxy.Locations
		}
		for i := range copy.Proxy.Locations {
			for _, actual := range current.Proxy.Locations {
				if copy.Proxy.Locations[i].Path == actual.Path && copy.Proxy.Locations[i].AdvancedConfig == nil && actual.AdvancedConfig != nil && *actual.AdvancedConfig == "" {
					copy.Proxy.Locations[i].AdvancedConfig = actual.AdvancedConfig
				}
			}
		}
	}
	if copy.Stream != nil && current.Stream != nil {
		copy.Stream.ID = current.Stream.ID
		copy.Stream.Enabled = current.Stream.Enabled
	}
	return consumerMatches(copy.raw(), current)
}
