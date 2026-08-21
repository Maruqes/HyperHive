package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"512SvMan/dnsmasq"
	"512SvMan/npm"

	"github.com/vishvananda/netlink"
)

func TestLiveTCPStateMapping(t *testing.T) {
	tests := []struct {
		value uint8
		state string
		group string
	}{
		{netlink.TCP_ESTABLISHED, "established", "active"},
		{netlink.TCP_SYN_SENT, "syn_sent", "handshake"},
		{netlink.TCP_SYN_RECV, "syn_received", "handshake"},
		{netlink.TCP_NEW_SYN_REC, "new_syn_received", "handshake"},
		{netlink.TCP_LISTEN, "listen", "listening"},
		{netlink.TCP_FIN_WAIT1, "fin_wait_1", "closing"},
		{netlink.TCP_FIN_WAIT2, "fin_wait_2", "closing"},
		{netlink.TCP_TIME_WAIT, "time_wait", "closing"},
		{netlink.TCP_CLOSE, "close", "closing"},
		{netlink.TCP_CLOSE_WAIT, "close_wait", "closing"},
		{netlink.TCP_LAST_ACK, "last_ack", "closing"},
		{netlink.TCP_CLOSING, "closing", "closing"},
		{255, "unknown", "other"},
	}
	for _, test := range tests {
		state, group := liveTCPState(test.value)
		if state != test.state || group != test.group {
			t.Errorf("liveTCPState(%d) = (%q, %q), want (%q, %q)", test.value, state, group, test.state, test.group)
		}
	}
}

func TestNormalizeLiveConnectionAddressesAndMetrics(t *testing.T) {
	metrics := &netlink.TCPInfo{
		Rtt: 12500, Rttvar: 2250, Rto: 250000, Min_rtt: 1000,
		Bytes_sent: 10, Bytes_acked: 9, Bytes_received: 8, Bytes_retrans: 7,
		Segs_in: 6, Segs_out: 5, Retransmits: 4, Total_retrans: 3, Unacked: 2, Lost: 1,
		Snd_cwnd: 20, Rcv_space: 21, Snd_wnd: 22, Delivery_rate: 23, Pacing_rate: 24,
		Last_data_sent: 25, Last_data_recv: 26,
	}
	response := testLiveSocket(syscall.AF_INET, net.IPv4zero, 8080, net.IPv4(192, 0, 2, 8), 443, netlink.TCP_ESTABLISHED, 7)
	response.TCPInfo = metrics
	response.InetDiagMsg.ID.Interface = 3
	response.InetDiagMsg.RQueue = 30
	response.InetDiagMsg.WQueue = 40
	connection, ok := normalizeLiveConnection(response, syscall.AF_INET, map[string][]string{}, newLiveStreamIndex(nil), nil, true, func(index int) (string, error) {
		if index != 3 {
			t.Fatalf("interface index = %d, want 3", index)
		}
		return "eth-test", nil
	})
	if !ok {
		t.Fatal("IPv4 connection was rejected")
	}
	if connection.Family != "tcp4" || connection.Local.RawAddress != "0.0.0.0:8080" || connection.Remote.RawAddress != "192.0.2.8:443" {
		t.Fatalf("unexpected IPv4 endpoints: %#v -> %#v", connection.Local, connection.Remote)
	}
	if connection.InterfaceName != "eth-test" || connection.ID == "" {
		t.Fatalf("interface/id not normalized: %#v", connection)
	}
	if connection.TCPInfo == nil || connection.TCPInfo.RTTMilliseconds != 12.5 || connection.TCPInfo.RTTVarianceMilliseconds != 2.25 || connection.TCPInfo.RTOMilliseconds != 250 || connection.TCPInfo.MinRTTMilliseconds != 1 {
		t.Fatalf("metric unit conversion failed: %#v", connection.TCPInfo)
	}
	if connection.TCPInfo.BytesSent != 10 || connection.TCPInfo.BytesAcked != 9 || connection.TCPInfo.BytesReceived != 8 || connection.TCPInfo.BytesRetransmitted != 7 || connection.TCPInfo.SendWindow != 22 || connection.TCPInfo.LastDataReceivedMilliseconds != 26 {
		t.Fatalf("TCP metrics not preserved: %#v", connection.TCPInfo)
	}
	summary := summarizeLiveConnections([]liveConnection{connection})
	if summary.Total != 1 || summary.StateGroups["active"] != 1 || summary.TotalReadQueue != 30 || summary.TotalWriteQueue != 40 || summary.BytesSent != 10 || summary.Retransmissions != 4 || summary.TotalRetransmissions != 3 {
		t.Fatalf("summary did not aggregate the full filtered connection: %#v", summary)
	}

	ipv6 := testLiveSocket(syscall.AF_INET6, net.IPv6zero, 9090, net.ParseIP("2001:db8::2"), 8443, netlink.TCP_LISTEN, 8)
	connection6, ok := normalizeLiveConnection(ipv6, syscall.AF_INET6, map[string][]string{}, newLiveStreamIndex(nil), nil, true, func(int) (string, error) { return "", errors.New("missing") })
	if !ok || connection6.Family != "tcp6" || connection6.Local.RawAddress != "[::]:9090" || connection6.Remote.RawAddress != "[2001:db8::2]:8443" {
		t.Fatalf("unexpected IPv6 normalization: ok=%v connection=%#v", ok, connection6)
	}
	if connection6.TCPInfo != nil {
		t.Fatal("missing TCP info must remain omitted")
	}
}

