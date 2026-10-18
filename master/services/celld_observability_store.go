package services

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	limits "github.com/Maruqes/512SvMan/api/celldobserve"
	pb "github.com/Maruqes/512SvMan/api/proto/celld"
	_ "github.com/mattn/go-sqlite3"
	"google.golang.org/protobuf/proto"
)

var observationFleetID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
var observationTraceID = regexp.MustCompile(`^[a-f0-9]{32}$`)

const observationRetention = 7 * 24 * time.Hour

type observationStore struct {
	onAppend                                func(string, []StoredObservation)
	rejected                                uint64
	mu                                      sync.Mutex
	root                                    string
	fleetBudget, globalBudget, segmentBytes int64
	reserve                                 uint64
	now                                     func() time.Time
	space                                   func(string) (uint64, uint64, error)
	dropped, pruned                         uint64
	lastError                               string
}
type observationSegment struct {
	path, fleet string
	created     int64
	size        int64
}
type ObservationStorage struct {
	RejectedBatches uint64 `json:"rejected_batches"`
	Bytes           int64  `json:"bytes"`
	GlobalBytes     int64  `json:"global_bytes"`
	FleetBudget     int64  `json:"fleet_budget_bytes"`
	GlobalBudget    int64  `json:"global_budget_bytes"`
	RetentionDays   int    `json:"retention_days"`
	Dropped         uint64 `json:"dropped"`
	Pruned          uint64 `json:"pruned_segments"`
	Error           string `json:"error,omitempty"`
}
type ObservationQuery struct {
	ReceivedAfter                                                 int64
	From, To                                                      int64
	Cursor, Node, Workload, Source, Severity, Text, TraceID, Kind string
	Limit                                                         int
	Download                                                      bool
}
type StoredObservation struct {
	ID         string                  `json:"id"`
	Node       string                  `json:"node"`
	Generation string                  `json:"generation"`
	ReceivedMS int64                   `json:"received_ms"`
	Record     *pb.ObservationRecord   `json:"record,omitempty"`
	Snapshot   *pb.ObservationSnapshot `json:"snapshot,omitempty"`
}
type ObservationPage struct {
	Records    []StoredObservation `json:"records"`
	NextCursor string              `json:"next_cursor,omitempty"`
	From       int64               `json:"from"`
	To         int64               `json:"to"`
	Truncated  bool                `json:"truncated"`
	Incomplete bool                `json:"incomplete"`
	Error      string              `json:"error,omitempty"`
}

