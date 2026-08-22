package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"512SvMan/db"
	"512SvMan/dnsmasq"
	"512SvMan/npm"

	"github.com/vishvananda/netlink"
)

const (
	liveDefaultLimit        = 100
	liveMaxLimit            = 500
	liveSnapshotTTL         = 30 * time.Second
	liveMaxSnapshots        = 4
	liveMaxStreamCandidates = 20
)

type liveDependencies struct {
	collect         func(uint8) ([]*netlink.InetDiagTCPInfoResp, error)
	listStreams     func(string, string) ([]npm.Stream, error)
	getAliases      func() ([]dnsmasq.AliasEntry, error)
	getDescriptions func(context.Context, string, []int) (map[int]string, error)
	interfaceName   func(int) (string, error)
	now             func() time.Time
	newToken        func() (string, error)
}

var productionLiveDependencies = liveDependencies{
	collect:         netlink.SocketDiagTCPInfo,
	listStreams:     npm.ListStreams,
	getAliases:      getCombinedAliases,
	getDescriptions: db.GetResourceDescriptions,
	interfaceName: func(index int) (string, error) {
		iface, err := net.InterfaceByIndex(index)
		if err != nil {
			return "", err
		}
		return iface.Name, nil
	},
	now:      time.Now,
	newToken: newLiveSnapshotToken,
}

type liveQueues struct {
	Read  uint32 `json:"read"`
	Write uint32 `json:"write"`
}

type liveTCPMetrics struct {
	RTTMilliseconds              float64 `json:"rtt_ms"`
	RTTVarianceMilliseconds      float64 `json:"rtt_variance_ms"`
	RTOMilliseconds              float64 `json:"rto_ms"`
	MinRTTMilliseconds           float64 `json:"min_rtt_ms"`
	BytesSent                    uint64  `json:"bytes_sent"`
	BytesAcked                   uint64  `json:"bytes_acked"`
	BytesReceived                uint64  `json:"bytes_received"`
	BytesRetransmitted           uint64  `json:"bytes_retransmitted"`
	SegmentsIn                   uint32  `json:"segments_in"`
	SegmentsOut                  uint32  `json:"segments_out"`
	Retransmissions              uint32  `json:"retransmissions"`
	TotalRetransmissions         uint32  `json:"total_retransmissions"`
	Unacked                      uint32  `json:"unacked"`
	Lost                         uint32  `json:"lost"`
	CongestionWindow             uint32  `json:"congestion_window"`
	ReceiveWindow                uint32  `json:"receive_window"`
	SendWindow                   uint32  `json:"send_window"`
	DeliveryRateBytesPerSecond   uint64  `json:"delivery_rate_bytes_per_second"`
	PacingRateBytesPerSecond     uint64  `json:"pacing_rate_bytes_per_second"`
	LastDataSentMilliseconds     uint32  `json:"last_data_sent_ms"`
	LastDataReceivedMilliseconds uint32  `json:"last_data_received_ms"`
}

type liveCorrelation struct {
	Role       string            `json:"role"`
	Confidence string            `json:"confidence"`
	Status     string            `json:"status"`
	Streams    []analyticsStream `json:"streams"`
}

type liveConnection struct {
	ID             string            `json:"id"`
	Family         string            `json:"family"`
	State          string            `json:"state"`
	StateGroup     string            `json:"state_group"`
	Local          analyticsEndpoint `json:"local"`
	Remote         analyticsEndpoint `json:"remote"`
	InterfaceIndex uint32            `json:"interface_index"`
	InterfaceName  string            `json:"interface_name"`
	UID            uint32            `json:"uid"`
	Inode          uint32            `json:"inode"`
	Queues         liveQueues        `json:"queues"`
	TCPInfo        *liveTCPMetrics   `json:"tcp_info,omitempty"`
	Correlation    liveCorrelation   `json:"correlation"`
}