func TestNormalizeLiveConnectionAcceptsIPv4MappedAFINET6Addresses(t *testing.T) {
	aliases := map[string][]string{
		"192.0.2.10":    {"mapped-local.example"},
		"198.51.100.20": {"mapped-remote.example"},
	}
	tests := []struct {
		name      string
		local     net.IP
		remote    net.IP
		localIP   string
		remoteIP  string
		localRaw  string
		remoteRaw string
	}{
		{
			name: "both mapped", local: net.ParseIP("::ffff:192.0.2.10"), remote: net.ParseIP("::ffff:198.51.100.20"),
			localIP: "192.0.2.10", remoteIP: "198.51.100.20", localRaw: "192.0.2.10:8080", remoteRaw: "198.51.100.20:443",
		},
		{
			name: "mapped local native remote", local: net.ParseIP("::ffff:192.0.2.10"), remote: net.ParseIP("2001:db8::20"),
			localIP: "192.0.2.10", remoteIP: "2001:db8::20", localRaw: "192.0.2.10:8080", remoteRaw: "[2001:db8::20]:443",
		},
		{
			name: "native local mapped remote", local: net.ParseIP("2001:db8::10"), remote: net.ParseIP("::ffff:198.51.100.20"),
			localIP: "2001:db8::10", remoteIP: "198.51.100.20", localRaw: "[2001:db8::10]:8080", remoteRaw: "198.51.100.20:443",
		},
	}
	connections := make([]liveConnection, 0, len(tests))
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := testLiveSocket(syscall.AF_INET6, test.local, 8080, test.remote, 443, netlink.TCP_ESTABLISHED, uint32(i+1))
			connection, ok := normalizeLiveConnection(response, syscall.AF_INET6, aliases, newLiveStreamIndex(nil), nil, true, func(int) (string, error) {
				return "", errors.New("not found")
			})
			if !ok {
				t.Fatal("AF_INET6 mapped connection was rejected")
			}
			if connection.Family != "tcp6" || connection.Local.IP != test.localIP || connection.Remote.IP != test.remoteIP || connection.Local.RawAddress != test.localRaw || connection.Remote.RawAddress != test.remoteRaw {
				t.Fatalf("mapped normalization = %#v -> %#v, family=%q", connection.Local, connection.Remote, connection.Family)
			}
			if strings.Contains(connection.Local.RawAddress, "::ffff") || strings.Contains(connection.Remote.RawAddress, "::ffff") || strings.Contains(connection.Local.RawAddress, "[192.0.2.10]") || strings.Contains(connection.Remote.RawAddress, "[198.51.100.20]") {
				t.Fatalf("mapped endpoint has malformed or duplicate representation: %q -> %q", connection.Local.RawAddress, connection.Remote.RawAddress)
			}
			if test.localIP == "192.0.2.10" && len(connection.Local.Aliases) != 1 {
				t.Fatalf("mapped local endpoint was not enriched: %#v", connection.Local)
			}
			if test.remoteIP == "198.51.100.20" && len(connection.Remote.Aliases) != 1 {
				t.Fatalf("mapped remote endpoint was not enriched: %#v", connection.Remote)
			}
			connections = append(connections, connection)
		})
	}
	filtered := filterLiveConnections(connections, liveFilters{state: "all", localIP: "192.0.2.10", remotePort: 443})
	if len(filtered) != 2 {
		t.Fatalf("mapped addresses were not exactly filterable: got %d, want 2", len(filtered))
	}
}

