package services

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"512SvMan/db"
	"512SvMan/dnsmasq"
	"512SvMan/npm"

	infoGrpc "github.com/Maruqes/512SvMan/api/proto/info"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func graphFixture() NetworkGraphInputs {
	return NetworkGraphInputs{
		Now: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC),
		Hosts: []NetworkGraphHost{
			{Name: "alpha", Addr: "10.0.0.1", Online: true, Master: true, Network: &infoGrpc.NetworkSummary{Interfaces: []*infoGrpc.NetworkInterfaceStruct{
				{Name: "lo", Flags: []string{"up", "loopback"}, Addrs: []string{"127.0.0.1/8"}},
				{Name: "br0", Flags: []string{"up"}, Addrs: []string{"192.168.1.10/24", "fe80::1/64"}},
				{Name: "vnet3", Flags: []string{"up"}},
				{Name: "virbr0", Flags: []string{"up"}, Addrs: []string{"192.168.122.1/24"}},
			}}},
			{Name: "beta", Addr: "10.0.0.2:50052", Online: true, Network: &infoGrpc.NetworkSummary{Interfaces: []*infoGrpc.NetworkInterfaceStruct{
				{Name: "virbr0", Flags: []string{"up"}, Addrs: []string{"192.168.122.1/24"}},
			}}},
		},
		VMs: []VmType{
			{Vm: &grpcVirsh.Vm{MachineName: "alpha", Name: "web", State: grpcVirsh.VmState_RUNNING, Ip: []string{"192.168.1.50"}, Network: "br0", VNCPassword: "vnc-secret"}},
			{Vm: &grpcVirsh.Vm{MachineName: "beta", Name: "db", State: grpcVirsh.VmState_SHUTOFF, Network: "nat"}},
		},
	}
}

func hasEdge(graph NetworkGraph, source, target, edgeType, match string) bool {
	for _, edge := range graph.Edges {
		if edge == (NetworkEdge{source, target, edgeType, match}) {
			return true
		}
	}
	return false
}

func graphNode(t *testing.T, graph NetworkGraph, id string) NetworkNode {
	t.Helper()
	for _, node := range graph.Nodes {
		if node.ID == id {
			return node
		}
	}
	t.Fatalf("node %s missing from %+v", id, graph.Nodes)
	return NetworkNode{}
}

func TestBuildNetworkGraphTopology(t *testing.T) {
	graph := BuildNetworkGraph(graphFixture())
	if !hasEdge(graph, "interface:alpha:br0", "bridge:alpha:br0", "bridges", "") {
		t.Fatalf("exact bridge name was not linked: %+v", graph.Edges)
	}
	if !hasEdge(graph, "host:beta", "bridge:beta:nat", "has_bridge", "") {
		t.Fatalf("network without an interface should hang off the host: %+v", graph.Edges)
	}
	if !hasEdge(graph, "bridge:alpha:br0", "vm:alpha:web", "attached", "") {
		t.Fatalf("VM not attached: %+v", graph.Edges)
	}
	for _, skipped := range []string{"interface:alpha:lo", "interface:alpha:vnet3"} {
		for _, node := range graph.Nodes {
			if node.ID == skipped {
				t.Fatalf("%s should be filtered", skipped)
			}
		}
	}
	if graphNode(t, graph, "vm:alpha:web").Status != "up" || graphNode(t, graph, "vm:beta:db").Status != "down" {
		t.Fatal("VM status not derived from state")
	}
	data, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "vnc-secret") {
		t.Fatal("VNC password leaked into the graph")
	}
}

