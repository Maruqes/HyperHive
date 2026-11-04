package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"512SvMan/db"
	"512SvMan/dnsmasq"
	"512SvMan/info"
	"512SvMan/k8s"
	"512SvMan/npm"
	"512SvMan/protocol"
	"512SvMan/wireguard"

	infoGrpc "github.com/Maruqes/512SvMan/api/proto/info"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"github.com/Maruqes/512SvMan/logger"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const (
	networkGraphSourceTimeout = 8 * time.Second
	networkGraphLookupTimeout = 2 * time.Second
	// A live WireGuard peer re-handshakes about every two minutes.
	wireguardLiveHandshake = 3 * time.Minute

	networkStatusUp       = "up"
	networkStatusDown     = "down"
	networkStatusDisabled = "disabled"
	networkStatusUnknown  = "unknown"

	networkMatchAlias    = "dns_alias"
	networkMatchHostname = "hostname"
	networkMatchIP       = "ip"
	networkMatchLoopback = "loopback"
	networkMatchDomain   = "domain"
	networkMatchUpstream = "upstream"
	networkMatchNone     = "none"
)

// NetworkGraph is built on demand and never stored. New sources add node types, not fields.
type NetworkGraph struct {
	GeneratedAt time.Time     `json:"generatedAt"`
	Nodes       []NetworkNode `json:"nodes"`
	Edges       []NetworkEdge `json:"edges"`
	Flows       []NetworkFlow `json:"flows"`
	Warnings    []string      `json:"warnings"`
}

type NetworkNode struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Label   string         `json:"label"`
	Host    string         `json:"host"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details"`
}

// NetworkEdge is a configured route, not proof that traffic takes it.
type NetworkEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
	Match  string `json:"match"`
}

// NetworkFlow lists the ip:port sockets that carry an edge, keying it to NetworkTraffic.Endpoints.
type NetworkFlow struct {
	Source    string   `json:"source"`
	Target    string   `json:"target"`
	Endpoints []string `json:"endpoints"`
}

type NetworkGraphHost struct {
	Name    string
	Addr    string
	Online  bool
	Master  bool
	Network *infoGrpc.NetworkSummary
}

// NetworkGraphInputs holds everything the pure builder reads.
type NetworkGraphInputs struct {
	Now           time.Time
	Hosts         []NetworkGraphHost
	VMs           []VmType
	Proxies       []npm.Proxy
	Redirections  []npm.Redirection
	Streams       []npm.Stream
	Upstreams     []npm.Upstream
	Aliases       []dnsmasq.AliasEntry
	Peers         []db.WireguardPeer
	LivePeers     []wgtypes.Peer
	ResolvedHosts map[string][]string
	Warnings      []string
}

type NetworkGraphService struct{}

type networkGraphVMs struct {
	list     []VmType
	warnings []string
}

