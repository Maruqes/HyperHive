package npm

import (
	"512SvMan/db"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

type upstreamConsumer struct {
	Kind   string  `json:"kind"`
	Proxy  *Proxy  `json:"proxy,omitempty"`
	Stream *Stream `json:"stream,omitempty"`
}

func (c upstreamConsumer) id() int {
	if c.Proxy != nil {
		return c.Proxy.ID
	}
	return c.Stream.ID
}
func (c upstreamConsumer) raw() []byte { b, _ := json.Marshal(c); return b }

func readUpstreamConsumer(base, token, kind string, id int) (upstreamConsumer, error) {
	if kind == "proxy" {
		ps, err := rawGetAllProxys(base, token)
		if err != nil {
			return upstreamConsumer{}, err
		}
		for _, p := range ps {
			if p.ID == id {
				return upstreamConsumer{Kind: kind, Proxy: &p}, nil
			}
		}
	} else {
		ps, err := rawListStreams(base, token)
		if err != nil {
			return upstreamConsumer{}, err
		}
		for _, p := range ps {
			if p.ID == id {
				return upstreamConsumer{Kind: kind, Stream: &p}, nil
			}
		}
	}
	return upstreamConsumer{}, upstreamError(404, "consumer_not_found")
}

func consumerComparable(c upstreamConsumer) upstreamConsumer {
	var result upstreamConsumer
	_ = json.Unmarshal(c.raw(), &result)
	if result.Proxy != nil {
		result.Proxy.UpstreamID = nil
		// New NPM defaults are equivalent to fields absent in older snapshots.
		if result.Proxy.TrustForwardedProto != nil && !*result.Proxy.TrustForwardedProto {
			result.Proxy.TrustForwardedProto = nil
		}
		for i := range result.Proxy.Locations {
			result.Proxy.Locations[i].UpstreamID = nil
			if id := result.Proxy.Locations[i].AccessListID; id != nil && *id == 0 {
				result.Proxy.Locations[i].AccessListID = nil
			}
		}
		delete(result.Proxy.Meta, "nginx_online")
		delete(result.Proxy.Meta, "nginx_err")
	}
	if result.Stream != nil {
		result.Stream.UpstreamID = nil
		result.Stream.Description = ""
		delete(result.Stream.Meta, "nginx_online")
		delete(result.Stream.Meta, "nginx_err")
	}
	return result
}

func consumerMatches(expected []byte, actual upstreamConsumer) bool {
	var old upstreamConsumer
	if json.Unmarshal(expected, &old) != nil {
		return false
	}
	return reflect.DeepEqual(consumerComparable(old), consumerComparable(actual))
}

func selectedPool(raw json.RawMessage, old int64, host string, port int, context string, pools []Upstream) (int64, string, int, error) {
	id := old
	if len(raw) > 0 {
		if string(raw) == "null" {
			id = 0
		} else if err := json.Unmarshal(raw, &id); err != nil || id < 1 {
			return 0, "", 0, upstreamError(400, "upstream_id_must_be_positive_or_null")
		}
	}
	if id == 0 {
		if strings.HasPrefix(host, "hh_upstream_") {
			return 0, "", 0, upstreamError(400, "reserved_upstream_target")
		}
		if old != 0 && (host == "" || port < 1 || port > 65535) {
			return 0, "", 0, upstreamError(400, "detach_requires_direct_target")
		}
		return 0, host, port, nil
	}
	if host != "" || port != 0 {
		return 0, "", 0, upstreamError(400, "upstream_conflicts_with_direct_target")
	}
	for _, p := range pools {
		if p.ID == id {
			if p.Context != context {
				return 0, "", 0, upstreamError(400, "upstream_context_mismatch")
			}
			return id, upstreamInternalName(id), 80, nil
		}
	}
	return 0, "", 0, upstreamError(404, "upstream_not_found")
}

func resolveConsumerTargets(desired upstreamConsumer, state upstreamSnapshot) (upstreamConsumer, []db.NPMUpstreamBinding, error) {
	old := map[string]int64{}
	for _, b := range state.Bindings {
		if b.ID == desired.id() && (b.Consumer == desired.Kind || desired.Kind == "proxy" && b.Consumer == "location") {
			old[b.Consumer+":"+b.Path] = b.PoolID
		}
	}
	bindings := []db.NPMUpstreamBinding{}
	resolve := func(kind, path string, raw json.RawMessage, host *string, port *int, context string) error {
		id, target, p, err := selectedPool(raw, old[kind+":"+path], *host, *port, context, state.Pools)
		if err != nil {
			return err
		}
		*host = target
		*port = p
		if id != 0 {
			bindings = append(bindings, db.NPMUpstreamBinding{Consumer: kind, ID: desired.id(), Path: path, PoolID: id})
		}
		return nil
	}
	if desired.Proxy != nil {
		p := desired.Proxy
		if err := resolve("proxy", "", p.UpstreamID, &p.ForwardHost, &p.ForwardPort, "http"); err != nil {
			return desired, nil, err
		}
		if len(bindings) > 0 && p.ForwardScheme != "http" && p.ForwardScheme != "https" {
			return desired, nil, upstreamError(400, "upstream_requires_http_or_https_scheme")
		}
		paths := map[string]bool{}
		for i := range p.Locations {
			l := &p.Locations[i]
			if l.Path == "" || paths[l.Path] {
				return desired, nil, upstreamError(400, "location_path_empty_or_duplicate")
			}
			paths[l.Path] = true
			if err := resolve("location", l.Path, l.UpstreamID, &l.ForwardHost, &l.ForwardPort, "http"); err != nil {
				return desired, nil, err
			}
			if len(l.UpstreamID) > 0 && string(l.UpstreamID) != "null" && l.ForwardScheme != "http" && l.ForwardScheme != "https" {
				return desired, nil, upstreamError(400, "upstream_requires_http_or_https_scheme")
			}
			l.UpstreamID = nil
		}
		p.UpstreamID = nil
	} else {
		p := desired.Stream
		if err := resolve("stream", "", p.UpstreamID, &p.Forwarding_host, &p.Forwarding_port, "stream"); err != nil {
			return desired, nil, err
		}
		p.UpstreamID = nil
		if len(bindings) > 0 && !p.Tcp_forwarding && !p.Udp_forwarding {
			return desired, nil, upstreamError(400, "stream_requires_tcp_or_udp")
		}
	}
	return desired, bindings, nil
}

func writeConsumer(base, token string, c upstreamConsumer, create bool) (int, error) {
	if c.Proxy != nil {
		if create {
			return rawCreateProxy(base, token, *c.Proxy)
		}
		return c.Proxy.ID, rawEditProxy(base, token, *c.Proxy)
	}
	if create {
		return rawCreateStream(base, token, *c.Stream)
	}
	return c.Stream.ID, rawEditStream(base, token, *c.Stream)
}
func deleteConsumer(base, token, kind string, id int) error {
	if kind == "proxy" {
		return rawDeleteProxy(base, token, id)
	}
	return rawDeleteStream(base, token, id)
}

// mutateUpstreamConsumer serializes downstream writes with pool mutations. The
// journal keeps uncertain HTTP outcomes visible instead of retrying a mutation.
func mutateUpstreamConsumer(base, token string, desired upstreamConsumer, create, remove bool) (int, error) {
	upstreamCoordinator.Lock()
	defer upstreamCoordinator.Unlock()
	ctx := context.Background()
	state := upstreamSnapshot{}
	if db.DB != nil {
		if err := checkUpstreamReady(); err != nil {
			return 0, err
		}
		var err error
		state, err = loadUpstreamSnapshot(ctx)
		if err != nil {
			return 0, upstreamError(503, "upstream_storage_unavailable")
		}
	}
	var current upstreamConsumer
	var err error
	bound := false
	for _, b := range state.Bindings {
		if b.ID == desired.id() && (b.Consumer == desired.Kind || desired.Kind == "proxy" && b.Consumer == "location") {
			if !bound {
				current, err = readUpstreamConsumer(base, token, desired.Kind, desired.id())
				if err != nil {
					return 0, err
				}
				bound = true
			}
			if !consumerMatches(b.Expected, current) {
				return 0, upstreamBindingConflict("upstream_consumer_drift")
			}
		}
	}
	var bindings []db.NPMUpstreamBinding
	if desired.Proxy != nil && !create && !remove {
		if current.Proxy == nil {
			current, err = readUpstreamConsumer(base, token, desired.Kind, desired.id())
			if err != nil {
				return 0, err
			}
		}
		preserveProxyVersionSettings(desired.Proxy, *current.Proxy)
	}
	if bound && !remove {
		// Existing integrations know only some metadata keys. Preserve unrelated
		// NPM/user metadata while allowing explicitly supplied values to change.
		merged := map[string]any{}
		if current.Proxy != nil {
			for key, value := range current.Proxy.Meta {
				merged[key] = value
			}
			for key, value := range desired.Proxy.Meta {
				merged[key] = value
			}
			desired.Proxy.Meta = merged
		}
		if current.Stream != nil {
			for key, value := range current.Stream.Meta {
				merged[key] = value
			}
			for key, value := range desired.Stream.Meta {
				merged[key] = value
			}
			desired.Stream.Meta = merged
		}
	}
	if !remove {
		desired, bindings, err = resolveConsumerTargets(desired, state)
		if err != nil {
			return 0, err
		}
	}
	if !bound && len(bindings) == 0 {
		if remove {
			return desired.id(), deleteConsumer(base, token, desired.Kind, desired.id())
		}
		return writeConsumer(base, token, desired, create)
	}
	if err := checkUpstreamReady(); err != nil {
		return 0, err
	}
	if err := upstreamCoordinator.runtime.Verify(ctx); err != nil {
		return 0, upstreamError(503, "upstream_adapter_unavailable")
	}
	if !create && !bound {
		current, err = readUpstreamConsumer(base, token, desired.Kind, desired.id())
		if err != nil {
			return 0, err
		}
	}
	// Pool definitions remain unchanged, but references are committed with the journal.
	beforeRaw, _ := json.Marshal(map[string]any{"snapshot": state, "consumer": current, "operation": "consumer", "create": create, "remove": remove})
	afterRaw, _ := json.Marshal(map[string]any{"consumer": desired, "bindings": bindings})
	op, err := db.BeginNPMUpstreamOperation(ctx, beforeRaw, afterRaw)
	if err != nil {
		return 0, upstreamError(503, "upstream_journal_unavailable")
	}
	if !remove {
		var meta map[string]any
		if desired.Proxy != nil {
			if desired.Proxy.Meta == nil {
				desired.Proxy.Meta = map[string]any{}
			}
			meta = desired.Proxy.Meta
		} else {
			if desired.Stream.Meta == nil {
				desired.Stream.Meta = map[string]any{}
			}
			meta = desired.Stream.Meta
		}
		meta["hyperhive_upstream_operation"] = op
		candidate, _ := json.Marshal(map[string]any{"consumer": desired, "bindings": bindings})
		if err := db.UpdateNPMUpstreamOperationCandidate(ctx, op, candidate); err != nil {
			_ = db.SetNPMUpstreamOperationState(ctx, op, "rolled_back")
			return 0, upstreamError(503, "upstream_journal_unavailable")
		}
	}
	id := desired.id()
	if remove {
		err = deleteConsumer(base, token, desired.Kind, id)
	} else {
		id, err = writeConsumer(base, token, desired, create)
	}
	if err != nil {
		upstreamCoordinator.recovery = op
		_ = db.SetNPMUpstreamOperationState(ctx, op, "recovery_required")
		return 0, &UpstreamError{Status: 503, Code: "consumer_outcome_requires_reconciliation", RecoveryID: op}
	}
	after := upstreamSnapshot{Pools: state.Pools, Bindings: []db.NPMUpstreamBinding{}}
	for _, b := range state.Bindings {
		if b.ID == id && (b.Consumer == desired.Kind || desired.Kind == "proxy" && b.Consumer == "location") {
			continue
		}
		after.Bindings = append(after.Bindings, b)
	}
	if !remove {
		actual, readErr := readUpstreamConsumer(base, token, desired.Kind, id)
		err = readErr
		if err == nil {
			if !consumerIntentMatches(desired, actual) {
				upstreamCoordinator.recovery = op
				_ = db.SetNPMUpstreamOperationState(ctx, op, "recovery_required")
				return 0, &UpstreamError{Status: 409, Code: "upstream_consumer_drift", RecoveryID: op}
			}
			meta := map[string]any{}
			if actual.Proxy != nil {
				meta = actual.Proxy.Meta
			} else {
				meta = actual.Stream.Meta
			}
			if online, ok := meta["nginx_online"].(bool); ok && !online {
				err = fmt.Errorf("consumer configuration rejected")
			}
			for _, b := range bindings {
				b.ID = id
				b.Expected = actual.raw()
				after.Bindings = append(after.Bindings, b)
			}
		}
	}
	if err == nil {
		err = commitUpstreamSnapshot(ctx, op, after)
	}
	if err == nil {
		return id, nil
	}
	// Compensation is safe only after the successful write returned its resource ID.
	var rollback error
	if create {
		upstreamCoordinator.recovery = op
		_ = db.SetNPMUpstreamOperationState(ctx, op, "recovery_required")
		return 0, &UpstreamError{Status: 503, Code: "consumer_create_requires_reconciliation", RecoveryID: op}
	} else if remove {
		// NPM cannot recreate a deleted resource with its immutable ID. Retain journal.
		rollback = fmt.Errorf("deleted consumer cannot be restored with its original ID")
	} else {
		_, rollback = writeConsumer(base, token, current, false)
	}
	if rollback == nil {
		rollback = restoreUpstreamSnapshot(ctx, op, state)
	}
	if rollback != nil {
		upstreamCoordinator.recovery = op
		_ = db.SetNPMUpstreamOperationState(ctx, op, "recovery_required")
		return 0, &UpstreamError{Status: 503, Code: "upstream_recovery_required", RecoveryID: op}
	}
	return 0, upstreamError(503, "consumer_apply_failed")
}

func CreateProxy(base, token string, p Proxy) (int, error) {
	return mutateUpstreamConsumer(base, token, upstreamConsumer{Kind: "proxy", Proxy: &p}, true, false)
}

func preserveProxyVersionSettings(desired *Proxy, current Proxy) {
	if desired.TrustForwardedProto == nil {
		desired.TrustForwardedProto = current.TrustForwardedProto
	}
	for i := range desired.Locations {
		if desired.Locations[i].AccessListID != nil {
			continue
		}
		for _, location := range current.Locations {
			if desired.Locations[i].Path == location.Path {
				desired.Locations[i].AccessListID = location.AccessListID
				break
			}
		}
	}
}
func EditProxy(base, token string, p Proxy) error {
	_, err := mutateUpstreamConsumer(base, token, upstreamConsumer{Kind: "proxy", Proxy: &p}, false, false)
	return err
}
func DeleteProxy(base, token string, id int) error {
	_, err := mutateUpstreamConsumer(base, token, upstreamConsumer{Kind: "proxy", Proxy: &Proxy{ID: id}}, false, true)
	return err
}
func CreateStream(base, token string, p Stream) (int, error) {
	return mutateUpstreamConsumer(base, token, upstreamConsumer{Kind: "stream", Stream: &p}, true, false)
}
func EditStream(base, token string, p Stream) error {
	_, err := mutateUpstreamConsumer(base, token, upstreamConsumer{Kind: "stream", Stream: &p}, false, false)
	return err
}
func DeleteStream(base, token string, id int) error {
	_, err := mutateUpstreamConsumer(base, token, upstreamConsumer{Kind: "stream", Stream: &Stream{ID: id}}, false, true)
	return err
}

func consumerBindings() ([]db.NPMUpstreamBinding, error) {
	if db.DB == nil {
		return nil, nil
	}
	return db.ListNPMUpstreamBindings(context.Background())
}
func GetAllProxys(base, token string) ([]Proxy, error) {
	upstreamCoordinator.Lock()
	defer upstreamCoordinator.Unlock()
	ps, err := rawGetAllProxys(base, token)
	if err != nil {
		return nil, err
	}
	bindings, err := consumerBindings()
	if err != nil {
		return nil, err
	}
	for i := range ps {
		for _, b := range bindings {
			if b.ID == ps[i].ID && (b.Consumer == "proxy" || b.Consumer == "location") && !consumerMatches(b.Expected, upstreamConsumer{Kind: "proxy", Proxy: &ps[i]}) {
				return nil, upstreamError(409, "upstream_consumer_drift")
			}
		}
	}
	for i := range ps {
		for _, b := range bindings {
			if b.ID != ps[i].ID {
				continue
			}
			raw, _ := json.Marshal(b.PoolID)
			if b.Consumer == "proxy" {
				ps[i].UpstreamID = raw
				ps[i].ForwardHost = ""
				ps[i].ForwardPort = 0
			}
			if b.Consumer == "location" {
				for j := range ps[i].Locations {
					if ps[i].Locations[j].Path == b.Path {
						ps[i].Locations[j].UpstreamID = raw
						ps[i].Locations[j].ForwardHost = ""
						ps[i].Locations[j].ForwardPort = 0
					}
				}
			}
		}
	}
	return ps, nil
}
func ListStreams(base, token string) ([]Stream, error) {
	upstreamCoordinator.Lock()
	defer upstreamCoordinator.Unlock()
	ps, err := rawListStreams(base, token)
	if err != nil {
		return nil, err
	}
	bindings, err := consumerBindings()
	if err != nil {
		return nil, err
	}
	for i := range ps {
		for _, b := range bindings {
			if b.ID == ps[i].ID && b.Consumer == "stream" && !consumerMatches(b.Expected, upstreamConsumer{Kind: "stream", Stream: &ps[i]}) {
				return nil, upstreamError(409, "upstream_consumer_drift")
			}
		}
	}
	for i := range ps {
		for _, b := range bindings {
			if b.ID == ps[i].ID && b.Consumer == "stream" {
				ps[i].UpstreamID, _ = json.Marshal(b.PoolID)
				ps[i].Forwarding_host = ""
				ps[i].Forwarding_port = 0
			}
		}
	}
	return ps, nil
}

func setConsumerEnabled(base, token, kind string, id int, enable bool) error {
	upstreamCoordinator.Lock()
	defer upstreamCoordinator.Unlock()
	ctx := context.Background()
	state := upstreamSnapshot{}
	if db.DB != nil {
		if err := checkUpstreamReady(); err != nil {
			return err
		}
		var err error
		state, err = loadUpstreamSnapshot(ctx)
		if err != nil {
			return err
		}
	}
	bound := false
	for _, b := range state.Bindings {
		if b.ID == id && (b.Consumer == kind || kind == "proxy" && b.Consumer == "location") {
			bound = true
		}
	}
	apply := func(value bool) error {
		if kind == "proxy" {
			if value {
				return rawEnableProxy(base, token, id)
			}
			return rawDisableProxy(base, token, id)
		}
		if value {
			return rawEnableStream(base, token, id)
		}
		return rawDisableStream(base, token, id)
	}
	if !bound {
		return apply(enable)
	}
	actual, err := readUpstreamConsumer(base, token, kind, id)
	if err != nil {
		return err
	}
	for _, b := range state.Bindings {
		if b.ID == id && (b.Consumer == kind || kind == "proxy" && b.Consumer == "location") && !consumerMatches(b.Expected, actual) {
			return upstreamBindingConflict("upstream_consumer_drift")
		}
	}
	originalRaw, _ := json.Marshal(state)
	var original upstreamSnapshot
	_ = json.Unmarshal(originalRaw, &original)
	before, _ := json.Marshal(map[string]any{"snapshot": state, "consumer": actual, "operation": "enable", "enabled": enable})
	op, err := db.BeginNPMUpstreamOperation(ctx, before, before)
	if err != nil {
		return err
	}
	if err = apply(enable); err != nil {
		upstreamCoordinator.recovery = op
		_ = db.SetNPMUpstreamOperationState(ctx, op, "recovery_required")
		return &UpstreamError{Status: 503, Code: "consumer_outcome_requires_reconciliation", RecoveryID: op}
	}
	updated, err := readUpstreamConsumer(base, token, kind, id)
	if err == nil {
		var expected upstreamConsumer
		_ = json.Unmarshal(actual.raw(), &expected)
		if expected.Proxy != nil {
			expected.Proxy.Enabled = enable
		} else {
			expected.Stream.Enabled = enable
		}
		if !consumerMatches(expected.raw(), updated) {
			upstreamCoordinator.recovery = op
			_ = db.SetNPMUpstreamOperationState(ctx, op, "recovery_required")
			return &UpstreamError{Status: 409, Code: "upstream_consumer_drift", RecoveryID: op}
		}
		for i := range state.Bindings {
			b := &state.Bindings[i]
			if b.ID == id && (b.Consumer == kind || kind == "proxy" && b.Consumer == "location") {
				b.Expected = updated.raw()
			}
		}
		err = commitUpstreamSnapshot(ctx, op, state)
	}
	if err == nil {
		return nil
	}
	oldEnabled := false
	if actual.Proxy != nil {
		oldEnabled = actual.Proxy.Enabled
	} else {
		oldEnabled = actual.Stream.Enabled
	}
	rollback := apply(oldEnabled)
	if rollback == nil {
		rollback = restoreUpstreamSnapshot(ctx, op, original)
	}
	if rollback != nil {
		upstreamCoordinator.recovery = op
		return &UpstreamError{Status: 503, Code: "upstream_recovery_required", RecoveryID: op}
	}
	return upstreamError(503, "consumer_apply_failed")
}

func EnableProxy(base, token string, id int) error {
	return setConsumerEnabled(base, token, "proxy", id, true)
}
func DisableProxy(base, token string, id int) error {
	return setConsumerEnabled(base, token, "proxy", id, false)
}
func EnableStream(base, token string, id int) error {
	return setConsumerEnabled(base, token, "stream", id, true)
}
func DisableStream(base, token string, id int) error {
	return setConsumerEnabled(base, token, "stream", id, false)
}