func observationSpace(path string) (uint64, uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return st.Bavail * uint64(st.Bsize), st.Blocks * uint64(st.Bsize), nil
}
func newObservationStore(root string) (*observationStore, error) {
	s := &observationStore{root: root, fleetBudget: 1 << 30, globalBudget: 10 << 30, segmentBytes: 8 << 20, reserve: 2 << 30, now: time.Now, space: observationSpace}
	for key, target := range map[string]*int64{"CELLD_OBSERVABILITY_FLEET_BYTES": &s.fleetBudget, "CELLD_OBSERVABILITY_TOTAL_BYTES": &s.globalBudget} {
		if v := os.Getenv(key); v != "" {
			n, e := strconv.ParseInt(v, 10, 64)
			if e != nil || n < 1<<20 {
				return nil, fmt.Errorf("invalid %s (minimum 1 MiB)", key)
			}
			*target = n
		}
	}
	s.segmentBytes = min(s.segmentBytes, s.fleetBudget/8, s.globalBudget/8)
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(root, 0700); err != nil {
		return nil, err
	}
	s.mu.Lock()
	err := s.cleanupLocked("", 0)
	s.mu.Unlock()
	if err != nil {
		s.lastError = "storage cleanup unavailable"
	}
	return s, nil
}
func physicalSize(info os.FileInfo) int64 {
	n := info.Size()
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		n = observationMax(n, st.Blocks*512)
	}
	return n
}
func (s *observationStore) segmentsLocked() ([]observationSegment, map[string]int64, int64, error) {
	var segs []observationSegment
	usage := map[string]int64{}
	var total int64
	err := filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unexpected symlink in observability store")
		}
		rel, e := filepath.Rel(s.root, path)
		if e != nil {
			return e
		}
		parts := strings.Split(rel, string(filepath.Separator))
		fleet := ""
		if len(parts) == 2 {
			fleet = parts[0]
		}
		n := physicalSize(info)
		usage[fleet] += n
		total += n
		if len(parts) == 2 && observationFleetID.MatchString(fleet) && strings.HasSuffix(parts[1], ".sqlite") {
			stamp, e := strconv.ParseInt(strings.TrimSuffix(parts[1], ".sqlite"), 10, 64)
			if e == nil {
				segs = append(segs, observationSegment{path, fleet, stamp, n})
			}
		}
		return nil
	})
	sort.Slice(segs, func(i, j int) bool { return segs[i].created < segs[j].created })
	return segs, usage, total, err
}
func (s *observationStore) removeLocked(seg observationSegment) error {
	// No persistent readers/writers; DELETE journals never remain open here.
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		if err := os.Remove(seg.path + suffix); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	s.pruned++
	_ = os.Remove(filepath.Dir(seg.path))
	return nil
}
func (s *observationStore) cleanupLocked(fleet string, headroom int64) error {
	segs, usage, total, err := s.segmentsLocked()
	if err != nil {
		return err
	}
	// Late observations expire by their observation time, not their arrival.
	// The timestamp index makes MIN a single indexed lookup per closed segment.
	oldest := map[string]int64{}
	deadline, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, seg := range segs {
		db, e := openObservationDB(seg.path, s.segmentBytes, true)
		if e != nil {
			return fmt.Errorf("unreadable retention segment")
		}
		var stamp sql.NullInt64
		e = db.QueryRowContext(deadline, "SELECT min(time_ms) FROM observations").Scan(&stamp)
		db.Close()
		if e != nil {
			return fmt.Errorf("retention timestamp unavailable")
		}
		oldest[seg.path] = seg.created
		if stamp.Valid && stamp.Int64 > 0 && stamp.Int64 < seg.created/1e6 {
			oldest[seg.path] = stamp.Int64 * 1e6
		}
	}
	sortOldest := func(parts []observationSegment) {
		for i := range parts {
			if stamp, ok := oldest[parts[i].path]; ok {
				parts[i].created = stamp
			}
		}
		sort.Slice(parts, func(i, j int) bool { return parts[i].created < parts[j].created })
	}
	sortOldest(segs)

	cutoff := s.now().Add(-observationRetention).UnixNano()
	for i, seg := range segs {
		if seg.created <= cutoff {
			if err := s.removeLocked(seg); err != nil {
				return err
			}
			total -= seg.size
			usage[seg.fleet] -= seg.size
			segs[i].path = ""
		}
	}
	// Reclaim a noisy fleet's data before touching any other fleet.
	for _, seg := range segs {
		if seg.path == "" {
			continue
		}
		extra := int64(0)
		if seg.fleet == fleet {
			extra = headroom
		}
		if usage[seg.fleet]+extra > s.fleetBudget {
			if err := s.removeLocked(seg); err != nil {
				return err
			}
			usage[seg.fleet] -= seg.size
			total -= seg.size
		}
	}
	// Re-scan to include journals and actual block allocation after reclamation.
	segs, usage, total, err = s.segmentsLocked()
	if err != nil {
		return err
	}
	sortOldest(segs)
	free, capacity, err := s.space(s.root)
	if err != nil {
		return err
	}
	reserve := observationMax(s.reserve, capacity/10)
	for _, seg := range segs {
		if total+headroom <= s.globalBudget && free >= reserve+uint64(headroom) {
			break
		}
		if err := s.removeLocked(seg); err != nil {
			return err
		}
		free, capacity, err = s.space(s.root)
		if err != nil {
			return err
		}
		reserve = observationMax(s.reserve, capacity/10)
		total -= seg.size
		usage[seg.fleet] -= seg.size
	}
	_, usage, total, err = s.segmentsLocked()
	if err != nil {
		return err
	}
	if total+headroom > s.globalBudget || (fleet != "" && usage[fleet]+headroom > s.fleetBudget) || free < reserve+uint64(headroom) {
		return fmt.Errorf("observability quota or free-space reserve reached")
	}
	return nil
}
func openObservationDB(path string, maxBytes int64, readonly bool) (*sql.DB, error) {
	mode := "rwc"
	if readonly {
		mode = "ro"
	}
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(path)+"?mode="+mode+"&_busy_timeout=200&_journal_mode=DELETE&_synchronous=FULL&_cache_size=-256&_temp_store=MEMORY")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if !readonly {
		_, err = db.Exec(fmt.Sprintf("PRAGMA max_page_count=%d", maxBytes/4096))
		if err == nil {
			_, err = db.Exec(`CREATE TABLE IF NOT EXISTS observations (id TEXT PRIMARY KEY, time_ms INTEGER NOT NULL, received_ms INTEGER NOT NULL, node TEXT NOT NULL, generation TEXT NOT NULL, sequence INTEGER NOT NULL, kind TEXT NOT NULL, source TEXT NOT NULL, workload TEXT NOT NULL, severity TEXT NOT NULL, trace_id TEXT NOT NULL, body TEXT NOT NULL, payload BLOB NOT NULL); CREATE INDEX IF NOT EXISTS observation_time ON observations(time_ms,id); CREATE INDEX IF NOT EXISTS observation_trace ON observations(trace_id,time_ms); CREATE INDEX IF NOT EXISTS observation_source ON observations(source,time_ms); CREATE INDEX IF NOT EXISTS observation_received ON observations(received_ms,id);`)
		}
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func (s *observationStore) append(fleet, node string, batch *pb.ObservationBatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !observationFleetID.MatchString(fleet) || batch == nil || len(batch.Generation) != 32 || len(node) > 128 || len(batch.Records) > limits.MaxRecords || proto.Size(batch) > limits.MaxBatchBytes {
		return fmt.Errorf("invalid observation batch")
	}
	if len(batch.Records) == 0 && (batch.Snapshot == nil || batch.Snapshot.CollectedMs == 0 || batch.Snapshot.State != "fresh" && batch.Snapshot.State != "partial") {
		return nil
	}
	// Reserve a full database plus its maximum rollback journal and one bounded
	// transport batch INSIDE both quotas. Databases are closed after each write.
	headroom := 2*s.segmentBytes + limits.MaxBatchBytes
	if err := s.cleanupLocked(fleet, headroom); err != nil {
		s.rejected++
		s.lastError = err.Error()
		return err
	}
	dir := filepath.Join(s.root, fleet)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	segs, _, _, err := s.segmentsLocked()
	if err != nil {
		return err
	}
	now := s.now()
	path := filepath.Join(dir, strconv.FormatInt(now.UnixNano(), 10)+".sqlite")
	for i := len(segs) - 1; i >= 0; i-- {
		seg := segs[i]
		if seg.fleet == fleet && now.UnixNano()-seg.created < int64(time.Hour) && seg.size+int64(proto.Size(batch))*4+(64<<10) < s.segmentBytes {
			path = seg.path
			break
		}
	}
	db, err := openObservationDB(path, s.segmentBytes, false)
	if err != nil {
		s.lastError = "cannot open observation segment"
		return err
	}
	defer db.Close()
	_ = os.Chmod(path, 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var committed []StoredObservation
	put := func(id string, stamp int64, sequence uint64, kind, source, workload, severity, trace, body string, payload []byte) error {
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO observations VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, stamp, now.UnixMilli(), node, batch.Generation, sequence, kind, source, workload, severity, trace, body, payload)
		if err == nil && kind != "metric" {
			if count, _ := res.RowsAffected(); count > 0 {
				var r pb.ObservationRecord
				if json.Unmarshal(payload, &r) == nil {
					committed = append(committed, StoredObservation{ID: id, Node: node, Generation: batch.Generation, ReceivedMS: now.UnixMilli(), Record: &r})
				}
			}
		}
		return err
	}
	// Retry deduplication spans segment rotation and survives master restart.
	known := map[string]bool{}
	ids := []any{}
	placeholders := []string{}
	for _, r := range batch.Records {
		if r == nil {
			return fmt.Errorf("invalid observation record")
		}
		ids = append(ids, node+"/"+batch.Generation+"/"+fmt.Sprintf("%020d", r.Sequence))
		placeholders = append(placeholders, "?")
	}
	if len(ids) > 0 {
		for _, seg := range segs {
			if seg.fleet != fleet || seg.path == path {
				continue
			}
			other, e := openObservationDB(seg.path, s.segmentBytes, true)
			if e != nil {
				s.lastError = "unreadable history segment"
				return e
			}
			rows, e := other.QueryContext(ctx, "SELECT id FROM observations WHERE id IN ("+strings.Join(placeholders, ",")+")", ids...)
			if e == nil {
				for rows.Next() {
					var id string
					if rows.Scan(&id) == nil {
						known[id] = true
					}
				}
				e = rows.Err()
				rows.Close()
			}
			other.Close()
			if e != nil {
				s.lastError = "history deduplication unavailable"
				return e
			}
		}
	}

	for _, r := range batch.Records {
		if r == nil || r.Sequence == 0 || r.Sequence > 1<<63-1 || proto.Size(r) > limits.MaxRecordBytes+2048 {
			return fmt.Errorf("invalid observation record")
		}
		if r.Kind != "log" && r.Kind != "span" && r.Kind != "event" {
			return fmt.Errorf("invalid observation kind")
		}
		if r.TimeMs < now.Add(-observationRetention).UnixMilli() {
			s.dropped++
			continue
		}
		if r.TimeMs > now.Add(time.Minute).UnixMilli() {
			r = proto.Clone(r).(*pb.ObservationRecord)
			r.TimeMs = now.UnixMilli()
		}
		id := node + "/" + batch.Generation + "/" + fmt.Sprintf("%020d", r.Sequence)
		if known[id] {
			continue
		}
		data, _ := json.Marshal(r)
		if err = put(id, r.TimeMs, r.Sequence, r.Kind, r.Source, r.Workload, r.Severity, r.TraceId, r.Body, data); err != nil {
			s.lastError = "observation transaction failed"
			return err
		}
	}
	if sn := batch.Snapshot; sn != nil && sn.CollectedMs >= now.Add(-observationRetention).UnixMilli() && sn.CollectedMs <= now.Add(time.Minute).UnixMilli() && (sn.State == "fresh" || sn.State == "partial") {
		sn = proto.Clone(sn).(*pb.ObservationSnapshot)
		sn.FleetId = fleet
		sn.NodeId = node
		data, _ := json.Marshal(sn)
		id := node + "/" + batch.Generation + "/snapshot/" + strconv.FormatInt(sn.CollectedMs, 10)
		// A repeated snapshot must stay unique even across rotated segments.
		duplicate := false
		for _, seg := range segs {
			if seg.fleet == fleet && seg.path != path {
				other, e := openObservationDB(seg.path, s.segmentBytes, true)
				if e == nil {
					var n int
					e = other.QueryRowContext(ctx, "SELECT count(*) FROM observations WHERE id=?", id).Scan(&n)
					other.Close()
					if e != nil {
						return e
					}
					duplicate = duplicate || n > 0
				}
			}
		}
		if !duplicate {
			err = put(id, sn.CollectedMs, 0, "metric", "runtime", "", "", "", "", data)
			if err != nil {
				return err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		s.lastError = "observation commit failed"
		return err
	}
	s.lastError = ""
	if s.onAppend != nil && len(committed) > 0 {
		s.onAppend(fleet, committed)
	}
	return nil
}
func (s *observationStore) storage(fleet string) ObservationStorage {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, usage, total, err := s.segmentsLocked()
	if err != nil {
		s.lastError = "cannot measure observation storage"
	}
	return ObservationStorage{s.rejected, usage[fleet], total, s.fleetBudget, s.globalBudget, 7, s.dropped, s.pruned, s.lastError}
}
func (s *observationStore) hasFleet(fleet string) bool {
	if !observationFleetID.MatchString(fleet) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	segs, _, _, _ := s.segmentsLocked()
	for _, seg := range segs {
		if seg.fleet == fleet {
			return true
		}
	}
	return false
}
func normalizeObservationQuery(q ObservationQuery, now time.Time) (ObservationQuery, error) {
	if q.To == 0 && q.Cursor != "" {
		var cursor observationCursor
		raw, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || json.Unmarshal(raw, &cursor) != nil {
			return q, ErrCelldInvalid
		}
		q.To = cursor.To
	}
	if q.To == 0 {
		q.To = now.UnixMilli()
	}
	if q.From == 0 {
		q.From = q.To - int64(time.Hour/time.Millisecond)
	}
	if q.From < now.Add(-observationRetention).UnixMilli() {
		q.From = now.Add(-observationRetention).UnixMilli()
	}
	if q.From > q.To || q.To-q.From > int64(observationRetention/time.Millisecond) || q.To > now.Add(time.Minute).UnixMilli() {
		return q, ErrCelldInvalid
	}
	cap := 500
	if q.Download {
		cap = 2000
	}
	if q.Limit == 0 {
		q.Limit = 200
	}
	if q.Limit < 1 || q.Limit > cap || len(q.Text) > 256 || len(q.Node) > 128 || len(q.Workload) > 256 || len(q.Source) > 32 || len(q.Severity) > 16 || len(q.Cursor) > 1024 || q.TraceID != "" && !observationTraceID.MatchString(q.TraceID) {
		return q, ErrCelldInvalid
	}
	return q, nil
}

type observationCursor struct {
	Fleet string `json:"f"`
	Time  int64  `json:"t"`
	ID    string `json:"i"`
	To    int64  `json:"to"`
}

func (s *observationStore) query(parent context.Context, fleet string, q ObservationQuery) (ObservationPage, error) {
	if !s.mu.TryLock() {
		return ObservationPage{Records: []StoredObservation{}, Incomplete: true, Error: "observability store is busy"}, nil
	}
	defer s.mu.Unlock()
	result := ObservationPage{Records: []StoredObservation{}, Incomplete: true}
	if !observationFleetID.MatchString(fleet) {
		return result, ErrCelldInvalid
	}
	var err error
	q, err = normalizeObservationQuery(q, s.now())
	if err != nil {
		return result, err
	}
	cursor := observationCursor{Fleet: fleet, Time: q.From - 1, To: q.To}
	if q.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(q.Cursor)
		if e != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Fleet != fleet || cursor.To != q.To {
			return result, ErrCelldInvalid
		}
	}
	result.From, result.To = q.From, q.To
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	segs, _, _, err := s.segmentsLocked()
	if err != nil {
		return result, err
	}
	bytes := 0
	for _, seg := range segs {
		if seg.fleet != fleet {
			continue
		}
		db, e := openObservationDB(seg.path, s.segmentBytes, true)
		if e != nil {
			result.Error = "unreadable history segment"
			continue
		}
		where := `time_ms>=? AND time_ms<=? AND (time_ms>? OR (time_ms=? AND id>?))`
		args := []any{q.From, q.To, cursor.Time, cursor.Time, cursor.ID}
		for _, filter := range []struct{ col, value string }{{"kind", q.Kind}, {"node", q.Node}, {"workload", q.Workload}, {"source", q.Source}, {"severity", q.Severity}, {"trace_id", q.TraceID}} {
			// Filter both process streams before pagination and export limits.
			if filter.col == "source" && filter.value == "process" {
				where += " AND source IN ('stdout','stderr')"
				continue
			}
			if filter.value != "" {
				where += " AND " + filter.col + "=?"
				args = append(args, filter.value)
			}
		}
		if q.ReceivedAfter > 0 {
			where += " AND received_ms>=?"
			args = append(args, q.ReceivedAfter)
		}
		if q.Text != "" {
			where += " AND instr(body,?)>0"
			args = append(args, q.Text)
		}
		args = append(args, q.Limit+1)
		rows, e := db.QueryContext(ctx, "SELECT id,node,generation,received_ms,kind,payload FROM observations WHERE "+where+" ORDER BY time_ms,id LIMIT ?", args...)
		if e != nil {
			db.Close()
			result.Error = "history query unavailable"
			continue
		}
		for rows.Next() {
			var row StoredObservation
			var kind string
			var payload []byte
			if e = rows.Scan(&row.ID, &row.Node, &row.Generation, &row.ReceivedMS, &kind, &payload); e != nil {
				break
			}
			if kind == "metric" {
				row.Snapshot = &pb.ObservationSnapshot{}
				e = json.Unmarshal(payload, row.Snapshot)
			} else {
				row.Record = &pb.ObservationRecord{}
				e = json.Unmarshal(payload, row.Record)
			}
			if e != nil {
				result.Error = "invalid history record"
				continue
			}
			result.Records = append(result.Records, row)
		}
		if rows.Err() != nil {
			result.Error = "history query interrupted"
		}
		rows.Close()
		db.Close()
		// Keep only the earliest page across segments; late timestamps must not
		// make segment order into query order or grow result memory without bound.
		sort.Slice(result.Records, func(i, j int) bool {
			a, b := result.Records[i], result.Records[j]
			ta, tb := int64(0), int64(0)
			if a.Record != nil {
				ta = a.Record.TimeMs
			} else {
				ta = a.Snapshot.CollectedMs
			}
			if b.Record != nil {
				tb = b.Record.TimeMs
			} else {
				tb = b.Snapshot.CollectedMs
			}
			if ta == tb {
				return a.ID < b.ID
			}
			return ta < tb
		})
		if len(result.Records) > q.Limit+1 {
			result.Records = result.Records[:q.Limit+1]
		}
		if ctx.Err() != nil {
			result.Error = "history query deadline reached"
			break
		}
	}
	stamp := func(r StoredObservation) int64 {
		if r.Record != nil {
			return r.Record.TimeMs
		}
		return r.Snapshot.CollectedMs
	}
	sort.Slice(result.Records, func(i, j int) bool {
		a, b := stamp(result.Records[i]), stamp(result.Records[j])
		if a == b {
			return result.Records[i].ID < result.Records[j].ID
		}
		return a < b
	})
	keep := 0
	for keep < len(result.Records) && keep < q.Limit {
		data, _ := json.Marshal(result.Records[keep])
		if bytes+len(data)+1 > (1<<20)-(8<<10) {
			break
		}
		bytes += len(data) + 1
		keep++
	}
	if keep < len(result.Records) {
		result.Truncated = true
		result.Records = result.Records[:keep]
	}
	if result.Truncated && keep > 0 {
		last := result.Records[keep-1]
		data, _ := json.Marshal(observationCursor{fleet, stamp(last), last.ID, q.To})
		result.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return result, nil
}

func (s *observationStore) latest(parent context.Context, fleet string) ([]*pb.ObservationSnapshot, error) {
	if !s.mu.TryLock() {
		return nil, fmt.Errorf("observability store is busy")
	}
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	segs, _, _, err := s.segmentsLocked()
	if err != nil {
		return nil, err
	}
	nodes := map[string]*pb.ObservationSnapshot{}
	for i := len(segs) - 1; i >= 0; i-- {
		seg := segs[i]
		if seg.fleet != fleet {
			continue
		}
		db, e := openObservationDB(seg.path, s.segmentBytes, true)
		if e != nil {
			continue
		}
		rows, e := db.QueryContext(ctx, `SELECT node,payload,max(time_ms) FROM observations WHERE kind='metric' AND time_ms>=? GROUP BY node LIMIT 128`, s.now().Add(-observationRetention).UnixMilli())
		if e == nil {
			for rows.Next() {
				var node string
				var raw []byte
				var stamp int64
				if rows.Scan(&node, &raw, &stamp) == nil {
					var sn pb.ObservationSnapshot
					if json.Unmarshal(raw, &sn) == nil && (nodes[node] == nil || nodes[node].CollectedMs < stamp) {
						sn.State = "stale"
						nodes[node] = &sn
					}
				}
			}
			rows.Close()
		}
		db.Close()
		if ctx.Err() != nil {
			break
		}
		if len(nodes) >= 128 {
			break
		}
	}
	result := []*pb.ObservationSnapshot{}
	for _, sn := range nodes {
		result = append(result, sn)
	}
	return result, ctx.Err()
}

type ObservedRequests struct {
	Count          uint64  `json:"count"`
	Errors         uint64  `json:"errors"`
	MeanDurationUS float64 `json:"mean_duration_us"`
	MaxDurationUS  uint64  `json:"max_duration_us"`
	RatePerSecond  float64 `json:"rate_per_second"`
	Observed       bool    `json:"observed"`
	Incomplete     bool    `json:"incomplete"`
	Error          string  `json:"error,omitempty"`
}

func (s *observationStore) requestSummary(parent context.Context, fleet string, q ObservationQuery) ObservedRequests {
	if !s.mu.TryLock() {
		return ObservedRequests{Observed: true, Incomplete: true, Error: "observability store is busy"}
	}
	defer s.mu.Unlock()
	result := ObservedRequests{Observed: true, Incomplete: true}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	segs, _, _, err := s.segmentsLocked()
	if err != nil {
		result.Error = "summary unavailable"
		return result
	}
	var sum float64
	for _, seg := range segs {
		if seg.fleet != fleet {
			continue
		}
		db, e := openObservationDB(seg.path, s.segmentBytes, true)
		if e != nil {
			result.Error = "unreadable summary segment"
			continue
		}
		where := `kind='span' AND time_ms>=? AND time_ms<=? AND json_extract(payload,'$.attributes.span_kind')='SPAN_KIND_SERVER'`
		args := []any{q.From, q.To}
		if q.Node != "" {
			where += " AND node=?"
			args = append(args, q.Node)
		}
		if q.Workload != "" {
			where += " AND workload=?"
			args = append(args, q.Workload)
		}
		var count, fail, maxDuration uint64
		var duration float64
		e = db.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(json_extract(payload,'$.failed')=1),0),coalesce(sum(json_extract(payload,'$.duration_us')),0),coalesce(max(json_extract(payload,'$.duration_us')),0) FROM observations WHERE `+where, args...).Scan(&count, &fail, &duration, &maxDuration)
		db.Close()
		if e != nil {
			result.Error = "summary query interrupted"
			continue
		}
		result.Count += count
		result.Errors += fail
		sum += duration
		result.MaxDurationUS = observationMax(result.MaxDurationUS, maxDuration)
	}
	if result.Count > 0 {
		result.MeanDurationUS = sum / float64(result.Count)
	}
	if q.To > q.From {
		result.RatePerSecond = float64(result.Count) * 1000 / float64(q.To-q.From)
	}
	return result
}

func observationMax[T cmp.Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}