// Build collects every source in parallel; a failed source only adds a warning.
func (s *NetworkGraphService) Build(ctx context.Context, npmBase, token string) NetworkGraph {
	in := NetworkGraphInputs{Now: time.Now().UTC(), ResolvedHosts: map[string][]string{}}
	var mu sync.Mutex
	warn := func(text string) {
		mu.Lock()
		defer mu.Unlock()
		in.Warnings = append(in.Warnings, text)
	}

	connections := protocol.GetConnectionsSnapshot()
	hosts := make([]NetworkGraphHost, len(connections))
	var wg sync.WaitGroup
	for i, conn := range connections {
		hosts[i] = NetworkGraphHost{Name: conn.MachineName, Addr: conn.Addr, Online: protocol.IsConnectionHealthy(conn.Connection)}
		wg.Add(1)
		go func() {
			defer wg.Done()
			hosts[i].Network = collectNetworkSource(ctx, "host "+conn.MachineName+" network", warn, func(ctx context.Context) (*infoGrpc.NetworkSummary, error) {
				return info.GetNetworkSummaryCtx(ctx, conn.Connection, &infoGrpc.Empty{})
			})
			hosts[i].Master = collectNetworkSource(ctx, "host "+conn.MachineName+" role", warn, func(ctx context.Context) (bool, error) {
				return k8s.IsMasterSlaveCtx(ctx, conn.Connection)
			})
		}()
	}
	run := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}
	var vms networkGraphVMs
	run(func() {
		vms = collectNetworkSource(ctx, "virtual machines", warn, func(ctx context.Context) (networkGraphVMs, error) {
			// Slaves time out first so the ones that answered are kept.
			ctx, cancel := context.WithTimeout(ctx, networkGraphSourceTimeout-time.Second)
			defer cancel()
			list, warnings, err := (&VirshService{}).GetAllVms(ctx)
			return networkGraphVMs{list, warnings}, err
		})
	})
	run(func() {
		in.Proxies = collectNetworkSource(ctx, "proxies", warn, func(context.Context) ([]npm.Proxy, error) { return npm.GetAllProxys(npmBase, token) })
	})
	run(func() {
		in.Redirections = collectNetworkSource(ctx, "redirections", warn, func(context.Context) ([]npm.Redirection, error) { return npm.ListRedirections(npmBase, token) })
	})
	run(func() {
		in.Streams = collectNetworkSource(ctx, "streams", warn, func(context.Context) ([]npm.Stream, error) { return npm.ListStreams(npmBase, token) })
	})
	run(func() { in.Upstreams = collectNetworkSource(ctx, "upstream pools", warn, npm.ListUpstreams) })
	run(func() {
		in.Aliases = collectNetworkSource(ctx, "DNS aliases", warn, func(context.Context) ([]dnsmasq.AliasEntry, error) { return dnsmasq.GetAllAliases() })
	})
	run(func() { in.Peers = collectNetworkSource(ctx, "WireGuard peers", warn, db.GetAllWireguardPeers) })
	var livePeersErr error
	run(func() {
		// Checked after the wait: a missing device is only a problem when peers exist.
		in.LivePeers, livePeersErr = runNetworkSource(ctx, func(context.Context) ([]wgtypes.Peer, error) { return wireguard.GetPeers() })
	})
	var offline []string
	run(func() { offline = collectNetworkSource(ctx, "offline hosts", warn, db.GetAllMachineNamesWithShares) })
	wg.Wait()

	in.VMs = vms.list
	in.Warnings = append(in.Warnings, vms.warnings...)

	if livePeersErr != nil && len(in.Peers) > 0 {
		logger.Warnf("network graph: WireGuard status: %v", livePeersErr)
		warn("WireGuard live status unavailable")
	}
	in.Hosts = append(hosts, offlineNetworkHosts(offline, hosts)...)
	in.ResolvedHosts = lookupNetworkHostnames(ctx, networkGraphHostnames(in))
	return BuildNetworkGraph(in)
}

// shortcut: dead slaves leave the connection list and no registry exists, so only
// machines with NFS shares can appear offline; upgrade if a host registry is added.
func offlineNetworkHosts(machines []string, connected []NetworkGraphHost) []NetworkGraphHost {
	known := make(map[string]bool, len(connected))
	for _, host := range connected {
		known[host.Name] = true
	}
	var out []NetworkGraphHost
	for _, name := range machines {
		if !known[name] {
			out = append(out, NetworkGraphHost{Name: name})
		}
	}
	return out
}