func TestActiveConnectionsCompletePaginationAndSnapshotReuse(t *testing.T) {
	var collectCalls atomic.Int32
	responses := make([]*netlink.InetDiagTCPInfoResp, 0, 1103)
	for i := 0; i < 1103; i++ {
		responses = append(responses, testLiveSocket(syscall.AF_INET, net.IPv4(10, byte(i/256), byte(i%256), byte((i%250)+1)), uint16(10000+i), net.IPv4(192, 0, 2, byte((i%250)+1)), 443, netlink.TCP_ESTABLISHED, uint32(i+1)))
	}
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	deps := testLiveDependencies(&now)
	deps.collect = func(family uint8) ([]*netlink.InetDiagTCPInfoResp, error) {
		collectCalls.Add(1)
		if family == syscall.AF_INET {
			return responses, nil
		}
		return []*netlink.InetDiagTCPInfoResp{}, nil
	}
	deps.newToken = func() (string, error) { return "snapshot-page-test", nil }
	cache := newLiveSnapshotCache(4, 30*time.Second)

	first := serveLiveRequest(t, "/api/streamInfo/active-connections?state=all&limit=500", deps, cache)
	second := serveLiveRequest(t, "/api/streamInfo/active-connections?state=all&limit=500&offset=500&snapshot_token=snapshot-page-test", deps, cache)
	third := serveLiveRequest(t, "/api/streamInfo/active-connections?state=all&limit=500&offset=1000&snapshot=snapshot-page-test", deps, cache)
	if collectCalls.Load() != 2 {
		t.Fatalf("collector calls = %d, want exactly one call per family", collectCalls.Load())
	}
	for pageNumber, response := range []liveResponse{first, second, third} {
		if response.Pagination.Total != 1103 || response.Summary.Total != 1103 {
			t.Fatalf("page %d total = %d summary = %d, want 1103", pageNumber, response.Pagination.Total, response.Summary.Total)
		}
	}
	if len(first.Connections) != 500 || len(second.Connections) != 500 || len(third.Connections) != 103 || !first.Pagination.HasMore || !second.Pagination.HasMore || third.Pagination.HasMore {
		t.Fatalf("unexpected page sizes/flags: %d/%v %d/%v %d/%v", len(first.Connections), first.Pagination.HasMore, len(second.Connections), second.Pagination.HasMore, len(third.Connections), third.Pagination.HasMore)
	}
	seen := make(map[string]struct{}, 1103)
	for _, response := range []liveResponse{first, second, third} {
		for _, connection := range response.Connections {
			if _, exists := seen[connection.ID]; exists {
				t.Fatalf("connection %q occurred on multiple pages", connection.ID)
			}
			seen[connection.ID] = struct{}{}
		}
	}
	if len(seen) != 1103 {
		t.Fatalf("unique paginated connections = %d, want 1103", len(seen))
	}
}

func TestLiveSnapshotExpiryAndCacheBound(t *testing.T) {
	now := time.Date(2026, 8, 21, 11, 0, 0, 0, time.UTC)
	deps := testLiveDependencies(&now)
	deps.newToken = func() (string, error) { return "expiring-token", nil }
	cache := newLiveSnapshotCache(4, 30*time.Second)
	recorder := serveLiveRecorder("/api/streamInfo/active-connections", deps, cache)
	if recorder.Code != http.StatusOK {
		t.Fatalf("initial status = %d", recorder.Code)
	}
	now = now.Add(30 * time.Second)
	recorder = serveLiveRecorder("/api/streamInfo/active-connections?snapshot=expiring-token", deps, cache)
	if recorder.Code != http.StatusGone || strings.Contains(recorder.Body.String(), "expiring-token") {
		t.Fatalf("expired response = %d %s", recorder.Code, recorder.Body.String())
	}

	cache = newLiveSnapshotCache(4, 30*time.Second)
	for i := 0; i < 5; i++ {
		captured := now.Add(time.Duration(i) * time.Second)
		cache.put(&liveSnapshot{token: fmt.Sprintf("token-%d", i), capturedAt: captured, expiresAt: captured.Add(time.Minute)}, captured)
	}
	if len(cache.snapshots) != 4 {
		t.Fatalf("cache size = %d, want 4", len(cache.snapshots))
	}
	if _, exists := cache.snapshots["token-0"]; exists {
		t.Fatal("oldest snapshot was not evicted")
	}
}

func TestLiveSnapshotTTLStartsWhenCaptureCompletes(t *testing.T) {
	const ttl = 30 * time.Second
	startedAt := time.Date(2026, 8, 21, 11, 30, 0, 0, time.UTC)
	now := startedAt
	deps := testLiveDependencies(&now)
	deps.newToken = func() (string, error) { return "slow-capture", nil }
	deps.getAliases = func() ([]dnsmasq.AliasEntry, error) {
		now = startedAt.Add(2 * ttl)
		return []dnsmasq.AliasEntry{}, nil
	}
	cache := newLiveSnapshotCache(4, ttl)
	created := serveLiveRecorder("/api/streamInfo/active-connections", deps, cache)
	if created.Code != http.StatusOK {
		t.Fatalf("slow capture status = %d: %s", created.Code, created.Body.String())
	}
	var response liveResponse
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	completedAt := startedAt.Add(2 * ttl)
	if response.Provenance.CapturedAt != completedAt.Format(time.RFC3339Nano) || response.Provenance.ExpiresAt != completedAt.Add(ttl).Format(time.RFC3339Nano) {
		t.Fatalf("provenance timestamps = %q/%q, want completion %q/%q", response.Provenance.CapturedAt, response.Provenance.ExpiresAt, completedAt.Format(time.RFC3339Nano), completedAt.Add(ttl).Format(time.RFC3339Nano))
	}
	if snapshot := cache.snapshots["slow-capture"]; snapshot == nil || snapshot.capturedAt != completedAt || snapshot.expiresAt != completedAt.Add(ttl) {
		t.Fatalf("cached snapshot timestamps are not completion-based: %#v", snapshot)
	}

	valid := serveLiveRecorder("/api/streamInfo/active-connections?snapshot=slow-capture", deps, cache)
	if valid.Code != http.StatusOK {
		t.Fatalf("snapshot expired during its simulated capture: %d %s", valid.Code, valid.Body.String())
	}
	now = completedAt.Add(ttl - time.Nanosecond)
	valid = serveLiveRecorder("/api/streamInfo/active-connections?snapshot=slow-capture", deps, cache)
	if valid.Code != http.StatusOK {
		t.Fatalf("snapshot expired before full TTL: %d %s", valid.Code, valid.Body.String())
	}
	now = completedAt.Add(ttl)
	expired := serveLiveRecorder("/api/streamInfo/active-connections?snapshot=slow-capture", deps, cache)
	if expired.Code != http.StatusGone {
		t.Fatalf("snapshot status at TTL boundary = %d, want 410", expired.Code)
	}
}

