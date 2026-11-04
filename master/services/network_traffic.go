package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"syscall"
	"time"

	"512SvMan/db"
	"512SvMan/info"
	"512SvMan/protocol"
	"512SvMan/virsh"
	"512SvMan/wireguard"

	infoGrpc "github.com/Maruqes/512SvMan/api/proto/info"
	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const (
	networkTrafficTimeout = 1500 * time.Millisecond
	// Tabs polling together share one sample instead of shrinking the interval.
	networkTrafficMinInterval = time.Second
	// An older baseline would report an average, not what is happening now.
	networkTrafficMaxGap = 15 * time.Second
	tcpStateListen       = 10
)

// NetworkRate is bytes per second. On a node it is that node's own view; on an
// endpoint, Sent goes towards the target and Received comes back from it.
type NetworkRate struct {
	Sent     float64 `json:"sent"`
	Received float64 `json:"received"`
}

// NetworkTraffic lists only what moved during the last interval.
type NetworkTraffic struct {
	At        time.Time              `json:"at"`
	Endpoints map[string]NetworkRate `json:"endpoints"`
	Nodes     map[string]NetworkRate `json:"nodes"`
	Warnings  []string               `json:"warnings"`
}

type networkCounters struct {
	Sent     uint64
	Received uint64
}

type networkSocket struct {
	Endpoint string
	networkCounters
}

type networkTrafficSample struct {
	At    time.Time
	Nodes map[string]networkCounters
	// Nil means collection failed, so the next sample cannot tell new sockets from old.
	Sockets map[string]networkSocket
}

var networkTrafficState struct {
	sync.Mutex
	last   networkTrafficSample
	result NetworkTraffic
}

// Traffic keeps one sample so rates are computed here: socket totals fall whenever a
// connection closes, which a client comparing plain counters would read as a reset.
func (s *NetworkGraphService) Traffic(ctx context.Context) NetworkTraffic {
	state := &networkTrafficState
	state.Lock()
	defer state.Unlock()
	if time.Since(state.last.At) < networkTrafficMinInterval {
		return state.result
	}
	// A closed tab must not cut the sample short, the next poll compares against it.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), networkTrafficTimeout)
	defer cancel()
	current, warnings := collectNetworkTraffic(ctx)
	state.result = networkTrafficRates(state.last, current)
	state.result.Warnings = warnings
	state.last = current
	return state.result
}

func collectNetworkTraffic(ctx context.Context) (networkTrafficSample, []string) {
	sample := networkTrafficSample{Nodes: map[string]networkCounters{}}
	warnings := []string{}
	var mu sync.Mutex
	warn := func(text string) {
		mu.Lock()
		defer mu.Unlock()
		warnings = append(warnings, text)
	}
	add := func(id string, counters networkCounters) {
		mu.Lock()
		defer mu.Unlock()
		sample.Nodes[id] = counters
	}

	var wg sync.WaitGroup
	run := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}
	for _, conn := range protocol.GetConnectionsSnapshot() {
		run(func() {
			summary := collectNetworkSource(ctx, "host "+conn.MachineName+" traffic", warn, func(ctx context.Context) (*infoGrpc.NetworkSummary, error) {
				return info.GetNetworkSummaryCtx(ctx, conn.Connection, &infoGrpc.Empty{})
			})
			for _, stat := range summary.GetStats() {
				add(networkInterfaceID(conn.MachineName, stat.GetName()), networkCounters{stat.GetBytesSent(), stat.GetBytesRecv()})
			}
		})
		run(func() {
			// An older slave lacks this RPC and ends up here as unavailable.
			stats, err := runNetworkSource(ctx, func(ctx context.Context) (vmStats []vmCounters, err error) {
				res, err := virsh.GetVMInterfaceStats(ctx, conn.Connection)
				for _, vm := range res.GetVms() {
					vmStats = append(vmStats, vmCounters{vm.GetName(), networkCounters{vm.GetTxBytes(), vm.GetRxBytes()}})
				}
				return vmStats, err
			})
			if err != nil {
				warn("host " + conn.MachineName + " VM traffic unavailable")
			}
			for _, vm := range stats {
				add(networkVMID(conn.MachineName, vm.name), vm.counters)
			}
		})
	}
	run(func() {
		for id, counters := range collectNetworkSource(ctx, "WireGuard traffic", warn, collectWireguardTraffic) {
			add(id, counters)
		}
	})
	run(func() {
		sockets, err := collectNetworkSockets()
		if err != nil {
			warn("connection traffic unavailable")
			return
		}
		mu.Lock()
		defer mu.Unlock()
		sample.Sockets = sockets
	})
	wg.Wait()
	sample.At = time.Now().UTC()
	return sample, warnings
}