// runNetworkSource bounds sources that cannot take a context (NPM waits on a shared lock),
// and returns any partial value fn produced alongside its error.
func runNetworkSource[T any](ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, networkGraphSourceTimeout)
	defer cancel()
	type result struct {
		value T
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := fn(ctx)
		done <- result{value, err}
	}()
	select {
	case out := <-done:
		return out.value, out.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// collectNetworkSource keeps raw errors in the log: they can quote upstream response bodies.
func collectNetworkSource[T any](ctx context.Context, name string, warn func(string), fn func(context.Context) (T, error)) T {
	value, err := runNetworkSource(ctx, fn)
	if err == nil {
		return value
	}
	logger.Warnf("network graph: %s: %v", name, err)
	if errors.Is(err, context.DeadlineExceeded) {
		warn(name + " timed out")
	} else {
		warn(name + " unavailable")
	}
	return value
}

func lookupNetworkHostnames(ctx context.Context, names []string) map[string][]string {
	out := make(map[string][]string, len(names))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(ctx, networkGraphLookupTimeout)
			defer cancel()
			// A name that does not resolve is an unknown target, not a source failure.
			if addrs, err := net.DefaultResolver.LookupHost(ctx, name); err == nil {
				mu.Lock()
				out[name] = addrs
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}

// networkGraphHostnames lists targets that need a real lookup.
func networkGraphHostnames(in NetworkGraphInputs) []string {
	aliases := networkAliasIndex(in.Aliases)
	seen := map[string]bool{}
	var out []string
	add := func(raw string) {
		host := normalizeNetworkHost(raw)
		if host == "" || seen[host] || aliases[host] != nil || isLoopbackNetworkHost(host) {
			return
		}
		seen[host] = true
		if _, err := netip.ParseAddr(host); err != nil {
			out = append(out, host)
		}
	}
	for _, proxy := range in.Proxies {
		add(proxy.ForwardHost)
		for _, location := range proxy.Locations {
			add(location.ForwardHost)
		}
	}
	for _, stream := range in.Streams {
		add(stream.Forwarding_host)
	}
	for _, redirection := range in.Redirections {
		add(redirection.Forward_domain)
	}
	for _, pool := range in.Upstreams {
		for _, member := range pool.Members {
			add(member.IP)
		}
	}
	sort.Strings(out)
	return out
}

func normalizeNetworkHost(raw string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(raw), "[]"))
}

func isLoopbackNetworkHost(host string) bool {
	if host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
}

// networkIP reads the forms hosts report: a bare address, CIDR or host:port.
func networkIP(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if prefix, err := netip.ParsePrefix(raw); err == nil {
		return prefix.Addr().Unmap().String(), true
	}
	if addr, err := netip.ParseAddr(raw); err == nil {
		return addr.Unmap().String(), true
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		if addr, err := netip.ParseAddr(host); err == nil {
			return addr.Unmap().String(), true
		}
	}
	return "", false
}

// networkAliasIndex splits multi-domain aliases, stored as a/b in address=/a/b/ip.
func networkAliasIndex(aliases []dnsmasq.AliasEntry) map[string][]dnsmasq.AliasEntry {
	out := map[string][]dnsmasq.AliasEntry{}
	for _, entry := range aliases {
		for _, name := range strings.Split(entry.Alias, "/") {
			if name = normalizeNetworkHost(name); name != "" {
				out[name] = append(out[name], entry)
			}
		}
	}
	return out
}

type networkGraphBuilder struct {
	in         NetworkGraphInputs
	nodes      map[string]*NetworkNode
	edges      map[NetworkEdge]bool
	warnings   []string
	ipNodes    map[string][]string
	vmsByName  map[string][]string
	aliases    map[string][]dnsmasq.AliasEntry
	masterName string
	masterID   string
	flows      map[[2]string]map[string]bool
	pools      map[string][]string
}

// BuildNetworkGraph is pure: all I/O happens in the collectors.
func BuildNetworkGraph(in NetworkGraphInputs) NetworkGraph {
	b := &networkGraphBuilder{
		in:        in,
		nodes:     map[string]*NetworkNode{},
		edges:     map[NetworkEdge]bool{},
		flows:     map[[2]string]map[string]bool{},
		pools:     map[string][]string{},
		warnings:  append([]string{}, in.Warnings...),
		ipNodes:   map[string][]string{},
		vmsByName: map[string][]string{},
		aliases:   networkAliasIndex(in.Aliases),
	}
	b.addHosts()
	b.addVMs()
	b.addWireguard()
	b.addDomains()
	b.addUpstreams()
	b.addProxies()
	b.addStreams()
	b.addRedirections()
	return b.graph()
}