func TestLiveSnapshotIsBoundToAuthenticatedPrincipal(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	deps := testLiveDependencies(&now)
	deps.newToken = func() (string, error) { return "principal-snapshot", nil }
	var npmToken string
	deps.listStreams = func(_ string, token string) ([]npm.Stream, error) {
		npmToken = token
		return []npm.Stream{}, nil
	}
	cache := newLiveSnapshotCache(4, 30*time.Second)
	const principalA = "principal-a-secret-token"
	const principalB = "principal-b-secret-token"

	created := serveLiveRecorderAs("/api/streamInfo/active-connections", deps, cache, principalA)
	if created.Code != http.StatusOK || npmToken != principalA {
		t.Fatalf("create status/token = %d/%q", created.Code, npmToken)
	}
	sameOwner := serveLiveRecorderAs("/api/streamInfo/active-connections?snapshot=principal-snapshot", deps, cache, principalA)
	if sameOwner.Code != http.StatusOK {
		t.Fatalf("same-owner reuse status = %d: %s", sameOwner.Code, sameOwner.Body.String())
	}
	wrongOwner := serveLiveRecorderAs("/api/streamInfo/active-connections?snapshot=principal-snapshot", deps, cache, principalB)
	unknown := serveLiveRecorderAs("/api/streamInfo/active-connections?snapshot=unknown-snapshot", deps, cache, principalB)
	if wrongOwner.Code != http.StatusGone || wrongOwner.Body.String() != unknown.Body.String() {
		t.Fatalf("wrong-owner response differs from unknown: wrong=%d %q unknown=%d %q", wrongOwner.Code, wrongOwner.Body.String(), unknown.Code, unknown.Body.String())
	}

	snapshot := cache.snapshots["principal-snapshot"]
	if snapshot == nil || snapshot.ownerDigest != liveOwnerDigest(principalA) {
		t.Fatalf("snapshot owner digest missing or incorrect: %#v", snapshot)
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprintf("%#v", snapshot), principalA) || strings.Contains(string(snapshotJSON), principalA) || strings.Contains(created.Body.String(), principalA) || strings.Contains(sameOwner.Body.String(), principalA) {
		t.Fatal("raw authentication token retained in snapshot or exposed in JSON")
	}
}

func TestLiveCaptureGateRejectsConcurrentCaptureAndAllowsReuse(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 30, 0, 0, time.UTC)
	deps := testLiveDependencies(&now)
	entered := make(chan struct{})
	release := make(chan struct{})
	var clockCalls atomic.Int32
	var tokenCalls atomic.Int32
	var aliasCalls atomic.Int32
	var streamCalls atomic.Int32
	var descriptionCalls atomic.Int32
	var collectCalls atomic.Int32
	var enteredOnce sync.Once
	deps.now = func() time.Time { clockCalls.Add(1); return now }
	deps.newToken = func() (string, error) { tokenCalls.Add(1); return "new-capture", nil }
	deps.getAliases = func() ([]dnsmasq.AliasEntry, error) {
		aliasCalls.Add(1)
		enteredOnce.Do(func() { close(entered) })
		<-release
		return []dnsmasq.AliasEntry{}, nil
	}
	deps.listStreams = func(string, string) ([]npm.Stream, error) { streamCalls.Add(1); return []npm.Stream{}, nil }
	deps.getDescriptions = func(context.Context, string, []int) (map[int]string, error) {
		descriptionCalls.Add(1)
		return map[int]string{}, nil
	}
	deps.collect = func(uint8) ([]*netlink.InetDiagTCPInfoResp, error) {
		collectCalls.Add(1)
		return []*netlink.InetDiagTCPInfoResp{}, nil
	}
	cache := newLiveSnapshotCache(4, 30*time.Second)
	cache.put(&liveSnapshot{
		token: "existing-snapshot", ownerDigest: liveOwnerDigest("principal"), capturedAt: now,
		expiresAt: now.Add(30 * time.Second), connections: []liveConnection{}, warnings: []string{},
	}, now)

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		firstDone <- serveLiveRecorderAs("/api/streamInfo/active-connections", deps, cache, "principal")
	}()
	<-entered

	busy := serveLiveRecorderAs("/api/streamInfo/active-connections", deps, cache, "principal")
	if busy.Code != http.StatusTooManyRequests || busy.Body.String() != "{\"error\":\"live capture already in progress; retry shortly\"}\n" {
		t.Fatalf("busy response = %d %q", busy.Code, busy.Body.String())
	}
	if clockCalls.Load() != 0 || tokenCalls.Load() != 1 || aliasCalls.Load() != 1 || streamCalls.Load() != 0 || descriptionCalls.Load() != 0 || collectCalls.Load() != 0 {
		t.Fatalf("busy request called dependencies: clock=%d token=%d aliases=%d streams=%d descriptions=%d collect=%d", clockCalls.Load(), tokenCalls.Load(), aliasCalls.Load(), streamCalls.Load(), descriptionCalls.Load(), collectCalls.Load())
	}
	reused := serveLiveRecorderAs("/api/streamInfo/active-connections?snapshot=existing-snapshot&state=all", deps, cache, "principal")
	if reused.Code != http.StatusOK {
		t.Fatalf("snapshot reuse blocked by capture gate: %d %s", reused.Code, reused.Body.String())
	}

	close(release)
	if first := <-firstDone; first.Code != http.StatusOK {
		t.Fatalf("first capture status = %d: %s", first.Code, first.Body.String())
	}
}

