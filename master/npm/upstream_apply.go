package npm

import (
	"512SvMan/db"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

const upstreamRecoveryTimeout = 45 * time.Second

type UpstreamError struct {
	Status     int
	Code       string
	RecoveryID int64
}

func (e *UpstreamError) Error() string            { return e.Code }
func upstreamError(status int, code string) error { return &UpstreamError{Status: status, Code: code} }

type upstreamSnapshot struct {
	Pools    []Upstream              `json:"pools"`
	Bindings []db.NPMUpstreamBinding `json:"bindings"`
}

var upstreamCoordinator = struct {
	sync.Mutex
	runtime  UpstreamRuntime
	recovery int64
	ready    bool
}{runtime: DockerUpstreamRuntime{Container: "npm"}}

func loadUpstreamSnapshot(ctx context.Context) (upstreamSnapshot, error) {
	state := upstreamSnapshot{Pools: []Upstream{}, Bindings: []db.NPMUpstreamBinding{}}
	rows, err := db.ListNPMUpstreams(ctx)
	if err != nil {
		return state, err
	}
	for _, row := range rows {
		var p Upstream
		if err := json.Unmarshal(row.Definition, &p); err != nil {
			return state, err
		}
		if p.ID != row.ID || p.Name != row.Name || p.Context != row.Context || p.Revision != row.Revision || p.ID < 1 || p.Revision < 1 {
			return state, fmt.Errorf("inconsistent stored upstream identity")
		}
		if err := NormalizeUpstream(&p); err != nil {
			return state, fmt.Errorf("invalid stored upstream definition")
		}
		state.Pools = append(state.Pools, p)
	}
	state.Bindings, err = db.ListNPMUpstreamBindings(ctx)
	return state, err
}

func commitUpstreamSnapshot(ctx context.Context, id int64, state upstreamSnapshot) error {
	return writeUpstreamSnapshot(ctx, id, state, false)
}
func restoreUpstreamSnapshot(ctx context.Context, id int64, state upstreamSnapshot) error {
	return writeUpstreamSnapshot(ctx, id, state, true)
}
func writeUpstreamSnapshot(ctx context.Context, id int64, state upstreamSnapshot, restore bool) error {
	rows := make([]db.NPMUpstreamRow, 0, len(state.Pools))
	for _, p := range state.Pools {
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		rows = append(rows, db.NPMUpstreamRow{ID: p.ID, Name: p.Name, Context: p.Context, Revision: p.Revision, Definition: raw})
	}
	if restore {
		return db.RestoreNPMUpstreamSnapshot(ctx, id, rows, state.Bindings)
	}
	return db.CommitNPMUpstreamSnapshot(ctx, id, rows, state.Bindings)
}

func checkUpstreamReady() error {
	if db.DB == nil {
		return upstreamError(503, "upstream_storage_unavailable")
	}
	if !upstreamCoordinator.ready && upstreamCoordinator.recovery == 0 {
		return upstreamError(503, "upstream_startup_reconciliation_required")
	}
	if upstreamCoordinator.recovery != 0 {
		return &UpstreamError{Status: 503, Code: "upstream_recovery_required", RecoveryID: upstreamCoordinator.recovery}
	}
	return nil
}

func InitializeUpstreams(ctx context.Context) error {
	upstreamCoordinator.Lock()
	defer upstreamCoordinator.Unlock()
	upstreamCoordinator.ready = false
	if err := db.CreateNPMUpstreamTables(ctx); err != nil {
		return err
	}
	pending, err := db.PendingNPMUpstreamOperations(ctx)
	if err != nil {
		return err
	}
	for _, op := range pending {
		if err := recoverUpstreamOperation(ctx, op); err != nil {
			upstreamCoordinator.recovery = op.ID
			return &UpstreamError{Status: 503, Code: "upstream_recovery_required", RecoveryID: op.ID}
		}
	}
	upstreamCoordinator.recovery = 0

	state, err := loadUpstreamSnapshot(ctx)
	if err != nil {
		return err
	}
	if len(state.Pools) == 0 {
		upstreamCoordinator.ready = true
		return nil
	}
	err = applyUpstreamSnapshot(ctx, state, state)
	if err == nil {
		upstreamCoordinator.ready = true
	}
	return err
}

// Read requests must not wait indefinitely behind a configuration apply.
// Keep mutation serialization intact while allowing readers to honor cancellation.
func lockUpstreamRead(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return upstreamError(503, "upstream_read_timeout")
		}
		if upstreamCoordinator.TryLock() {
			return nil
		}
		select {
		case <-ctx.Done():
			return upstreamError(503, "upstream_read_timeout")
		case <-ticker.C:
		}
	}
}