func (b *networkGraphBuilder) graph() NetworkGraph {
	out := NetworkGraph{GeneratedAt: b.in.Now, Nodes: make([]NetworkNode, 0, len(b.nodes)), Edges: make([]NetworkEdge, 0, len(b.edges)), Flows: make([]NetworkFlow, 0, len(b.flows)), Warnings: b.warnings}
	for _, node := range b.nodes {
		out.Nodes = append(out.Nodes, *node)
	}
	for edge := range b.edges {
		out.Edges = append(out.Edges, edge)
	}
	// Stable order keeps the client layout from reshuffling between requests.
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].ID < out.Nodes[j].ID })
	sort.Slice(out.Edges, func(i, j int) bool {
		a, c := out.Edges[i], out.Edges[j]
		if a.Source != c.Source {
			return a.Source < c.Source
		}
		if a.Target != c.Target {
			return a.Target < c.Target
		}
		return a.Type < c.Type
	})
	for key, endpoints := range b.flows {
		flow := NetworkFlow{Source: key[0], Target: key[1], Endpoints: make([]string, 0, len(endpoints))}
		for endpoint := range endpoints {
			flow.Endpoints = append(flow.Endpoints, endpoint)
		}
		sort.Strings(flow.Endpoints)
		out.Flows = append(out.Flows, flow)
	}
	sort.Slice(out.Flows, func(i, j int) bool {
		a, c := out.Flows[i], out.Flows[j]
		if a.Source != c.Source {
			return a.Source < c.Source
		}
		return a.Target < c.Target
	})
	sort.Strings(out.Warnings)
	return out
}

func (b *networkGraphBuilder) node(id, nodeType, label, host, status string, details map[string]any) *NetworkNode {
	if details == nil {
		details = map[string]any{}
	}
	node := &NetworkNode{ID: id, Type: nodeType, Label: label, Host: host, Status: status, Details: details}
	b.nodes[id] = node
	return node
}

func (b *networkGraphBuilder) edge(source, target, edgeType, match string) {
	b.edges[NetworkEdge{Source: source, Target: target, Type: edgeType, Match: match}] = true
}

func (b *networkGraphBuilder) flow(source, target string, endpoints []string) {
	key := [2]string{source, target}
	for _, endpoint := range endpoints {
		if b.flows[key] == nil {
			b.flows[key] = map[string]bool{}
		}
		b.flows[key][endpoint] = true
	}
}

// endpoints lists the ip:port forms a socket to this target can have on the NPM host.
func (b *networkGraphBuilder) endpoints(raw string, port int) []string {
	host := normalizeNetworkHost(raw)
	if port <= 0 || host == "" {
		return nil
	}
	var addresses []string
	switch {
	case isLoopbackNetworkHost(host):
		addresses = []string{"127.0.0.1", "::1"}
	case b.aliases[host] != nil:
		for _, entry := range b.aliases[host] {
			addresses = append(addresses, entry.IP)
		}
	case len(b.in.ResolvedHosts[host]) > 0:
		addresses = b.in.ResolvedHosts[host]
	default:
		addresses = []string{host}
	}
	var out []string
	for _, address := range addresses {
		if ip, ok := networkIP(address); ok {
			out = append(out, net.JoinHostPort(ip, strconv.Itoa(port)))
		}
	}
	return out
}

func (b *networkGraphBuilder) indexIP(raw, id string) {
	ip, ok := networkIP(raw)
	if !ok {
		return
	}
	// Link-local and loopback addresses repeat on every host and identify nothing.
	if addr, err := netip.ParseAddr(ip); err != nil || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return
	}
	for _, existing := range b.ipNodes[ip] {
		if existing == id {
			return
		}
	}
	b.ipNodes[ip] = append(b.ipNodes[ip], id)
}

func networkHostID(name string) string { return "host:" + name }

func (b *networkGraphBuilder) addHosts() {
	for _, host := range b.in.Hosts {
		id := networkHostID(host.Name)
		status := networkStatusDown
		if host.Online {
			status = networkStatusUp
		}
		details := map[string]any{"master": host.Master}
		if host.Addr != "" {
			details["address"] = host.Addr
		}
		b.node(id, "host", host.Name, host.Name, status, details)
		b.indexIP(host.Addr, id)
		if host.Master {
			b.masterName, b.masterID = host.Name, id
		}
		if host.Network == nil {
			continue
		}
		for _, iface := range host.Network.Interfaces {
			for _, addr := range iface.Addrs {
				b.indexIP(addr, id)
			}
			if !isGraphInterface(iface) {
				continue
			}
			ifaceID := networkInterfaceID(host.Name, iface.Name)
			ifaceStatus := networkStatusDown
			for _, flag := range iface.Flags {
				if strings.EqualFold(flag, "up") {
					ifaceStatus = networkStatusUp
				}
			}
			b.node(ifaceID, "interface", iface.Name, host.Name, ifaceStatus, map[string]any{"mac": iface.HardwareAddr, "mtu": iface.Mtu, "addresses": iface.Addrs})
			b.edge(id, ifaceID, "has_interface", "")
		}
	}
}