func TestLiveCaptureGateReleasesAfterFailure(t *testing.T) {
	now := time.Now()
	deps := testLiveDependencies(&now)
	var tokenCalls atomic.Int32
	deps.newToken = func() (string, error) {
		if tokenCalls.Add(1) == 1 {
			return "", errors.New("token generation failed")
		}
		return "recovered-snapshot", nil
	}
	cache := newLiveSnapshotCache(4, 30*time.Second)
	failed := serveLiveRecorderAs("/api/streamInfo/active-connections", deps, cache, "principal")
	if failed.Code != http.StatusInternalServerError || strings.Contains(failed.Body.String(), "token generation") {
		t.Fatalf("failed capture response = %d %s", failed.Code, failed.Body.String())
	}
	recovered := serveLiveRecorderAs("/api/streamInfo/active-connections", deps, cache, "principal")
	if recovered.Code != http.StatusOK {
		t.Fatalf("capture gate was not released: %d %s", recovered.Code, recovered.Body.String())
	}
}

func TestLiveCaptureCachesInterfaceLookupsIncludingFailures(t *testing.T) {
	now := time.Now()
	deps := testLiveDependencies(&now)
	lookupCalls := make(map[int]int)
	deps.interfaceName = func(index int) (string, error) {
		lookupCalls[index]++
		if index == 9 {
			return "", errors.New("missing interface")
		}
		return "eth-cached", nil
	}
	deps.collect = func(family uint8) ([]*netlink.InetDiagTCPInfoResp, error) {
		if family == syscall.AF_INET6 {
			return []*netlink.InetDiagTCPInfoResp{}, nil
		}
		responses := make([]*netlink.InetDiagTCPInfoResp, 0, 8)
		for i := 0; i < 5; i++ {
			response := testLiveSocket(family, net.IPv4(10, 0, 0, 1), uint16(1000+i), net.IPv4(10, 0, 0, 2), 443, netlink.TCP_ESTABLISHED, uint32(i+1))
			response.InetDiagMsg.ID.Interface = 7
			responses = append(responses, response)
		}
		for i := 0; i < 3; i++ {
			response := testLiveSocket(family, net.IPv4(10, 0, 0, 1), uint16(2000+i), net.IPv4(10, 0, 0, 3), 443, netlink.TCP_ESTABLISHED, uint32(i+10))
			response.InetDiagMsg.ID.Interface = 9
			responses = append(responses, response)
		}
		return responses, nil
	}
	response := serveLiveRequest(t, "/api/streamInfo/active-connections?state=all", deps, newLiveSnapshotCache(4, 30*time.Second))
	if len(response.Connections) != 8 || lookupCalls[7] != 1 || lookupCalls[9] != 1 {
		t.Fatalf("interface cache calls = %#v, connections=%d", lookupCalls, len(response.Connections))
	}
}

