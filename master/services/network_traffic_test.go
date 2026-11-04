package services

import (
	"net"
	"reflect"
	"testing"
	"time"

	"512SvMan/db"
	"512SvMan/dnsmasq"
	"512SvMan/npm"

	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestNetworkTrafficRates(t *testing.T) {
	start := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	previous := networkTrafficSample{
		At: start,
		Nodes: map[string]networkCounters{
			"vm:a:web":      {100, 200},
			"vm:a:idle":     {5, 5},
			"vm:a:rebooted": {900, 900},
		},
		Sockets: map[string]networkSocket{
			"kept":   {"10.0.0.5:80", networkCounters{1000, 2000}},
			"closed": {"10.0.0.5:80", networkCounters{5000, 5000}},
		},
	}
	current := networkTrafficSample{
		At: start.Add(2 * time.Second),
		Nodes: map[string]networkCounters{
			"vm:a:web":      {300, 600},
			"vm:a:idle":     {5, 5},
			"vm:a:rebooted": {10, 10},
			"vm:a:started":  {50, 50},
		},
		Sockets: map[string]networkSocket{
			"kept":   {"10.0.0.5:80", networkCounters{1200, 2400}},
			"opened": {"10.0.0.5:80", networkCounters{100, 600}},
			"other":  {"10.0.0.6:443", networkCounters{20, 0}},
		},
	}
	got := networkTrafficRates(previous, current)
	wantNodes := map[string]NetworkRate{"vm:a:web": {100, 200}}
	if !reflect.DeepEqual(got.Nodes, wantNodes) {
		t.Fatalf("nodes = %+v, want %+v", got.Nodes, wantNodes)
	}
	// A closed socket lowers the live total, but must not lower the rate.
	wantEndpoints := map[string]NetworkRate{"10.0.0.5:80": {150, 500}, "10.0.0.6:443": {10, 0}}
	if !reflect.DeepEqual(got.Endpoints, wantEndpoints) {
		t.Fatalf("endpoints = %+v, want %+v", got.Endpoints, wantEndpoints)
	}
}

func TestNetworkTrafficRatesNeedsAUsableBaseline(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	current := networkTrafficSample{
		At:      now,
		Nodes:   map[string]networkCounters{"vm:a:web": {500, 500}},
		Sockets: map[string]networkSocket{"s": {"10.0.0.5:80", networkCounters{500, 500}}},
	}
	for name, previous := range map[string]networkTrafficSample{
		"first sample": {},
		"stale":        {At: now.Add(-time.Minute), Nodes: map[string]networkCounters{"vm:a:web": {1, 1}}, Sockets: map[string]networkSocket{}},
	} {
		got := networkTrafficRates(previous, current)
		if len(got.Nodes) != 0 || len(got.Endpoints) != 0 {
			t.Fatalf("%s: unexpected rates %+v", name, got)
		}
	}
	// Failed socket collection: every live socket would otherwise look brand new.
	got := networkTrafficRates(networkTrafficSample{At: now.Add(-time.Second), Nodes: map[string]networkCounters{"vm:a:web": {400, 400}}}, current)
	if len(got.Endpoints) != 0 || got.Nodes["vm:a:web"].Sent != 100 {
		t.Fatalf("unexpected rates after failed socket sample: %+v", got)
	}
}

func diagSocket(state uint8, localPort uint16, remote string, remotePort uint16, cookie uint32, sent, received uint64) *netlink.InetDiagTCPInfoResp {
	msg := &netlink.Socket{State: state}
	msg.ID.SourcePort, msg.ID.DestinationPort = localPort, remotePort
	msg.ID.Destination = net.ParseIP(remote)
	msg.ID.Cookie = [2]uint32{cookie, 0}
	return &netlink.InetDiagTCPInfoResp{InetDiagMsg: msg, TCPInfo: &netlink.TCPInfo{Bytes_acked: sent, Bytes_received: received}}
}

func TestNetworkSocketsKeepsOnlyOutbound(t *testing.T) {
	const established = 1
	sockets := networkSockets([]*netlink.InetDiagTCPInfoResp{
		nil,
		{InetDiagMsg: &netlink.Socket{State: tcpStateListen}},
		diagSocket(tcpStateListen, 443, "0.0.0.0", 0, 1, 0, 0),
		diagSocket(tcpStateListen, 8080, "0.0.0.0", 0, 2, 0, 0),
		diagSocket(established, 443, "203.0.113.9", 51000, 3, 9, 9),
		diagSocket(established, 8080, "127.0.0.1", 40000, 4, 9, 9),
		diagSocket(established, 40000, "127.0.0.1", 8080, 5, 10, 20),
		diagSocket(established, 41000, "10.0.0.5", 80, 6, 30, 40),
		diagSocket(established, 41001, "::ffff:10.0.0.5", 80, 7, 1, 2),
	})
	want := map[string]networkSocket{
		"[5 0]": {"127.0.0.1:8080", networkCounters{10, 20}},
		"[6 0]": {"10.0.0.5:80", networkCounters{30, 40}},
		"[7 0]": {"10.0.0.5:80", networkCounters{1, 2}},
	}
	if !reflect.DeepEqual(sockets, want) {
		t.Fatalf("sockets = %+v, want %+v", sockets, want)
	}
}

func TestWireguardTrafficUsesThePeerView(t *testing.T) {
	_, allowed, err := net.ParseCIDR("10.8.0.2/32")
	if err != nil {
		t.Fatal(err)
	}
	got := wireguardTraffic([]db.WireguardPeer{{Id: 7, ClientIP: "10.8.0.2/32"}, {Id: 8, ClientIP: "10.8.0.3/32"}}, []wgtypes.Peer{{AllowedIPs: []net.IPNet{*allowed}, ReceiveBytes: 11, TransmitBytes: 22}})
	if want := (map[string]networkCounters{"wireguard:peer:7": {11, 22}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("traffic = %+v, want %+v", got, want)
	}
}

func TestBuildNetworkGraphFlows(t *testing.T) {
	graph := BuildNetworkGraph(NetworkGraphInputs{
		Hosts:     []NetworkGraphHost{{Name: "alpha", Addr: "10.0.0.1", Online: true, Master: true}},
		Aliases:   []dnsmasq.AliasEntry{{Alias: "app.lan", IP: "10.0.0.7"}},
		Upstreams: []npm.Upstream{{ID: 3, Name: "pool", Members: []npm.UpstreamMember{{IP: "10.0.0.8", Port: 9000}, {IP: "10.0.0.9", Port: 9000}}}},
		Proxies: []npm.Proxy{
			{ID: 1, ForwardHost: "app.lan", ForwardPort: 80, Locations: []npm.Location{{ForwardHost: "app.lan", ForwardPort: 81}}},
			{ID: 2, ForwardHost: "127.0.0.1", ForwardPort: 8080},
		},
		Streams: []npm.Stream{{ID: 4, Forwarding_host: "10.0.0.6", Forwarding_port: 25, UpstreamID: []byte("3")}, {ID: 5, Forwarding_host: "10.0.0.6", Forwarding_port: 25}},
	})
	got := map[string][]string{}
	for _, flow := range graph.Flows {
		got[flow.Source+">"+flow.Target] = flow.Endpoints
	}
	want := map[string][]string{
		"proxy:1>unknown:app.lan":     {"10.0.0.7:80", "10.0.0.7:81"},
		"proxy:2>host:alpha":          {"127.0.0.1:8080", "[::1]:8080"},
		"stream:4>upstream:3":         {"10.0.0.8:9000", "10.0.0.9:9000"},
		"stream:5>unknown:10.0.0.6":   {"10.0.0.6:25"},
		"upstream:3>unknown:10.0.0.8": {"10.0.0.8:9000"},
		"upstream:3>unknown:10.0.0.9": {"10.0.0.9:9000"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flows = %+v, want %+v", got, want)
	}
}