func networkInterfaceID(host, name string) string { return "interface:" + host + ":" + name }

// isGraphInterface drops loopback and per-guest or per-container links, which would
// add one node per VM or container without showing a route. Their addresses still match.
func isGraphInterface(iface *infoGrpc.NetworkInterfaceStruct) bool {
	for _, flag := range iface.Flags {
		if strings.EqualFold(flag, "loopback") {
			return false
		}
	}
	return !strings.HasPrefix(iface.Name, "veth") && !strings.HasPrefix(iface.Name, "vnet")
}

func networkVMID(host, name string) string { return "vm:" + host + ":" + name }

func (b *networkGraphBuilder) addVMs() {
	for _, vm := range b.in.VMs {
		if vm.Vm == nil {
			continue
		}
		id := networkVMID(vm.MachineName, vm.Name)
		status := networkStatusDown
		if vm.State == grpcVirsh.VmState_RUNNING {
			status = networkStatusUp
		}
		// Explicit fields only: Vm also carries the VNC password.
		b.node(id, "vm", vm.Name, vm.MachineName, status, map[string]any{"state": vm.State.String(), "addresses": vm.Ip, "network": vm.Network, "cpus": vm.DefinedCPUS, "memoryMB": vm.DefinedRam, "live": vm.IsLive})
		b.vmsByName[vm.Name] = append(b.vmsByName[vm.Name], id)
		for _, ip := range vm.Ip {
			b.indexIP(ip, id)
		}
		if vm.Network == "" {
			b.edge(networkHostID(vm.MachineName), id, "runs", "")
			continue
		}
		// shortcut: Vm.network is the first NIC only, upgrade with the M5 interface stats.
		bridgeID := "bridge:" + vm.MachineName + ":" + vm.Network
		if b.nodes[bridgeID] == nil {
			b.node(bridgeID, "bridge", vm.Network, vm.MachineName, networkStatusUnknown, nil)
			// The network name only sometimes equals a host interface name.
			if ifaceID := networkInterfaceID(vm.MachineName, vm.Network); b.nodes[ifaceID] != nil {
				b.edge(ifaceID, bridgeID, "bridges", "")
			} else {
				b.edge(networkHostID(vm.MachineName), bridgeID, "has_bridge", "")
			}
		}
		b.edge(bridgeID, id, "attached", "")
	}
}

func networkPeerID(id int) string { return fmt.Sprintf("wireguard:peer:%d", id) }

func (b *networkGraphBuilder) addWireguard() {
	if len(b.in.Peers) == 0 {
		return
	}
	const serverID = "wireguard:master"
	b.node(serverID, "wireguard", "WireGuard VPN", b.masterName, networkStatusUnknown, nil)
	if b.masterID != "" {
		b.edge(b.masterID, serverID, "vpn", "")
	}
	for _, peer := range b.in.Peers {
		id := networkPeerID(peer.Id)
		details := map[string]any{"address": peer.ClientIP}
		status := networkStatusUnknown
		if live, ok := liveNetworkPeer(peer.ClientIP, b.in.LivePeers); ok {
			details["bytesReceived"], details["bytesSent"] = live.ReceiveBytes, live.TransmitBytes
			if !live.LastHandshakeTime.IsZero() {
				details["lastHandshake"] = live.LastHandshakeTime.UTC()
			}
			if b.in.Now.Sub(live.LastHandshakeTime) < wireguardLiveHandshake {
				status = networkStatusUp
			}
		}
		b.node(id, "wireguard_peer", peer.Name, b.masterName, status, details)
		b.indexIP(peer.ClientIP, id)
		b.edge(serverID, id, "vpn", "")
	}
}

func liveNetworkPeer(clientCIDR string, peers []wgtypes.Peer) (wgtypes.Peer, bool) {
	for _, peer := range peers {
		for _, allowed := range peer.AllowedIPs {
			if allowed.String() == clientCIDR {
				return peer, true
			}
		}
	}
	return wgtypes.Peer{}, false
}

func networkDomainID(name string) string { return "domain:" + normalizeNetworkHost(name) }