func TestLiveValidationHappensBeforeDependencies(t *testing.T) {
	now := time.Now()
	deps := testLiveDependencies(&now)
	var calls atomic.Int32
	deps.collect = func(uint8) ([]*netlink.InetDiagTCPInfoResp, error) { calls.Add(1); return nil, nil }
	deps.listStreams = func(string, string) ([]npm.Stream, error) { calls.Add(1); return nil, nil }
	deps.getAliases = func() ([]dnsmasq.AliasEntry, error) { calls.Add(1); return nil, nil }
	deps.getDescriptions = func(context.Context, string, []int) (map[int]string, error) { calls.Add(1); return nil, nil }
	tests := []string{
		"?state=broken", "?local_ip=nope", "?remote_ip=nope", "?local_port=0", "?remote_port=65536",
		"?npm_only=yes", "?offset=-1", "?limit=0", "?limit=501", "?snapshot=bad+token", "?snapshot=one&snapshot_token=two",
	}
	for _, query := range tests {
		recorder := serveLiveRecorder("/api/streamInfo/active-connections"+query, deps, newLiveSnapshotCache(4, 30*time.Second))
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("query %q status = %d, want 400: %s", query, recorder.Code, recorder.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("dependencies called %d times for invalid queries", calls.Load())
	}
}

func TestLiveFiltersComposeAndDefaultState(t *testing.T) {
	now := time.Now()
	deps := testLiveDependencies(&now)
	stream := testNPMStream(1, 18080, "203.0.113.8", 443)
	deps.listStreams = func(string, string) ([]npm.Stream, error) { return []npm.Stream{stream}, nil }
	deps.getAliases = func() ([]dnsmasq.AliasEntry, error) {
		return []dnsmasq.AliasEntry{{Alias: "upstream.example", IP: "203.0.113.8"}}, nil
	}
	deps.interfaceName = func(int) (string, error) { return "eth-filter", nil }
	deps.collect = func(family uint8) ([]*netlink.InetDiagTCPInfoResp, error) {
		if family == syscall.AF_INET6 {
			return nil, nil
		}
		matching := testLiveSocket(family, net.IPv4(10, 0, 0, 2), 50000, net.IPv4(203, 0, 113, 8), 443, netlink.TCP_ESTABLISHED, 1)
		matching.InetDiagMsg.ID.Interface = 2
		return []*netlink.InetDiagTCPInfoResp{
			matching,
			testLiveSocket(family, net.IPv4(10, 0, 0, 2), 50001, net.IPv4(203, 0, 113, 9), 443, netlink.TCP_ESTABLISHED, 2),
			testLiveSocket(family, net.IPv4zero, 18080, net.IPv4zero, 0, netlink.TCP_LISTEN, 3),
		}, nil
	}
	query := url.Values{
		"state": {"established"}, "local_ip": {"10.0.0.2"}, "remote_ip": {"203.0.113.8"},
		"local_port": {"50000"}, "remote_port": {"443"}, "search": {"ETH-FILTER"}, "npm_only": {"true"},
	}.Encode()
	response := serveLiveRequest(t, "/api/streamInfo/active-connections?"+query, deps, newLiveSnapshotCache(4, 30*time.Second))
	if response.Pagination.Total != 1 || response.Connections[0].Correlation.Status != "matched" {
		t.Fatalf("composed filters returned %#v", response)
	}

	defaultResponse := serveLiveRequest(t, "/api/streamInfo/active-connections", deps, newLiveSnapshotCache(4, 30*time.Second))
	if defaultResponse.Pagination.Total != 2 {
		t.Fatalf("default established filter total = %d, want 2", defaultResponse.Pagination.Total)
	}
}

func TestLiveCollectionPartialAndUnavailableAreSanitized(t *testing.T) {
	now := time.Now()
	deps := testLiveDependencies(&now)
	deps.collect = func(family uint8) ([]*netlink.InetDiagTCPInfoResp, error) {
		if family == syscall.AF_INET {
			return []*netlink.InetDiagTCPInfoResp{testLiveSocket(family, net.IPv4(10, 0, 0, 1), 1234, net.IPv4(10, 0, 0, 2), 4321, netlink.TCP_ESTABLISHED, 1)}, netlink.ErrDumpInterrupted
		}
		return nil, errors.New("SECRET kernel details")
	}
	recorder := serveLiveRecorder("/api/streamInfo/active-connections?state=all", deps, newLiveSnapshotCache(4, 30*time.Second))
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "SECRET") {
		t.Fatalf("partial response = %d %s", recorder.Code, recorder.Body.String())
	}
	var partial liveResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &partial); err != nil {
		t.Fatal(err)
	}
	if !partial.Availability.Available || !partial.Availability.Partial || len(partial.Connections) != 1 || !partial.Availability.TCP4 || partial.Availability.TCP6 {
		t.Fatalf("partial availability = %#v connections=%d", partial.Availability, len(partial.Connections))
	}

	deps.collect = func(uint8) ([]*netlink.InetDiagTCPInfoResp, error) { return nil, errors.New("SECRET total failure") }
	recorder = serveLiveRecorder("/api/streamInfo/active-connections?state=all", deps, newLiveSnapshotCache(4, 30*time.Second))
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "SECRET") {
		t.Fatalf("unavailable response = %d %s", recorder.Code, recorder.Body.String())
	}
	var unavailable liveResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &unavailable); err != nil {
		t.Fatal(err)
	}
	if unavailable.Availability.Available || unavailable.Availability.Partial || unavailable.Connections == nil || len(unavailable.Connections) != 0 {
		t.Fatalf("unavailable response not degraded correctly: %#v", unavailable)
	}
}