func TestBuildNetworkGraphMatchOrder(t *testing.T) {
	in := graphFixture()
	in.Aliases = []dnsmasq.AliasEntry{
		{Alias: "web.home/www.home", IP: "192.168.1.50"},
		{Alias: "db.home", IP: "192.168.9.9", RelatedVM: "db"},
		{Alias: "both.home", IP: "192.168.1.50"},
	}
	in.ResolvedHosts = map[string][]string{"lookup.lan": {"192.168.1.50"}, "both.home": {"10.0.0.1"}}
	in.Proxies = []npm.Proxy{
		{ID: 1, DomainNames: []string{"a.example"}, ForwardHost: "WWW.home", Enabled: true},
		{ID: 2, DomainNames: []string{"b.example"}, ForwardHost: "lookup.lan", Enabled: true},
		{ID: 3, DomainNames: []string{"c.example"}, ForwardHost: "192.168.1.10", Enabled: false},
		{ID: 4, DomainNames: []string{"d.example"}, ForwardHost: "203.0.113.7", Enabled: true},
		{ID: 5, DomainNames: []string{"e.example"}, ForwardHost: "127.0.0.1", Locations: []npm.Location{{Path: "/api", ForwardHost: "192.168.1.50"}}},
		{ID: 6, DomainNames: []string{"f.example"}, ForwardHost: "db.home"},
		{ID: 7, DomainNames: []string{"g.example"}, ForwardHost: "both.home"},
	}
	graph := BuildNetworkGraph(in)

	for _, want := range []NetworkEdge{
		{"proxy:1", "vm:alpha:web", "forwards", "dns_alias"},
		{"proxy:2", "vm:alpha:web", "forwards", "hostname"},
		{"proxy:3", "host:alpha", "forwards", "ip"},
		{"proxy:4", "unknown:203.0.113.7", "forwards", "none"},
		{"proxy:5", "host:alpha", "forwards", "loopback"},
		{"proxy:5", "vm:alpha:web", "forwards", "ip"},
		{"proxy:6", "vm:beta:db", "forwards", "dns_alias"},
		{"proxy:7", "vm:alpha:web", "forwards", "dns_alias"},
		{"domain:a.example", "proxy:1", "routes", ""},
		{"domain:www.home", "vm:alpha:web", "resolves", "dns_alias"},
	} {
		if !hasEdge(graph, want.Source, want.Target, want.Type, want.Match) {
			t.Errorf("missing %+v in %+v", want, graph.Edges)
		}
	}
	if graphNode(t, graph, "proxy:3").Status != "disabled" {
		t.Error("disabled proxy status")
	}
	if len(graph.Warnings) != 0 {
		t.Errorf("unexpected warnings %v", graph.Warnings)
	}
}

func TestBuildNetworkGraphAmbiguousAddressIsNotGuessed(t *testing.T) {
	in := graphFixture()
	in.Streams = []npm.Stream{{ID: 1, Incoming_port: 2222, Forwarding_host: "192.168.122.1", Enabled: true}}
	graph := BuildNetworkGraph(in)
	if !hasEdge(graph, "stream:1", "unknown:192.168.122.1", "forwards", "none") {
		t.Fatalf("shared address must stay unknown: %+v", graph.Edges)
	}
	if len(graph.Warnings) != 1 || !strings.Contains(graph.Warnings[0], "host:alpha, host:beta") {
		t.Fatalf("warnings = %v", graph.Warnings)
	}
}

func TestBuildNetworkGraphLoopbackWithoutMasterIsUnknown(t *testing.T) {
	in := graphFixture()
	in.Hosts[0].Master = false
	in.Streams = []npm.Stream{{ID: 1, Forwarding_host: "localhost"}}
	if graph := BuildNetworkGraph(in); !hasEdge(graph, "stream:1", "unknown:localhost", "forwards", "none") {
		t.Fatalf("edges = %+v", graph.Edges)
	}
}

func TestBuildNetworkGraphUpstreamPools(t *testing.T) {
	in := graphFixture()
	in.Upstreams = []npm.Upstream{{ID: 7, Name: "web_pool", Members: []npm.UpstreamMember{{IP: "192.168.1.50", Port: 80}, {IP: "198.51.100.1", Port: 80}}}}
	in.Proxies = []npm.Proxy{
		{ID: 1, DomainNames: []string{"pool.example"}, UpstreamID: json.RawMessage("7")},
		{ID: 2, DomainNames: []string{"gone.example"}, UpstreamID: json.RawMessage("9")},
		{ID: 3, DomainNames: []string{"direct.example"}, UpstreamID: json.RawMessage("null"), ForwardHost: "192.168.1.50"},
	}
	graph := BuildNetworkGraph(in)
	for _, want := range []NetworkEdge{
		{"proxy:1", "upstream:7", "forwards", "upstream"},
		{"upstream:7", "vm:alpha:web", "forwards", "ip"},
		{"upstream:7", "unknown:198.51.100.1", "forwards", "none"},
		{"proxy:2", "upstream:9", "forwards", "upstream"},
		{"proxy:3", "vm:alpha:web", "forwards", "ip"},
	} {
		if !hasEdge(graph, want.Source, want.Target, want.Type, want.Match) {
			t.Errorf("missing %+v in %+v", want, graph.Edges)
		}
	}
}