type liveProvenance struct {
	Kind               string `json:"kind"`
	Scope              string `json:"scope"`
	CapturedAt         string `json:"captured_at"`
	SnapshotToken      string `json:"snapshot_token"`
	ExpiresAt          string `json:"expires_at"`
	ProcessAttribution string `json:"process_attribution"`
	CorrelationScope   string `json:"correlation_scope"`
}

type liveAvailability struct {
	Available          bool `json:"available"`
	Partial            bool `json:"partial"`
	TCP4               bool `json:"tcp4"`
	TCP6               bool `json:"tcp6"`
	TCPInfo            bool `json:"tcp_info"`
	ProcessAttribution bool `json:"process_attribution"`
	NPMStreams         bool `json:"npm_streams"`
	DNSAliases         bool `json:"dns_aliases"`
	Descriptions       bool `json:"descriptions"`
}

type liveSummary struct {
	Total                int            `json:"total"`
	StateGroups          map[string]int `json:"state_groups"`
	UniqueLocalIPs       int            `json:"unique_local_ips"`
	UniqueRemoteIPs      int            `json:"unique_remote_ips"`
	UniqueLocalPorts     int            `json:"unique_local_ports"`
	UniqueRemotePorts    int            `json:"unique_remote_ports"`
	TotalReadQueue       uint64         `json:"total_read_queue"`
	TotalWriteQueue      uint64         `json:"total_write_queue"`
	BytesSent            uint64         `json:"bytes_sent"`
	BytesAcked           uint64         `json:"bytes_acked"`
	BytesReceived        uint64         `json:"bytes_received"`
	BytesRetransmitted   uint64         `json:"bytes_retransmitted"`
	SegmentsIn           uint64         `json:"segments_in"`
	SegmentsOut          uint64         `json:"segments_out"`
	Retransmissions      uint64         `json:"retransmissions"`
	TotalRetransmissions uint64         `json:"total_retransmissions"`
}

type livePagination struct {
	Offset   int  `json:"offset"`
	Limit    int  `json:"limit"`
	Total    int  `json:"total"`
	Returned int  `json:"returned"`
	HasMore  bool `json:"has_more"`
}

type liveResponse struct {
	Provenance   liveProvenance   `json:"provenance"`
	Availability liveAvailability `json:"availability"`
	Summary      liveSummary      `json:"summary"`
	Connections  []liveConnection `json:"connections"`
	Pagination   livePagination   `json:"pagination"`
	Warnings     []string         `json:"warnings"`
}

type liveFilters struct {
	state      string
	localIP    string
	remoteIP   string
	localPort  int
	remotePort int
	search     string
	npmOnly    bool
	token      string
	offset     int
	limit      int
}

type liveSnapshot struct {
	token        string
	ownerDigest  [sha256.Size]byte
	capturedAt   time.Time
	expiresAt    time.Time
	connections  []liveConnection
	availability liveAvailability
	warnings     []string
}

type liveSnapshotCache struct {
	mu        sync.Mutex
	max       int
	ttl       time.Duration
	snapshots map[string]*liveSnapshot
	capture   chan struct{}
}

var productionLiveSnapshotCache = newLiveSnapshotCache(liveMaxSnapshots, liveSnapshotTTL)

func newLiveSnapshotCache(max int, ttl time.Duration) *liveSnapshotCache {
	return &liveSnapshotCache{
		max:       max,
		ttl:       ttl,
		snapshots: make(map[string]*liveSnapshot),
		capture:   make(chan struct{}, 1),
	}
}

func (cache *liveSnapshotCache) get(token string, ownerDigest [sha256.Size]byte, now time.Time) (*liveSnapshot, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.removeExpired(now)
	snapshot, ok := cache.snapshots[token]
	if !ok || subtle.ConstantTimeCompare(snapshot.ownerDigest[:], ownerDigest[:]) != 1 {
		return nil, false
	}
	return snapshot, true
}

func (cache *liveSnapshotCache) beginCapture() bool {
	select {
	case cache.capture <- struct{}{}:
		return true
	default:
		return false
	}
}

func (cache *liveSnapshotCache) endCapture() {
	<-cache.capture
}