func TestLiveNPMCorrelationEvidence(t *testing.T) {
	base := liveConnection{
		Local:  enrichAnalyticsEndpoint("10.0.0.2:50000", map[string][]string{}),
		Remote: enrichAnalyticsEndpoint("192.0.2.10:5432", map[string][]string{}),
	}
	exact := testNPMStream(2, 15432, "192.0.2.10", 5432)
	correlation := correlateLiveConnection(base, map[string][]string{}, newLiveStreamIndex([]npm.Stream{exact}), map[int]string{2: "database"}, true)
	if correlation.Status != "matched" || correlation.Role != "outbound_upstream" || correlation.Confidence != "exact_current_config" || len(correlation.Streams) != 1 || correlation.Streams[0].Description != "database" {
		t.Fatalf("exact correlation = %#v", correlation)
	}

	portOnly := testNPMStream(3, 15432, "unresolved.example", 5432)
	correlation = correlateLiveConnection(base, map[string][]string{}, newLiveStreamIndex([]npm.Stream{portOnly}), nil, true)
	if correlation.Status != "indeterminate" || correlation.Confidence != "port_only" {
		t.Fatalf("port-only correlation = %#v", correlation)
	}

	secondExact := testNPMStream(1, 25432, "192.0.2.10", 5432)
	correlation = correlateLiveConnection(base, map[string][]string{}, newLiveStreamIndex([]npm.Stream{exact, secondExact}), nil, true)
	if correlation.Status != "ambiguous" || len(correlation.Streams) != 2 || correlation.Streams[0].ID != 1 || correlation.Streams[1].ID != 2 {
		t.Fatalf("ambiguous correlation = %#v", correlation)
	}

	inboundConnection := base
	inboundConnection.Local = enrichAnalyticsEndpoint("0.0.0.0:15432", map[string][]string{})
	inboundConnection.Remote = enrichAnalyticsEndpoint("0.0.0.0:0", map[string][]string{})
	correlation = correlateLiveConnection(inboundConnection, map[string][]string{}, newLiveStreamIndex([]npm.Stream{exact}), nil, true)
	if correlation.Status != "matched" || correlation.Role != "inbound_listener" || correlation.Confidence != "listener_port" {
		t.Fatalf("inbound correlation = %#v", correlation)
	}

	aliasMap := map[string][]string{"192.0.2.10": {"database.internal"}}
	aliasStream := testNPMStream(4, 15432, "database.internal", 5432)
	correlation = correlateLiveConnection(base, aliasMap, newLiveStreamIndex([]npm.Stream{aliasStream}), nil, true)
	if correlation.Status != "matched" || correlation.Confidence != "exact_current_config" {
		t.Fatalf("alias correlation = %#v", correlation)
	}

	correlation = correlateLiveConnection(base, nil, newLiveStreamIndex([]npm.Stream{exact}), nil, false)
	if correlation.Status != "unavailable" || correlation.Streams == nil || len(correlation.Streams) != 0 {
		t.Fatalf("unavailable correlation = %#v", correlation)
	}

	many := make([]npm.Stream, 25)
	for i := range many {
		many[i] = testNPMStream(25-i, 10000+i, "192.0.2.10", 5432)
	}
	correlation = correlateLiveConnection(base, nil, newLiveStreamIndex(many), nil, true)
	if correlation.Status != "ambiguous" || len(correlation.Streams) != liveMaxStreamCandidates || correlation.Streams[0].ID != 1 || correlation.Streams[len(correlation.Streams)-1].ID != liveMaxStreamCandidates {
		t.Fatalf("bounded/sorted candidates = %#v", correlation)
	}
}

func TestLiveStreamIndexLimitsPerConnectionCandidates(t *testing.T) {
	streams := make([]npm.Stream, 0, 1002)
	for i := 0; i < 1000; i++ {
		streams = append(streams, testNPMStream(1000-i, 20000+i, "198.51.100.1", 30000+i))
	}
	streams = append(streams,
		testNPMStream(2001, 15432, "192.0.2.10", 5432),
		testNPMStream(2000, 15432, "unresolved.example", 5432),
	)
	index := newLiveStreamIndex(streams)
	if got := len(index.incomingByPort[15432]); got != 2 {
		t.Fatalf("incoming candidate count = %d, want 2", got)
	}
	if got := len(index.forwardingByPort[5432]); got != 2 {
		t.Fatalf("forwarding candidate count = %d, want 2", got)
	}
	if index.forwardingByPort[5432][0].ID != 2000 || index.forwardingByPort[5432][1].ID != 2001 {
		t.Fatalf("forwarding index is not sorted: %#v", index.forwardingByPort[5432])
	}

	connection := liveConnection{
		Local:  enrichAnalyticsEndpoint("10.0.0.2:50000", map[string][]string{}),
		Remote: enrichAnalyticsEndpoint("192.0.2.10:5432", map[string][]string{}),
	}
	correlation := correlateLiveConnection(connection, nil, index, nil, true)
	if correlation.Status != "matched" || len(correlation.Streams) != 1 || correlation.Streams[0].ID != 2001 {
		t.Fatalf("indexed correlation changed behavior: %#v", correlation)
	}
}