func TestBuildNetworkGraphRedirections(t *testing.T) {
	in := graphFixture()
	in.Proxies = []npm.Proxy{{ID: 1, DomainNames: []string{"new.example"}, ForwardHost: "192.168.1.50"}}
	in.Redirections = []npm.Redirection{
		{ID: 1, Domain_names: []string{"old.example"}, Forward_domain: "NEW.example", Enabled: true},
		{ID: 2, Domain_names: []string{"away.example"}, Forward_domain: "elsewhere.test", Enabled: true},
		{ID: 3, Domain_names: []string{"blank.example"}},
	}
	graph := BuildNetworkGraph(in)
	for _, want := range []NetworkEdge{
		{"domain:old.example", "redirection:1", "routes", ""},
		{"redirection:1", "domain:new.example", "redirects", "domain"},
		{"redirection:2", "unknown:elsewhere.test", "redirects", "none"},
	} {
		if !hasEdge(graph, want.Source, want.Target, want.Type, want.Match) {
			t.Errorf("missing %+v in %+v", want, graph.Edges)
		}
	}
	for _, edge := range graph.Edges {
		if edge.Source == "redirection:3" {
			t.Errorf("blank redirect target created %+v", edge)
		}
	}
}

func TestBuildNetworkGraphWireguard(t *testing.T) {
	in := graphFixture()
	_, liveNet, _ := net.ParseCIDR("10.8.0.2/32")
	_, staleNet, _ := net.ParseCIDR("10.8.0.3/32")
	in.Peers = []db.WireguardPeer{{Id: 1, Name: "laptop", ClientIP: "10.8.0.2/32"}, {Id: 2, Name: "phone", ClientIP: "10.8.0.3/32"}, {Id: 3, Name: "new", ClientIP: "10.8.0.4/32"}}
	in.LivePeers = []wgtypes.Peer{
		{AllowedIPs: []net.IPNet{*liveNet}, LastHandshakeTime: in.Now.Add(-time.Minute), ReceiveBytes: 5, PresharedKey: wgtypes.Key{1}},
		{AllowedIPs: []net.IPNet{*staleNet}, LastHandshakeTime: in.Now.Add(-time.Hour)},
	}
	in.Streams = []npm.Stream{{ID: 1, Forwarding_host: "10.8.0.2"}}
	graph := BuildNetworkGraph(in)
	if graphNode(t, graph, "wireguard:peer:1").Status != "up" {
		t.Error("recent handshake should be up")
	}
	for _, id := range []string{"wireguard:peer:2", "wireguard:peer:3"} {
		if graphNode(t, graph, id).Status != "unknown" {
			t.Errorf("%s must stay neutral", id)
		}
	}
	if !hasEdge(graph, "host:alpha", "wireguard:master", "vpn", "") || !hasEdge(graph, "wireguard:master", "wireguard:peer:3", "vpn", "") {
		t.Errorf("VPN edges missing: %+v", graph.Edges)
	}
	if !hasEdge(graph, "stream:1", "wireguard:peer:1", "forwards", "ip") {
		t.Errorf("peer address did not match: %+v", graph.Edges)
	}
}