func (cache *liveSnapshotCache) put(snapshot *liveSnapshot, now time.Time) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.removeExpired(now)
	for len(cache.snapshots) >= cache.max {
		var oldestToken string
		var oldest time.Time
		for token, candidate := range cache.snapshots {
			if oldestToken == "" || candidate.capturedAt.Before(oldest) || (candidate.capturedAt.Equal(oldest) && token < oldestToken) {
				oldestToken = token
				oldest = candidate.capturedAt
			}
		}
		delete(cache.snapshots, oldestToken)
	}
	cache.snapshots[snapshot.token] = snapshot
}

func (cache *liveSnapshotCache) removeExpired(now time.Time) {
	for token, snapshot := range cache.snapshots {
		if !now.Before(snapshot.expiresAt) {
			delete(cache.snapshots, token)
		}
	}
}

func getActiveConnections(w http.ResponseWriter, r *http.Request) {
	serveActiveConnections(w, r, productionLiveDependencies, productionLiveSnapshotCache)
}

func serveActiveConnections(w http.ResponseWriter, r *http.Request, deps liveDependencies, cache *liveSnapshotCache) {
	filters, err := parseLiveFilters(r)
	if err != nil {
		respondJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	authToken := GetTokenFromContext(r)
	ownerDigest := liveOwnerDigest(authToken)
	var snapshot *liveSnapshot
	if filters.token != "" {
		now := deps.now().UTC()
		var ok bool
		snapshot, ok = cache.get(filters.token, ownerDigest, now)
		if !ok {
			respondJSONError(w, http.StatusGone, "snapshot token is unknown or expired; request a new capture")
			return
		}
	} else {
		if !cache.beginCapture() {
			respondJSONError(w, http.StatusTooManyRequests, "live capture already in progress; retry shortly")
			return
		}
		defer cache.endCapture()
		snapshot, err = captureLiveSnapshot(r.Context(), authToken, deps, cache.ttl)
		if err != nil {
			respondJSONError(w, http.StatusInternalServerError, "failed to create live snapshot")
			return
		}
		cache.put(snapshot, snapshot.capturedAt)
	}

	filtered := filterLiveConnections(snapshot.connections, filters)
	summary := summarizeLiveConnections(filtered)
	start := filters.offset
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + filters.limit
	if end > len(filtered) {
		end = len(filtered)
	}
	page := append([]liveConnection{}, filtered[start:end]...)

	response := liveResponse{
		Provenance: liveProvenance{
			Kind:               "kernel_live_snapshot",
			Scope:              "master_host_network_namespace",
			CapturedAt:         snapshot.capturedAt.Format(time.RFC3339Nano),
			SnapshotToken:      snapshot.token,
			ExpiresAt:          snapshot.expiresAt.Format(time.RFC3339Nano),
			ProcessAttribution: "not_collected",
			CorrelationScope:   "current_npm_configuration_only",
		},
		Availability: snapshot.availability,
		Summary:      summary,
		Connections:  page,
		Pagination: livePagination{
			Offset: filters.offset, Limit: filters.limit, Total: len(filtered),
			Returned: len(page), HasMore: end < len(filtered),
		},
		Warnings: append([]string{}, snapshot.warnings...),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func captureLiveSnapshot(ctx context.Context, authToken string, deps liveDependencies, ttl time.Duration) (*liveSnapshot, error) {
	token, err := deps.newToken()
	if err != nil {
		return nil, err
	}
	snapshot := &liveSnapshot{
		token:       token,
		ownerDigest: liveOwnerDigest(authToken),
		connections: make([]liveConnection, 0),
		warnings: []string{
			"PID, process, and connection duration attribution are not collected",
			"NPM correlations use current configuration only and do not assert a source-to-destination route",
		},
	}

	aliasEntries, aliasErr := deps.getAliases()
	aliases := buildAnalyticsAliasMap(aliasEntries)
	snapshot.availability.DNSAliases = aliasErr == nil
	if aliasErr != nil {
		aliases = make(map[string][]string)
		snapshot.warnings = append(snapshot.warnings, "managed DNS aliases are unavailable")
	}

	streams, streamErr := deps.listStreams(baseURL, authToken)
	snapshot.availability.NPMStreams = streamErr == nil
	streamIndex := newLiveStreamIndex(nil)
	descriptions := make(map[int]string)
	if streamErr != nil {
		streams = []npm.Stream{}
		snapshot.warnings = append(snapshot.warnings, "NPM stream definitions are unavailable")
	} else {
		streamIndex = newLiveStreamIndex(streams)
		ids := make([]int, len(streams))
		for i := range streams {
			ids[i] = streams[i].ID
		}
		descriptions, err = deps.getDescriptions(ctx, "npm_stream", ids)
		snapshot.availability.Descriptions = err == nil
		if err != nil {
			descriptions = make(map[int]string)
			snapshot.warnings = append(snapshot.warnings, "NPM stream descriptions are unavailable")
		}
	}

	type familyCollection struct {
		family uint8
		name   string
	}
	families := []familyCollection{{family: syscall.AF_INET, name: "TCP4"}, {family: syscall.AF_INET6, name: "TCP6"}}
	interfaceName := newLiveInterfaceNameCache(deps.interfaceName)
	succeeded := 0
	collectionIssue := false
	for _, family := range families {
		responses, collectErr := deps.collect(family.family)
		interrupted := errors.Is(collectErr, netlink.ErrDumpInterrupted)
		if collectErr == nil || interrupted {
			succeeded++
			if family.family == syscall.AF_INET {
				snapshot.availability.TCP4 = true
			} else {
				snapshot.availability.TCP6 = true
			}
		}
		if collectErr != nil {
			collectionIssue = true
			if interrupted {
				snapshot.warnings = append(snapshot.warnings, family.name+" kernel socket dump was interrupted; partial results were retained")
			} else {
				snapshot.warnings = append(snapshot.warnings, family.name+" kernel socket collection is unavailable")
			}
		}
		for _, response := range responses {
			connection, ok := normalizeLiveConnection(response, family.family, aliases, streamIndex, descriptions, snapshot.availability.NPMStreams, interfaceName)
			if !ok {
				continue
			}
			if connection.TCPInfo != nil {
				snapshot.availability.TCPInfo = true
			}
			snapshot.connections = append(snapshot.connections, connection)
		}
	}
	snapshot.availability.Available = succeeded > 0
	snapshot.availability.Partial = collectionIssue && succeeded > 0
	if succeeded == 0 {
		snapshot.connections = make([]liveConnection, 0)
		snapshot.warnings = append(snapshot.warnings, "kernel TCP socket collection is unavailable")
	}
	sortLiveConnections(snapshot.connections)
	snapshot.capturedAt = deps.now().UTC()
	snapshot.expiresAt = snapshot.capturedAt.Add(ttl)
	return snapshot, nil
}

func normalizeLiveConnection(response *netlink.InetDiagTCPInfoResp, family uint8, aliases map[string][]string, streams liveStreamIndex, descriptions map[int]string, streamsAvailable bool, interfaceName func(int) (string, error)) (liveConnection, bool) {
	if response == nil || response.InetDiagMsg == nil {
		return liveConnection{}, false
	}
	socket := response.InetDiagMsg
	localIP := canonicalLiveIP(socket.ID.Source, family)
	remoteIP := canonicalLiveIP(socket.ID.Destination, family)
	if localIP == "" || remoteIP == "" {
		return liveConnection{}, false
	}
	localRaw := net.JoinHostPort(localIP, strconv.Itoa(int(socket.ID.SourcePort)))
	remoteRaw := net.JoinHostPort(remoteIP, strconv.Itoa(int(socket.ID.DestinationPort)))
	state, stateGroup := liveTCPState(socket.State)
	connection := liveConnection{
		Family:         liveFamily(family),
		State:          state,
		StateGroup:     stateGroup,
		Local:          enrichAnalyticsEndpoint(localRaw, aliases),
		Remote:         enrichAnalyticsEndpoint(remoteRaw, aliases),
		InterfaceIndex: socket.ID.Interface,
		UID:            socket.UID,
		Inode:          socket.INode,
		Queues:         liveQueues{Read: socket.RQueue, Write: socket.WQueue},
	}
	if socket.ID.Interface > 0 {
		if name, err := interfaceName(int(socket.ID.Interface)); err == nil {
			connection.InterfaceName = name
		}
	}
	connection.ID = liveConnectionID(connection.Family, localIP, socket.ID.SourcePort, remoteIP, socket.ID.DestinationPort, socket.ID.Interface, socket.UID, socket.INode, socket.ID.Cookie)
	connection.TCPInfo = normalizeLiveTCPMetrics(response.TCPInfo)
	connection.Correlation = correlateLiveConnection(connection, aliases, streams, descriptions, streamsAvailable)
	return connection, true
}

func canonicalLiveIP(value net.IP, family uint8) string {
	if family == syscall.AF_INET {
		if ip := value.To4(); ip != nil {
			return ip.String()
		}
		return ""
	}
	if family == syscall.AF_INET6 {
		if ip := value.To4(); ip != nil {
			return ip.String()
		}
		if ip := value.To16(); ip != nil {
			return ip.String()
		}
	}
	return ""
}

func liveFamily(family uint8) string {
	if family == syscall.AF_INET6 {
		return "tcp6"
	}
	return "tcp4"
}

func liveTCPState(state uint8) (string, string) {
	switch state {
	case netlink.TCP_ESTABLISHED:
		return "established", "active"
	case netlink.TCP_SYN_SENT:
		return "syn_sent", "handshake"
	case netlink.TCP_SYN_RECV:
		return "syn_received", "handshake"
	case netlink.TCP_NEW_SYN_REC:
		return "new_syn_received", "handshake"
	case netlink.TCP_LISTEN:
		return "listen", "listening"
	case netlink.TCP_FIN_WAIT1:
		return "fin_wait_1", "closing"
	case netlink.TCP_FIN_WAIT2:
		return "fin_wait_2", "closing"
	case netlink.TCP_TIME_WAIT:
		return "time_wait", "closing"
	case netlink.TCP_CLOSE:
		return "close", "closing"
	case netlink.TCP_CLOSE_WAIT:
		return "close_wait", "closing"
	case netlink.TCP_LAST_ACK:
		return "last_ack", "closing"
	case netlink.TCP_CLOSING:
		return "closing", "closing"
	default:
		return "unknown", "other"
	}
}

func normalizeLiveTCPMetrics(info *netlink.TCPInfo) *liveTCPMetrics {
	if info == nil {
		return nil
	}
	return &liveTCPMetrics{
		RTTMilliseconds:              float64(info.Rtt) / 1000,
		RTTVarianceMilliseconds:      float64(info.Rttvar) / 1000,
		RTOMilliseconds:              float64(info.Rto) / 1000,
		MinRTTMilliseconds:           float64(info.Min_rtt) / 1000,
		BytesSent:                    info.Bytes_sent,
		BytesAcked:                   info.Bytes_acked,
		BytesReceived:                info.Bytes_received,
		BytesRetransmitted:           info.Bytes_retrans,
		SegmentsIn:                   info.Segs_in,
		SegmentsOut:                  info.Segs_out,
		Retransmissions:              uint32(info.Retransmits),
		TotalRetransmissions:         info.Total_retrans,
		Unacked:                      info.Unacked,
		Lost:                         info.Lost,
		CongestionWindow:             info.Snd_cwnd,
		ReceiveWindow:                info.Rcv_space,
		SendWindow:                   info.Snd_wnd,
		DeliveryRateBytesPerSecond:   info.Delivery_rate,
		PacingRateBytesPerSecond:     info.Pacing_rate,
		LastDataSentMilliseconds:     info.Last_data_sent,
		LastDataReceivedMilliseconds: info.Last_data_recv,
	}
}

func liveConnectionID(family, localIP string, localPort uint16, remoteIP string, remotePort uint16, interfaceIndex, uid, inode uint32, cookie [2]uint32) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(family + "\x00" + localIP + "\x00" + remoteIP + "\x00"))
	values := []uint32{uint32(localPort), uint32(remotePort), interfaceIndex, uid, inode, cookie[0], cookie[1]}
	for _, value := range values {
		var encoded [4]byte
		binary.BigEndian.PutUint32(encoded[:], value)
		_, _ = hash.Write(encoded[:])
	}
	return "conn_" + hex.EncodeToString(hash.Sum(nil)[:8])
}

type liveStreamIndex struct {
	incomingByPort   map[int][]npm.Stream
	forwardingByPort map[int][]npm.Stream
}

func newLiveStreamIndex(streams []npm.Stream) liveStreamIndex {
	index := liveStreamIndex{
		incomingByPort:   make(map[int][]npm.Stream),
		forwardingByPort: make(map[int][]npm.Stream),
	}
	for _, stream := range streams {
		if !stream.Tcp_forwarding {
			continue
		}
		index.incomingByPort[stream.Incoming_port] = append(index.incomingByPort[stream.Incoming_port], stream)
		index.forwardingByPort[stream.Forwarding_port] = append(index.forwardingByPort[stream.Forwarding_port], stream)
	}
	for _, candidates := range index.incomingByPort {
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	}
	for _, candidates := range index.forwardingByPort {
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	}
	return index
}

func newLiveInterfaceNameCache(resolve func(int) (string, error)) func(int) (string, error) {
	type result struct {
		name string
		err  error
	}
	results := make(map[int]result)
	return func(index int) (string, error) {
		if cached, ok := results[index]; ok {
			return cached.name, cached.err
		}
		name, err := resolve(index)
		results[index] = result{name: name, err: err}
		return name, err
	}
}

func liveOwnerDigest(authToken string) [sha256.Size]byte {
	return sha256.Sum256([]byte(authToken))
}

func correlateLiveConnection(connection liveConnection, aliases map[string][]string, streams liveStreamIndex, descriptions map[int]string, available bool) liveCorrelation {
	correlation := liveCorrelation{Status: "unmatched", Streams: make([]analyticsStream, 0)}
	if !available {
		correlation.Status = "unavailable"
		return correlation
	}
	localPort, _ := strconv.Atoi(connection.Local.Port)
	remotePort, _ := strconv.Atoi(connection.Remote.Port)
	type candidate struct {
		stream     npm.Stream
		role       string
		confidence string
	}
	inbound := make([]candidate, 0)
	exactOutbound := make([]candidate, 0)
	portOnlyOutbound := make([]candidate, 0)
	for _, stream := range streams.incomingByPort[localPort] {
		inbound = append(inbound, candidate{stream: stream, role: "inbound_listener", confidence: "listener_port"})
	}
	for _, stream := range streams.forwardingByPort[remotePort] {
		switch analyticsStreamHostEvidence(stream.Forwarding_host, connection.Remote, aliases) {
		case analyticsEvidenceMatch:
			exactOutbound = append(exactOutbound, candidate{stream: stream, role: "outbound_upstream", confidence: "exact_current_config"})
		case analyticsEvidenceIndeterminate:
			portOnlyOutbound = append(portOnlyOutbound, candidate{stream: stream, role: "outbound_upstream", confidence: "port_only"})
		}
	}
	candidates := inbound
	if len(exactOutbound) > 0 {
		candidates = exactOutbound
	} else if len(inbound) == 0 {
		candidates = portOnlyOutbound
	}
	if len(candidates) == 0 {
		return correlation
	}
	correlation.Role = candidates[0].role
	correlation.Confidence = candidates[0].confidence
	if correlation.Confidence == "port_only" {
		correlation.Status = "indeterminate"
	} else if len(candidates) == 1 {
		correlation.Status = "matched"
	} else {
		correlation.Status = "ambiguous"
	}
	if len(candidates) > liveMaxStreamCandidates {
		candidates = candidates[:liveMaxStreamCandidates]
	}
	for _, candidate := range candidates {
		correlation.Streams = append(correlation.Streams, buildAnalyticsStream(candidate.stream, descriptions[candidate.stream.ID]))
	}
	return correlation
}

func parseLiveFilters(r *http.Request) (liveFilters, error) {
	query := r.URL.Query()
	filters := liveFilters{
		state:  strings.ToLower(strings.TrimSpace(query.Get("state"))),
		search: strings.TrimSpace(query.Get("search")),
		token:  strings.TrimSpace(query.Get("snapshot")),
		limit:  liveDefaultLimit,
	}
	if alternateToken := strings.TrimSpace(query.Get("snapshot_token")); alternateToken != "" {
		if filters.token != "" && filters.token != alternateToken {
			return liveFilters{}, errors.New("snapshot and snapshot_token must identify the same snapshot")
		}
		filters.token = alternateToken
	}
	if filters.state == "" {
		filters.state = "established"
	}
	switch filters.state {
	case "established", "listen", "handshake", "closing", "all":
	default:
		return liveFilters{}, errors.New("state must be established, listen, handshake, closing, or all")
	}
	var err error
	filters.localIP, err = parseLiveIPFilter(query.Get("local_ip"), "local_ip")
	if err != nil {
		return liveFilters{}, err
	}
	filters.remoteIP, err = parseLiveIPFilter(query.Get("remote_ip"), "remote_ip")
	if err != nil {
		return liveFilters{}, err
	}
	filters.localPort, err = parseLivePortFilter(query.Get("local_port"), "local_port")
	if err != nil {
		return liveFilters{}, err
	}
	filters.remotePort, err = parseLivePortFilter(query.Get("remote_port"), "remote_port")
	if err != nil {
		return liveFilters{}, err
	}
	if value := strings.TrimSpace(query.Get("npm_only")); value != "" {
		switch strings.ToLower(value) {
		case "true":
			filters.npmOnly = true
		case "false":
		default:
			return liveFilters{}, errors.New("npm_only must be true or false")
		}
	}
	filters.offset, err = parseLiveNonnegative(query.Get("offset"), "offset", 0)
	if err != nil {
		return liveFilters{}, err
	}
	filters.limit, err = parseLiveLimit(query.Get("limit"))
	if err != nil {
		return liveFilters{}, err
	}
	if len(filters.token) > 128 || strings.ContainsAny(filters.token, " \t\r\n") {
		return liveFilters{}, errors.New("snapshot must be a valid token")
	}
	return filters, nil
}

func parseLiveIPFilter(value, name string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return "", errors.New(name + " must be a valid IP address")
	}
	return ip.String(), nil
}

