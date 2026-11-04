package tools

import (
	"context"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/safety"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NetworkGraphAPI is read-only: the graph is derived from configuration and never stored.
type NetworkGraphAPI interface {
	GetNetworkGraph(context.Context) (client.NetworkGraph, error)
}

func defineNetworkGraphGet(api NetworkGraphAPI) ToolDefinition {
	destructive := false
	tool := &mcp.Tool{
		Name:        "network_graph_get",
		Description: "Read the HyperHive network flow graph, built on demand: hosts, interfaces, bridges, VMs and their NICs, domains, proxies, redirections, streams, upstream pools, celld fleets and their per-host members, Docker containers, Kubernetes clusters, ingresses and services, and WireGuard peers. Edges are configured routes, not proof of traffic; match records how a target was identified (dns_alias, hostname, ip, loopback, domain, upstream, celld_port, docker_port, k8s_port, k8s_ingress, or none for an unknown node). A failed source adds a string to warnings and leaves the graph partial, so check warnings before treating a missing node as absent. Calls GET /network/graph. Takes no arguments.",
		Meta:        mcp.Meta{"hyperhive/endpoint": map[string]any{"method": "GET", "path": client.NetworkGraphPath}},
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &destructive, IdempotentHint: true},
	}
	return defineTool(tool, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, client.NetworkGraph, error) {
		graph, err := api.GetNetworkGraph(ctx)
		return nil, graph, err
	})
}

// RegisterNetworkGraph is the complete tool and safety catalog for this area.
func RegisterNetworkGraph(server *Registry, api NetworkGraphAPI) {
	server = server.Area("network_graph")
	RegisterTool(server, "network_graph_get", defineNetworkGraphGet(api), safety.Low)
}