const upstreamReadTimeout = 15 * time.Second

func ListUpstreams(ctx context.Context) ([]Upstream, error) {
	ctx, cancel := context.WithTimeout(ctx, upstreamReadTimeout)
	defer cancel()
	if err := lockUpstreamRead(ctx); err != nil {
		return nil, err
	}
	defer upstreamCoordinator.Unlock()
	if db.DB == nil {
		return nil, upstreamError(503, "upstream_storage_unavailable")
	}
	state, err := loadUpstreamSnapshot(ctx)
	if err != nil {
		return nil, upstreamError(503, "upstream_storage_unavailable")
	}
	for i := range state.Pools {
		p := &state.Pools[i]
		p.References = []UpstreamReference{}
		for _, b := range state.Bindings {
			if b.PoolID == p.ID {
				p.References = append(p.References, UpstreamReference{Consumer: b.Consumer, ID: b.ID, Path: b.Path})
			}
		}
		if !upstreamCoordinator.ready {
			p.ApplyStatus = "reconciliation_required"
		}
		if upstreamCoordinator.recovery != 0 {
			p.ApplyStatus = "recovery_required"
			p.LastError = "upstream_recovery_required"
			p.RecoveryID = upstreamCoordinator.recovery
		}
	}
	return state.Pools, nil
}

func GetUpstream(ctx context.Context, id int64) (Upstream, error) {
	ps, err := ListUpstreams(ctx)
	if err != nil {
		return Upstream{}, err
	}
	for _, p := range ps {
		if p.ID == id {
			return p, nil
		}
	}
	return Upstream{}, upstreamError(404, "upstream_not_found")
}

// expectedRevision is required on replacement/deletion. Creates always start at 1.
func SaveUpstream(ctx context.Context, p Upstream, expectedRevision int64) (Upstream, error) {
	if err := NormalizeUpstream(&p); err != nil {
		return p, &UpstreamError{Status: 400, Code: err.Error()}
	}
	upstreamCoordinator.Lock()
	defer upstreamCoordinator.Unlock()
	if err := checkUpstreamReady(); err != nil {
		return p, err
	}
	before, err := loadUpstreamSnapshot(ctx)
	if err != nil {
		return p, upstreamError(503, "upstream_storage_unavailable")
	}
	after := upstreamSnapshot{Pools: append([]Upstream{}, before.Pools...), Bindings: before.Bindings}
	index := -1
	for i, old := range before.Pools {
		if old.Name == p.Name && old.ID != p.ID {
			return p, upstreamError(409, "upstream_name_conflict")
		}
		if old.ID == p.ID {
			index = i
			if old.Revision != expectedRevision {
				return p, upstreamError(409, "upstream_revision_conflict")
			}
			if old.Context != p.Context {
				for _, b := range before.Bindings {
					if b.PoolID == p.ID {
						return p, upstreamError(409, "upstream_context_referenced")
					}
				}
			}
		}
	}
	if p.ID == 0 {
		if expectedRevision != 0 {
			return p, upstreamError(400, "new_upstream_revision_must_be_zero")
		}
		p.ID, err = db.ReserveNPMUpstreamID(ctx)
		if err != nil {
			return p, upstreamError(503, "upstream_storage_unavailable")
		}
		p.Revision = 1
		after.Pools = append(after.Pools, p)
	} else {
		if index < 0 {
			return p, upstreamError(404, "upstream_not_found")
		}
		p.Revision = expectedRevision + 1
		after.Pools[index] = p
	}
	if err := applyUpstreamSnapshot(ctx, before, after); err != nil {
		return p, err
	}
	for _, saved := range after.Pools {
		if saved.ID == p.ID {
			return saved, nil
		}
	}
	return p, nil
}