func parseLivePortFilter(value, name string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, errors.New(name + " must be an integer from 1 to 65535")
	}
	return port, nil
}

func parseLiveNonnegative(value, name string, defaultValue int) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, errors.New(name + " must be a nonnegative integer")
	}
	return parsed, nil
}

func parseLiveLimit(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return liveDefaultLimit, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > liveMaxLimit {
		return 0, errors.New("limit must be an integer from 1 to 500")
	}
	return limit, nil
}

func filterLiveConnections(connections []liveConnection, filters liveFilters) []liveConnection {
	filtered := make([]liveConnection, 0, len(connections))
	for _, connection := range connections {
		if filters.state != "all" {
			if filters.state == "established" && connection.State != "established" {
				continue
			}
			if filters.state == "listen" && connection.State != "listen" {
				continue
			}
			if (filters.state == "handshake" || filters.state == "closing") && connection.StateGroup != filters.state {
				continue
			}
		}
		if filters.localIP != "" && connection.Local.IP != filters.localIP {
			continue
		}
		if filters.remoteIP != "" && connection.Remote.IP != filters.remoteIP {
			continue
		}
		if filters.localPort != 0 && connection.Local.Port != strconv.Itoa(filters.localPort) {
			continue
		}
		if filters.remotePort != 0 && connection.Remote.Port != strconv.Itoa(filters.remotePort) {
			continue
		}
		if filters.search != "" && !liveConnectionMatches(connection, filters.search) {
			continue
		}
		if filters.npmOnly && len(connection.Correlation.Streams) == 0 {
			continue
		}
		filtered = append(filtered, connection)
	}
	return filtered
}