func TestBuildNetworkGraphEmptyAndConsistent(t *testing.T) {
	data, err := json.Marshal(BuildNetworkGraph(NetworkGraphInputs{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"nodes":[]`, `"edges":[]`, `"warnings":[]`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("%s not in %s", want, data)
		}
	}

	in := graphFixture()
	in.Hosts = append(in.Hosts, NetworkGraphHost{Name: "gamma"})
	in.Aliases = []dnsmasq.AliasEntry{{Alias: "x.home", IP: "1.2.3.4"}}
	in.Proxies = []npm.Proxy{{ID: 1, DomainNames: []string{"a.example"}, ForwardHost: "no-such-host"}}
	graph := BuildNetworkGraph(in)
	ids := map[string]bool{}
	for _, node := range graph.Nodes {
		ids[node.ID] = true
		if node.Details == nil {
			t.Errorf("%s has nil details", node.ID)
		}
	}
	for _, edge := range graph.Edges {
		if !ids[edge.Source] || !ids[edge.Target] {
			t.Errorf("dangling edge %+v", edge)
		}
	}
	if graphNode(t, graph, "host:gamma").Status != "down" {
		t.Error("offline host should be down")
	}
}

func TestNetworkGraphHostnamesSkipsKnownTargets(t *testing.T) {
	in := NetworkGraphInputs{
		Aliases:      []dnsmasq.AliasEntry{{Alias: "x.home", IP: "1.2.3.4"}},
		Proxies:      []npm.Proxy{{ForwardHost: "x.home", Locations: []npm.Location{{ForwardHost: "svc.lan"}}}, {ForwardHost: "127.0.0.1"}},
		Streams:      []npm.Stream{{Forwarding_host: "10.0.0.5"}, {Forwarding_host: "localhost"}},
		Redirections: []npm.Redirection{{Forward_domain: "SVC.lan"}},
		Upstreams:    []npm.Upstream{{Members: []npm.UpstreamMember{{IP: "pool.lan"}}}},
	}
	if got := strings.Join(networkGraphHostnames(in), ","); got != "pool.lan,svc.lan" {
		t.Fatalf("hostnames = %s", got)
	}
}

func TestOfflineNetworkHosts(t *testing.T) {
	got := offlineNetworkHosts([]string{"alpha", "gone"}, []NetworkGraphHost{{Name: "alpha"}})
	if len(got) != 1 || got[0].Name != "gone" || got[0].Online {
		t.Fatalf("offline = %+v", got)
	}
}

func TestCollectNetworkSourceFailureWarns(t *testing.T) {
	var warnings []string
	warn := func(text string) { warnings = append(warnings, text) }

	got := collectNetworkSource(context.Background(), "vms", warn, func(context.Context) ([]string, error) {
		return []string{"partial"}, errors.New("slave answered: secret-body")
	})
	if len(got) != 1 || got[0] != "partial" {
		t.Fatalf("partial value lost: %v", got)
	}
	if len(warnings) != 1 || warnings[0] != "vms unavailable" {
		t.Fatalf("raw error must stay out of warnings: %v", warnings)
	}
}

func TestCollectNetworkSourceTimesOutWithoutWaiting(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(20*time.Millisecond))
	defer cancel()
	release := make(chan struct{})
	defer close(release)
	var warnings []string
	collectNetworkSource(ctx, "proxies", func(text string) { warnings = append(warnings, text) }, func(context.Context) (int, error) {
		<-release // ignores cancellation, like the NPM lock
		return 1, nil
	})
	if len(warnings) != 1 || warnings[0] != "proxies timed out" {
		t.Fatalf("warnings = %v", warnings)
	}
}

func celldFixture() NetworkGraphInputs {
	in := graphFixture()
	ports := CelldPorts{HTTP: 18080, Internal: 18081}
	in.Fleets = []CelldFleetInfo{
		{ID: "f1", Name: "shop", Status: "running", Ports: ports, URLs: []string{"http://url-secret"}, Error: "raw-error", Deploy: CelldDeployment{Command: "deploy-secret"}, Storage: CelldLocation{Bucket: "bucket-secret"}, Servers: []CelldMachine{
			{Name: "alpha", IP: "10.0.0.1", Running: true, Healthy: true, Ports: ports},
			{Name: "beta", IP: "10.0.0.2", Ports: ports, Error: "raw-error"},
		}},
		{ID: "f2", Name: "blog", Status: "stopped", Ports: CelldPorts{HTTP: 19080, Internal: 19081}, Servers: []CelldMachine{
			{Name: "alpha", IP: "10.0.0.1", Ports: CelldPorts{HTTP: 19080, Internal: 19081}},
			{Name: "gone", Ports: CelldPorts{HTTP: 19080, Internal: 19081}},
		}},
	}
	return in
}

func TestBuildNetworkGraphCelldNodes(t *testing.T) {
	graph := BuildNetworkGraph(celldFixture())
	for id, status := range map[string]string{"celld:f1": "up", "celld:f1:alpha": "up", "celld:f1:beta": "down", "celld:f2": "disabled", "celld:f2:alpha": "disabled", "celld:f2:gone": "disabled"} {
		if got := graphNode(t, graph, id).Status; got != status {
			t.Errorf("%s status = %s, want %s", id, got, status)
		}
	}
	if graphNode(t, graph, "celld:f1:beta").Host != "beta" {
		t.Error("member host should be its machine")
	}
	if !hasEdge(graph, "celld:f1", "celld:f1:beta", "celld_member", "") || !hasEdge(graph, "host:alpha", "celld:f2:alpha", "runs", "") {
		t.Errorf("missing celld edges: %+v", graph.Edges)
	}
	for _, edge := range graph.Edges {
		if edge.Target == "celld:f2:gone" && edge.Type == "runs" {
			t.Error("a member on an unknown host must not leave a dangling edge")
		}
	}
	data, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"url-secret", "raw-error", "deploy-secret", "bucket-secret"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("%s leaked into the graph", secret)
		}
	}
}

func TestBuildNetworkGraphCelldPortMatching(t *testing.T) {
	in := celldFixture()
	in.Aliases = []dnsmasq.AliasEntry{{Alias: "shop.home", IP: "10.0.0.2"}}
	in.ResolvedHosts = map[string][]string{"beta.lan": {"10.0.0.2"}}
	in.Proxies = []npm.Proxy{
		{ID: 1, ForwardHost: "10.0.0.1", ForwardPort: 18080},
		{ID: 2, ForwardHost: "10.0.0.1", ForwardPort: 19081},
		{ID: 3, ForwardHost: "10.0.0.1", ForwardPort: 22},
		{ID: 4, ForwardHost: "shop.home", ForwardPort: 18080},
		{ID: 5, ForwardHost: "beta.lan", ForwardPort: 18081},
		{ID: 6, ForwardHost: "localhost", ForwardPort: 18080},
	}
	in.Streams = []npm.Stream{{ID: 1, Forwarding_host: "10.0.0.2", Forwarding_port: 18081}}
	graph := BuildNetworkGraph(in)
	for source, target := range map[string]string{"proxy:1": "celld:f1:alpha", "proxy:2": "celld:f2:alpha", "proxy:4": "celld:f1:beta", "proxy:5": "celld:f1:beta", "proxy:6": "celld:f1:alpha", "stream:1": "celld:f1:beta"} {
		if !hasEdge(graph, source, target, "forwards", "celld_port") {
			t.Errorf("%s should link to %s: %+v", source, target, graph.Edges)
		}
	}
	if !hasEdge(graph, "proxy:3", "host:alpha", "forwards", "ip") {
		t.Error("a non-celld port on the same address should stay on the host")
	}
	for _, flow := range graph.Flows {
		if flow.Source == "proxy:1" && (flow.Target != "celld:f1:alpha" || len(flow.Endpoints) != 1 || flow.Endpoints[0] != "10.0.0.1:18080") {
			t.Errorf("proxy flow = %+v", flow)
		}
	}
}

func TestBuildNetworkGraphCelldMemberWithoutIPHasNoPortMatch(t *testing.T) {
	in := celldFixture()
	in.Fleets[0].Servers[1].IP = ""
	in.Proxies = []npm.Proxy{{ID: 1, ForwardHost: "10.0.0.2", ForwardPort: 18080}}
	graph := BuildNetworkGraph(in)
	graphNode(t, graph, "celld:f1:beta")
	if !hasEdge(graph, "proxy:1", "host:beta", "forwards", "ip") {
		t.Errorf("expected the host route: %+v", graph.Edges)
	}
}