func (b *networkGraphBuilder) domain(name string) string {
	id := networkDomainID(name)
	if b.nodes[id] == nil {
		b.node(id, "domain", normalizeNetworkHost(name), "", networkStatusUnknown, nil)
	}
	return id
}

func (b *networkGraphBuilder) addDomains() {
	for name, entries := range b.aliases {
		id := b.domain(name)
		addresses := make([]string, 0, len(entries))
		for _, entry := range entries {
			addresses = append(addresses, entry.IP)
		}
		b.nodes[id].Details["aliasAddresses"] = addresses
		target, match := b.resolve(name)
		b.edge(id, target, "resolves", match)
	}
}

// resolve follows the locked match order: DNS alias, hostname lookup, then IP.
func (b *networkGraphBuilder) resolve(raw string) (string, string) {
	host := normalizeNetworkHost(raw)
	if isLoopbackNetworkHost(host) {
		// NPM runs with host networking, so loopback is the master host.
		if b.masterID != "" {
			return b.masterID, networkMatchLoopback
		}
		return b.unknown(host, nil), networkMatchNone
	}
	if entries := b.aliases[host]; entries != nil {
		var ids, addresses []string
		for _, entry := range entries {
			addresses = append(addresses, entry.IP)
			ids = append(ids, b.nodesForIP(entry.IP)...)
		}
		if len(ids) == 0 {
			// A stopped VM has no address, but the alias may still name it.
			for _, entry := range entries {
				ids = append(ids, b.vmsByName[entry.RelatedVM]...)
			}
		}
		return b.pick(host, ids, addresses, networkMatchAlias)
	}
	if addresses := b.in.ResolvedHosts[host]; len(addresses) > 0 {
		var ids []string
		for _, address := range addresses {
			ids = append(ids, b.nodesForIP(address)...)
		}
		return b.pick(host, ids, addresses, networkMatchHostname)
	}
	return b.pick(host, b.nodesForIP(host), nil, networkMatchIP)
}

func (b *networkGraphBuilder) nodesForIP(raw string) []string {
	ip, ok := networkIP(raw)
	if !ok {
		return nil
	}
	return b.ipNodes[ip]
}

// pick refuses to guess: shared addresses such as a default NAT gateway exist on many hosts.
func (b *networkGraphBuilder) pick(host string, ids, addresses []string, match string) (string, string) {
	unique := map[string]bool{}
	for _, id := range ids {
		unique[id] = true
	}
	switch len(unique) {
	case 0:
		return b.unknown(host, addresses), networkMatchNone
	case 1:
		for id := range unique {
			return id, match
		}
	}
	matches := make([]string, 0, len(unique))
	for id := range unique {
		matches = append(matches, id)
	}
	sort.Strings(matches)
	b.addWarning(fmt.Sprintf("target %s matches several nodes: %s", host, strings.Join(matches, ", ")))
	return b.unknown(host, addresses), networkMatchNone
}

func (b *networkGraphBuilder) addWarning(text string) {
	for _, existing := range b.warnings {
		if existing == text {
			return
		}
	}
	b.warnings = append(b.warnings, text)
}

func (b *networkGraphBuilder) unknown(host string, addresses []string) string {
	id := "unknown:" + host
	if b.nodes[id] == nil {
		details := map[string]any{}
		if len(addresses) > 0 {
			details["addresses"] = addresses
		}
		b.node(id, "unknown", host, "", networkStatusUnknown, details)
	}
	return id
}

func networkUpstreamID(id int64) string { return fmt.Sprintf("upstream:%d", id) }

func (b *networkGraphBuilder) addUpstreams() {
	for _, pool := range b.in.Upstreams {
		id := networkUpstreamID(pool.ID)
		b.node(id, "upstream", pool.Name, b.masterName, networkStatusUnknown, map[string]any{"context": pool.Context, "method": pool.Method, "applyStatus": pool.ApplyStatus})
		for _, member := range pool.Members {
			target, match := b.resolve(member.IP)
			b.edge(id, target, "forwards", match)
			endpoints := b.endpoints(member.IP, member.Port)
			b.flow(id, target, endpoints)
			b.pools[id] = append(b.pools[id], endpoints...)
		}
	}
}

