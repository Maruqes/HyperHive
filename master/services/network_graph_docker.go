package services

import (
	"fmt"
	"strings"

	dockerGrpc "github.com/Maruqes/512SvMan/api/proto/docker"
)

func networkContainerStatus(state string) string {
	switch state {
	case "running":
		return networkStatusUp
	case "exited", "created":
		return networkStatusDisabled
	case "dead":
		return networkStatusDown
	}
	return networkStatusUnknown
}

func networkDockerPortKey(hostID string, port int) string {
	return fmt.Sprintf("%s:%d", hostID, port)
}

func (b *networkGraphBuilder) addContainers() {
	for _, host := range b.in.Hosts {
		if host.Docker == nil {
			continue
		}
		if !host.Docker.Available {
			b.addWarning("host " + host.Name + " docker unavailable")
			continue
		}
		hostID := networkHostID(host.Name)
		for _, container := range host.Docker.Containers {
			b.addContainer(host.Name, hostID, container)
		}
	}
}

func (b *networkGraphBuilder) addContainer(hostName, hostID string, container *dockerGrpc.Container) {
	id := "container:" + hostName + ":" + container.Name
	networks := make([]map[string]any, 0, len(container.Networks))
	addresses := make([]string, 0, len(container.Networks))
	for _, network := range container.Networks {
		networks = append(networks, map[string]any{"name": network.Name, "ip": network.Ip})
		if network.Ip == "" {
			continue
		}
		addresses = append(addresses, network.Ip)
		// shortcut: only NPM's host can route to a Docker bridge address and bridge IPs
		// repeat across hosts, so other hosts match by published port only; upgrade if
		// routed container networks appear.
		if hostName == b.masterName {
			b.indexIP(network.Ip, id)
		}
	}
	ports := make([]map[string]any, 0, len(container.Ports))
	for _, port := range container.Ports {
		ports = append(ports, map[string]any{"ip": port.Ip, "privatePort": port.PrivatePort, "publicPort": port.PublicPort, "protocol": port.Protocol})
		if port.PublicPort > 0 {
			key := networkDockerPortKey(hostID, int(port.PublicPort))
			b.dockerPorts[key] = append(b.dockerPorts[key], id)
		}
	}
	b.node(id, "container", container.Name, hostName, networkContainerStatus(container.State), map[string]any{"image": container.Image, "state": container.State, "networks": networks, "ports": ports, "addresses": addresses})
	b.edge(hostID, id, "runs", "")
}

// dockerTarget refines a resolved target; a bare host match on another port stays as it is.
func (b *networkGraphBuilder) dockerTarget(host, target, match string, port int) (string, string) {
	if strings.HasPrefix(target, "container:") {
		return target, networkMatchDockerPort
	}
	if ids := b.dockerPorts[networkDockerPortKey(target, port)]; len(ids) > 0 {
		return b.pick(normalizeNetworkHost(host), ids, nil, networkMatchDockerPort)
	}
	return target, match
}
