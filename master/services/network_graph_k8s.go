package services

import (
	"net"
	"strconv"

	k8sGrpc "github.com/Maruqes/512SvMan/api/proto/k8s"
)

const k8sClusterID = "k8s:cluster"

func networkK8sServiceID(namespace, name string) string {
	return "k8s:service:" + namespace + ":" + name
}

func (b *networkGraphBuilder) addK8s() {
	cluster := b.in.K8s
	if cluster == nil {
		return
	}
	if !cluster.Available {
		b.addWarning("kubernetes unavailable")
		return
	}
	b.addK8sCluster(cluster.Nodes)

	targeted := map[string]bool{}
	for _, ingress := range cluster.Ingresses {
		for _, rule := range ingress.Rules {
			targeted[networkK8sServiceID(ingress.Namespace, rule.Service)] = true
		}
	}
	for _, service := range cluster.Services {
		id := networkK8sServiceID(service.Namespace, service.Name)
		internal := service.Namespace == "default" && service.Name == "kubernetes" || service.Namespace == "kube-system" && service.Type == "ClusterIP"
		if internal && !targeted[id] {
			continue
		}
		b.addK8sService(id, service)
	}
	for _, ingress := range cluster.Ingresses {
		b.addK8sIngress(ingress)
	}
}

func (b *networkGraphBuilder) addK8sCluster(nodes []*k8sGrpc.K8SNode) {
	ready := 0
	details := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		if node.Ready {
			ready++
		}
		details = append(details, map[string]any{"name": node.Name, "addresses": append([]string{}, node.Addresses...), "ready": node.Ready})
		for _, raw := range node.Addresses {
			ip, ok := networkIP(raw)
			if !ok {
				continue
			}
			b.k8sNodeIPs[ip] = true
			// Another address of the same machine reaches the node too; shared ones say nothing.
			if owners := b.nodesForIP(ip); len(owners) == 1 {
				b.k8sNodeOwners[owners[0]] = true
			}
		}
	}
	status := networkStatusUnknown
	switch ready {
	case 0:
		status = networkStatusDown
	case len(nodes):
		status = networkStatusUp
	}
	b.node(k8sClusterID, "k8s_cluster", "Kubernetes", b.masterName, status, map[string]any{"nodes": details, "ready": ready, "total": len(nodes)})
}

func (b *networkGraphBuilder) addK8sService(id string, service *k8sGrpc.K8SServiceInfo) {
	clusterIP, hasClusterIP := networkIP(service.ClusterIp)
	ports := make([]map[string]any, 0, len(service.Ports))
	for _, port := range service.Ports {
		ports = append(ports, map[string]any{"name": port.Name, "port": port.Port, "nodePort": port.NodePort, "protocol": port.Protocol})
		if hasClusterIP {
			endpoint := net.JoinHostPort(clusterIP, strconv.Itoa(int(port.Port)))
			b.k8sClusterPorts[endpoint] = append(b.k8sClusterPorts[endpoint], id)
		}
		if port.NodePort > 0 {
			b.k8sNodePorts[int(port.NodePort)] = append(b.k8sNodePorts[int(port.NodePort)], id)
		}
		if service.Type == "LoadBalancer" {
			b.k8sNodePorts[int(port.Port)] = append(b.k8sNodePorts[int(port.Port)], id)
		}
	}
	b.node(id, "k8s_service", service.Name, b.masterName, networkStatusUnknown, map[string]any{"namespace": service.Namespace, "serviceType": service.Type, "clusterIP": service.ClusterIp, "loadBalancerIPs": append([]string{}, service.LoadBalancerIps...), "ports": ports})
	b.edge(k8sClusterID, id, "k8s_member", "")
}

func (b *networkGraphBuilder) addK8sIngress(ingress *k8sGrpc.K8SIngress) {
	id := "k8s:ingress:" + ingress.Namespace + ":" + ingress.Name
	rules := make([]map[string]any, 0, len(ingress.Rules))
	for _, rule := range ingress.Rules {
		rules = append(rules, map[string]any{"host": rule.Host, "path": rule.Path, "service": rule.Service, "port": rule.Port})
		if host := normalizeNetworkHost(rule.Host); host != "" {
			if b.k8sIngressHosts[host] == nil {
				b.k8sIngressHosts[host] = map[string]bool{}
			}
			b.k8sIngressHosts[host][id] = true
		}
		if service := networkK8sServiceID(ingress.Namespace, rule.Service); b.nodes[service] != nil {
			b.edge(id, service, "k8s_route", "")
		}
	}
	b.node(id, "k8s_ingress", ingress.Name, b.masterName, networkStatusUnknown, map[string]any{"namespace": ingress.Namespace, "rules": rules})
	b.edge(k8sClusterID, id, "k8s_member", "")
}

// k8sTarget runs before resolve, which would leave an unknown node for a cluster IP or for
// a cluster node that is not a HyperHive machine.
func (b *networkGraphBuilder) k8sTarget(domains []string, host string, port int) (string, string, bool) {
	var ids []string
	for _, endpoint := range b.endpoints(host, port) {
		ids = append(ids, b.k8sClusterPorts[endpoint]...)
	}
	if len(ids) == 0 && len(b.k8sNodePorts[port]) > 0 && b.onK8sNode(host, port) {
		if id, ok := b.k8sIngress(domains); ok {
			return id, networkMatchK8sIngress, true
		}
		ids = b.k8sNodePorts[port]
	}
	if len(ids) == 0 {
		return "", "", false
	}
	target, match := b.pick(normalizeNetworkHost(host), ids, nil, networkMatchK8sPort)
	return target, match, true
}

// onK8sNode is false when a published Docker port claims the target, because Docker matches first.
func (b *networkGraphBuilder) onK8sNode(host string, port int) bool {
	if isLoopbackNetworkHost(normalizeNetworkHost(host)) {
		return b.k8sNodeOwners[b.masterID] && len(b.dockerPorts[networkDockerPortKey(b.masterID, port)]) == 0
	}
	found := false
	for _, endpoint := range b.endpoints(host, port) {
		ip, _ := networkIP(endpoint)
		owners := b.nodesForIP(ip)
		if b.k8sNodeIPs[ip] || len(owners) == 1 && b.k8sNodeOwners[owners[0]] {
			found = true
		}
		for _, owner := range owners {
			if len(b.dockerPorts[networkDockerPortKey(owner, port)]) > 0 {
				return false
			}
		}
	}
	return found
}

// k8sIngress leaves several matches to the service: ingresses often split one host by path.
func (b *networkGraphBuilder) k8sIngress(domains []string) (string, bool) {
	found := map[string]bool{}
	for _, domain := range domains {
		for id := range b.k8sIngressHosts[normalizeNetworkHost(domain)] {
			found[id] = true
		}
	}
	if len(found) != 1 {
		return "", false
	}
	for id := range found {
		return id, true
	}
	return "", false
}