// boundNetworkPool reads upstream_id, which NPM leaves unset for direct targets.
func boundNetworkPool(raw json.RawMessage) (int64, bool) {
	var id int64
	if json.Unmarshal(raw, &id) != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// forward adds one proxy or stream target. GetAllProxys and ListStreams blank the
// host on pool-bound targets, so the pool is the only route.
func (b *networkGraphBuilder) forward(source string, pool json.RawMessage, host string, port int) {
	if id, ok := boundNetworkPool(pool); ok {
		target := networkUpstreamID(id)
		if b.nodes[target] == nil {
			// The pool source failed: keep the route visible rather than dropping it.
			b.node(target, "upstream", fmt.Sprintf("pool %d", id), b.masterName, networkStatusUnknown, nil)
		}
		b.edge(source, target, "forwards", networkMatchUpstream)
		b.flow(source, target, b.pools[target])
		return
	}
	if normalizeNetworkHost(host) == "" {
		return
	}
	target, match := b.resolve(host)
	b.edge(source, target, "forwards", match)
	b.flow(source, target, b.endpoints(host, port))
}

func networkEnabledStatus(enabled bool) string {
	if enabled {
		return networkStatusUp
	}
	return networkStatusDisabled
}

func (b *networkGraphBuilder) addProxies() {
	for _, proxy := range b.in.Proxies {
		id := fmt.Sprintf("proxy:%d", proxy.ID)
		label := fmt.Sprintf("proxy %d", proxy.ID)
		if len(proxy.DomainNames) > 0 {
			label = proxy.DomainNames[0]
		}
		// Explicit fields only: advanced_config and meta can hold credentials.
		targets := []map[string]any{{"path": "/", "scheme": proxy.ForwardScheme, "host": proxy.ForwardHost, "port": proxy.ForwardPort}}
		b.forward(id, proxy.UpstreamID, proxy.ForwardHost, proxy.ForwardPort)
		for _, location := range proxy.Locations {
			targets = append(targets, map[string]any{"path": location.Path, "scheme": location.ForwardScheme, "host": location.ForwardHost, "port": location.ForwardPort})
			b.forward(id, location.UpstreamID, location.ForwardHost, location.ForwardPort)
		}
		b.node(id, "proxy", label, b.masterName, networkEnabledStatus(proxy.Enabled), map[string]any{"domains": proxy.DomainNames, "targets": targets, "sslForced": proxy.SslForced})
		for _, name := range proxy.DomainNames {
			b.edge(b.domain(name), id, "routes", "")
		}
	}
}

func (b *networkGraphBuilder) addStreams() {
	for _, stream := range b.in.Streams {
		id := fmt.Sprintf("stream:%d", stream.ID)
		b.node(id, "stream", fmt.Sprintf(":%d", stream.Incoming_port), b.masterName, networkEnabledStatus(stream.Enabled), map[string]any{
			"description": stream.Description, "incomingPort": stream.Incoming_port, "host": stream.Forwarding_host, "port": stream.Forwarding_port, "tcp": stream.Tcp_forwarding, "udp": stream.Udp_forwarding,
		})
		b.forward(id, stream.UpstreamID, stream.Forwarding_host, stream.Forwarding_port)
	}
}

func (b *networkGraphBuilder) addRedirections() {
	// Register every source domain first so redirections can chain to each other.
	for _, redirection := range b.in.Redirections {
		for _, name := range redirection.Domain_names {
			b.domain(name)
		}
	}
	for _, redirection := range b.in.Redirections {
		id := fmt.Sprintf("redirection:%d", redirection.ID)
		b.node(id, "redirection", redirection.Forward_domain, b.masterName, networkEnabledStatus(redirection.Enabled), map[string]any{"domains": redirection.Domain_names, "scheme": redirection.Forward_scheme, "target": redirection.Forward_domain, "code": fmt.Sprint(redirection.Forward_http_code)})
		for _, name := range redirection.Domain_names {
			b.edge(networkDomainID(name), id, "routes", "")
		}
		if normalizeNetworkHost(redirection.Forward_domain) == "" {
			continue
		}
		if b.nodes[networkDomainID(redirection.Forward_domain)] != nil {
			b.edge(id, networkDomainID(redirection.Forward_domain), "redirects", networkMatchDomain)
			continue
		}
		target, match := b.resolve(redirection.Forward_domain)
		b.edge(id, target, "redirects", match)
	}
}