func liveConnectionMatches(connection liveConnection, search string) bool {
	if analyticsEndpointMatches(connection.Local, search) || analyticsEndpointMatches(connection.Remote, search) {
		return true
	}
	return analyticsContains(connection.State, search) || analyticsContains(connection.StateGroup, search) || analyticsContains(connection.InterfaceName, search)
}

func sortLiveConnections(connections []liveConnection) {
	sort.Slice(connections, func(i, j int) bool {
		left, right := connections[i], connections[j]
		if left.Family != right.Family {
			return left.Family < right.Family
		}
		if compared := compareLiveIP(left.Local.IP, right.Local.IP); compared != 0 {
			return compared < 0
		}
		leftPort, _ := strconv.Atoi(left.Local.Port)
		rightPort, _ := strconv.Atoi(right.Local.Port)
		if leftPort != rightPort {
			return leftPort < rightPort
		}
		if compared := compareLiveIP(left.Remote.IP, right.Remote.IP); compared != 0 {
			return compared < 0
		}
		leftPort, _ = strconv.Atoi(left.Remote.Port)
		rightPort, _ = strconv.Atoi(right.Remote.Port)
		if leftPort != rightPort {
			return leftPort < rightPort
		}
		if left.State != right.State {
			return left.State < right.State
		}
		if left.Inode != right.Inode {
			return left.Inode < right.Inode
		}
		return left.ID < right.ID
	})
}