func TestLiveJSONUsesEmptyCollections(t *testing.T) {
	now := time.Now()
	deps := testLiveDependencies(&now)
	deps.collect = func(uint8) ([]*netlink.InetDiagTCPInfoResp, error) { return nil, errors.New("unavailable") }
	deps.listStreams = func(string, string) ([]npm.Stream, error) { return nil, errors.New("SECRET NPM failure") }
	recorder := serveLiveRecorder("/api/streamInfo/active-connections?state=all", deps, newLiveSnapshotCache(4, 30*time.Second))
	if strings.Contains(recorder.Body.String(), "SECRET") {
		t.Fatalf("raw auxiliary error leaked: %s", recorder.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if raw["connections"] == nil {
		t.Fatal("connections encoded as null")
	}
	summary := raw["summary"].(map[string]any)
	if summary["state_groups"] == nil {
		t.Fatal("state_groups encoded as null")
	}
	connections := raw["connections"].([]any)
	stateGroups := summary["state_groups"].(map[string]any)
	if len(connections) != 0 || len(stateGroups) != 0 {
		t.Fatalf("expected empty collections, got connections=%#v states=%#v", connections, stateGroups)
	}
	if raw["warnings"] == nil {
		t.Fatal("warnings encoded as null")
	}
}

func TestSortLiveConnectionsUsesNumericAddressesAndStableKeys(t *testing.T) {
	connections := []liveConnection{
		testNormalizedConnection("tcp6", "::1", 2, "2001:db8::1", 80, "listen", 3),
		testNormalizedConnection("tcp4", "10.0.0.10", 2, "192.0.2.1", 80, "established", 2),
		testNormalizedConnection("tcp4", "10.0.0.2", 10, "192.0.2.2", 80, "established", 1),
		testNormalizedConnection("tcp4", "10.0.0.2", 2, "192.0.2.2", 80, "established", 4),
	}
	sortLiveConnections(connections)
	want := []string{"10.0.0.2:2", "10.0.0.2:10", "10.0.0.10:2", "::1:2"}
	for i, connection := range connections {
		got := connection.Local.IP + ":" + connection.Local.Port
		if got != want[i] {
			t.Fatalf("sorted[%d] = %q, want %q", i, got, want[i])
		}
	}
}

func testLiveDependencies(now *time.Time) liveDependencies {
	return liveDependencies{
		collect:     func(uint8) ([]*netlink.InetDiagTCPInfoResp, error) { return []*netlink.InetDiagTCPInfoResp{}, nil },
		listStreams: func(string, string) ([]npm.Stream, error) { return []npm.Stream{}, nil },
		getAliases:  func() ([]dnsmasq.AliasEntry, error) { return []dnsmasq.AliasEntry{}, nil },
		getDescriptions: func(context.Context, string, []int) (map[int]string, error) {
			return map[int]string{}, nil
		},
		interfaceName: func(int) (string, error) { return "", errors.New("not found") },
		now:           func() time.Time { return *now },
		newToken:      func() (string, error) { return "test-snapshot-token", nil },
	}
}

func testLiveSocket(family uint8, localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, state uint8, inode uint32) *netlink.InetDiagTCPInfoResp {
	return &netlink.InetDiagTCPInfoResp{InetDiagMsg: &netlink.Socket{
		Family: family,
		State:  state,
		ID: netlink.SocketID{
			Source: localIP, SourcePort: localPort, Destination: remoteIP, DestinationPort: remotePort,
			Cookie: [2]uint32{inode, inode + 1},
		},
		UID: 1000, INode: inode,
	}}
}

func testNPMStream(id, incoming int, host string, forwarding int) npm.Stream {
	stream := npm.Stream{ID: id, Incoming_port: incoming, Forwarding_host: host, Forwarding_port: forwarding, Tcp_forwarding: true, Enabled: true}
	return stream
}

func testNormalizedConnection(family, localIP string, localPort int, remoteIP string, remotePort int, state string, inode uint32) liveConnection {
	return liveConnection{
		ID: "id-" + strconv.Itoa(int(inode)), Family: family, State: state, Inode: inode,
		Local:       analyticsEndpoint{IP: localIP, Port: strconv.Itoa(localPort), Aliases: []string{}},
		Remote:      analyticsEndpoint{IP: remoteIP, Port: strconv.Itoa(remotePort), Aliases: []string{}},
		Correlation: liveCorrelation{Status: "unmatched", Streams: []analyticsStream{}},
	}
}

func serveLiveRecorder(target string, deps liveDependencies, cache *liveSnapshotCache) *httptest.ResponseRecorder {
	return serveLiveRecorderAs(target, deps, cache, "")
}

func serveLiveRecorderAs(target string, deps liveDependencies, cache *liveSnapshotCache, authToken string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request = SetTokenInContext(request, authToken)
	recorder := httptest.NewRecorder()
	serveActiveConnections(recorder, request, deps, cache)
	return recorder
}

func serveLiveRequest(t *testing.T, target string, deps liveDependencies, cache *liveSnapshotCache) liveResponse {
	t.Helper()
	recorder := serveLiveRecorder(target, deps, cache)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d: %s", target, recorder.Code, recorder.Body.String())
	}
	var response liveResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode GET %s: %v", target, err)
	}
	return response
}