func DeleteUpstream(ctx context.Context, id, revision int64) error {
	upstreamCoordinator.Lock()
	defer upstreamCoordinator.Unlock()
	if err := checkUpstreamReady(); err != nil {
		return err
	}
	before, err := loadUpstreamSnapshot(ctx)
	if err != nil {
		return upstreamError(503, "upstream_storage_unavailable")
	}
	after := upstreamSnapshot{Pools: []Upstream{}, Bindings: before.Bindings}
	found := false
	for _, p := range before.Pools {
		if p.ID == id {
			found = true
			if p.Revision != revision {
				return upstreamError(409, "upstream_revision_conflict")
			}
		} else {
			after.Pools = append(after.Pools, p)
		}
	}
	if !found {
		return upstreamError(404, "upstream_not_found")
	}
	for _, b := range before.Bindings {
		if b.PoolID == id {
			return upstreamError(409, "upstream_referenced")
		}
	}
	return applyUpstreamSnapshot(ctx, before, after)
}

func applyUpstreamSnapshot(ctx context.Context, before, after upstreamSnapshot) error {
	if err := verifyUpstreamReferences(ctx, before); err != nil {
		return err
	}
	if err := upstreamCoordinator.runtime.Verify(ctx); err != nil {
		return upstreamError(503, "upstream_adapter_unavailable")
	}
	oldRaw, _ := json.Marshal(before)
	newRaw, _ := json.Marshal(after)
	id, err := db.BeginNPMUpstreamOperation(ctx, oldRaw, newRaw)
	if err != nil {
		return upstreamError(503, "upstream_journal_unavailable")
	}
	for i := range after.Pools {
		after.Pools[i].ApplyStatus = "applied"
		after.Pools[i].Generation = id * 2
		after.Pools[i].LastError = ""
		after.Pools[i].RecoveryID = 0
		after.Pools[i].References = []UpstreamReference{}
	}
	err = upstreamCoordinator.runtime.Apply(ctx, id*2, after.Pools)
	if err == nil {
		err = commitUpstreamSnapshot(ctx, id, after)
		if err != nil && len(after.Pools) > len(before.Pools) {
			// A commit error can be ambiguous. Do not remove a newly committed pool;
			// startup completes the journaled create after checking actual state.
			upstreamCoordinator.recovery = id
			recoveryCtx, cancel := context.WithTimeout(context.Background(), upstreamRecoveryTimeout)
			defer cancel()
			_ = db.SetNPMUpstreamOperationState(recoveryCtx, id, "recovery_required")
			return &UpstreamError{Status: 503, Code: "upstream_create_requires_reconciliation", RecoveryID: id}
		}
	}
	if err == nil {
		return nil
	}
	// The original request may be canceled; compensation gets its own bounded context.
	recoveryCtx, cancel := context.WithTimeout(context.Background(), upstreamRecoveryTimeout)
	defer cancel()
	rollback := upstreamCoordinator.runtime.Apply(recoveryCtx, id*2+1, before.Pools)
	if rollback == nil {
		for i := range before.Pools {
			before.Pools[i].Generation = id*2 + 1
			before.Pools[i].ApplyStatus = "applied"
			before.Pools[i].LastError = "upstream_apply_failed; previous definition restored"
		}
		rollback = restoreUpstreamSnapshot(recoveryCtx, id, before)
	}
	if rollback != nil {
		upstreamCoordinator.recovery = id
		_ = db.SetNPMUpstreamOperationState(recoveryCtx, id, "recovery_required")
		return &UpstreamError{Status: 503, Code: "upstream_recovery_required", RecoveryID: id}
	}
	return upstreamError(503, "upstream_apply_failed")
}

// UpstreamErrorDetails deliberately excludes container output and downstream response bodies.
func UpstreamErrorDetails(err error) (int, map[string]any) {
	var e *UpstreamError
	if errors.As(err, &e) {
		body := map[string]any{"error": e.Code}
		if e.RecoveryID != 0 {
			body["recovery_id"] = e.RecoveryID
		}
		return e.Status, body
	}
	return 503, map[string]any{"error": "upstream_unavailable"}
}

func upstreamBindingConflict(format string, args ...any) error {
	return upstreamError(409, fmt.Sprintf(format, args...))
}