type vmCounters struct {
	name     string
	counters networkCounters
}

// collectWireguardTraffic flips the device's view, so a peer's rate reads as the peer's own.
func collectWireguardTraffic(ctx context.Context) (map[string]networkCounters, error) {
	peers, err := db.GetAllWireguardPeers(ctx)
	if err != nil || len(peers) == 0 {
		return nil, err
	}
	live, err := wireguard.GetPeers()
	if err != nil {
		return nil, err
	}
	return wireguardTraffic(peers, live), nil
}

func wireguardTraffic(peers []db.WireguardPeer, live []wgtypes.Peer) map[string]networkCounters {
	out := map[string]networkCounters{}
	for _, peer := range peers {
		if device, ok := liveNetworkPeer(peer.ClientIP, live); ok {
			out[networkPeerID(peer.Id)] = networkCounters{uint64(device.ReceiveBytes), uint64(device.TransmitBytes)}
		}
	}
	return out
}

// collectNetworkSockets reads the sockets of this host, which is also NPM's: it uses host networking.
func collectNetworkSockets() (map[string]networkSocket, error) {
	var responses []*netlink.InetDiagTCPInfoResp
	var failure error
	succeeded := false
	for _, family := range []uint8{syscall.AF_INET, syscall.AF_INET6} {
		found, err := netlink.SocketDiagTCPInfo(family)
		// A partial dump looks like closed sockets, then like new ones with their whole history.
		if errors.Is(err, netlink.ErrDumpInterrupted) {
			return nil, err
		}
		if err != nil {
			failure = err
			continue
		}
		succeeded = true
		responses = append(responses, found...)
	}
	if !succeeded {
		return nil, failure
	}
	return networkSockets(responses), nil
}

// networkSockets keeps outbound sockets. An accepted socket shares its local port with
// a listener, and counting it would add every client and both ends of a loopback hop.
func networkSockets(responses []*netlink.InetDiagTCPInfoResp) map[string]networkSocket {
	listening := map[uint16]bool{}
	for _, response := range responses {
		if response != nil && response.InetDiagMsg != nil && response.InetDiagMsg.State == tcpStateListen {
			listening[response.InetDiagMsg.ID.SourcePort] = true
		}
	}
	out := map[string]networkSocket{}
	for _, response := range responses {
		if response == nil || response.InetDiagMsg == nil || response.TCPInfo == nil {
			continue
		}
		msg := response.InetDiagMsg
		if msg.State == tcpStateListen || listening[msg.ID.SourcePort] {
			continue
		}
		ip, ok := networkIP(msg.ID.Destination.String())
		if !ok {
			continue
		}
		out[fmt.Sprint(msg.ID.Cookie)] = networkSocket{
			Endpoint:        net.JoinHostPort(ip, strconv.Itoa(int(msg.ID.DestinationPort))),
			networkCounters: networkCounters{response.TCPInfo.Bytes_acked, response.TCPInfo.Bytes_received},
		}
	}
	return out
}

func networkTrafficRates(previous, current networkTrafficSample) NetworkTraffic {
	out := NetworkTraffic{At: current.At, Endpoints: map[string]NetworkRate{}, Nodes: map[string]NetworkRate{}}
	elapsed := current.At.Sub(previous.At)
	if previous.At.IsZero() || elapsed <= 0 || elapsed > networkTrafficMaxGap {
		return out
	}
	seconds := elapsed.Seconds()
	for id, now := range current.Nodes {
		// A missing baseline or a lower counter (VM restart) gives no rate rather than a spike.
		if before, ok := previous.Nodes[id]; ok && now.Sent >= before.Sent && now.Received >= before.Received {
			addNetworkRate(out.Nodes, id, now, before, seconds)
		}
	}
	if previous.Sockets == nil {
		return out
	}
	for id, now := range current.Sockets {
		// A socket missing from the baseline opened this interval, so all of it is new.
		before := previous.Sockets[id]
		if now.Sent >= before.Sent && now.Received >= before.Received {
			addNetworkRate(out.Endpoints, now.Endpoint, now.networkCounters, before.networkCounters, seconds)
		}
	}
	return out
}

func addNetworkRate(rates map[string]NetworkRate, id string, now, before networkCounters, seconds float64) {
	if now == before {
		return
	}
	rate := rates[id]
	rate.Sent += float64(now.Sent-before.Sent) / seconds
	rate.Received += float64(now.Received-before.Received) / seconds
	rates[id] = rate
}