func compareLiveIP(left, right string) int {
	leftIP := net.ParseIP(left)
	rightIP := net.ParseIP(right)
	if leftIP == nil || rightIP == nil {
		return strings.Compare(left, right)
	}
	return bytes.Compare(leftIP.To16(), rightIP.To16())
}

func summarizeLiveConnections(connections []liveConnection) liveSummary {
	summary := liveSummary{Total: len(connections), StateGroups: make(map[string]int)}
	localIPs := make(map[string]struct{})
	remoteIPs := make(map[string]struct{})
	localPorts := make(map[string]struct{})
	remotePorts := make(map[string]struct{})
	for _, connection := range connections {
		summary.StateGroups[connection.StateGroup]++
		localIPs[connection.Local.IP] = struct{}{}
		remoteIPs[connection.Remote.IP] = struct{}{}
		localPorts[connection.Local.Port] = struct{}{}
		remotePorts[connection.Remote.Port] = struct{}{}
		summary.TotalReadQueue += uint64(connection.Queues.Read)
		summary.TotalWriteQueue += uint64(connection.Queues.Write)
		if connection.TCPInfo != nil {
			summary.BytesSent += connection.TCPInfo.BytesSent
			summary.BytesAcked += connection.TCPInfo.BytesAcked
			summary.BytesReceived += connection.TCPInfo.BytesReceived
			summary.BytesRetransmitted += connection.TCPInfo.BytesRetransmitted
			summary.SegmentsIn += uint64(connection.TCPInfo.SegmentsIn)
			summary.SegmentsOut += uint64(connection.TCPInfo.SegmentsOut)
			summary.Retransmissions += uint64(connection.TCPInfo.Retransmissions)
			summary.TotalRetransmissions += uint64(connection.TCPInfo.TotalRetransmissions)
		}
	}
	summary.UniqueLocalIPs = len(localIPs)
	summary.UniqueRemoteIPs = len(remoteIPs)
	summary.UniqueLocalPorts = len(localPorts)
	summary.UniqueRemotePorts = len(remotePorts)
	return summary
}

func newLiveSnapshotToken() (string, error) {
	var value [18]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}
