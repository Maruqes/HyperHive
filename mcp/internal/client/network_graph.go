package client

import (
	"context"
	"errors"
)

const NetworkGraphPath = "/network/graph"

type NetworkGraphNode struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Label   string         `json:"label"`
	Host    string         `json:"host"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details"`
}

// NetworkGraphEdge is a configured route; Match records how a target was identified.
type NetworkGraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
	Match  string `json:"match"`
}

type NetworkGraph struct {
	GeneratedAt string             `json:"generatedAt"`
	Nodes       []NetworkGraphNode `json:"nodes"`
	Edges       []NetworkGraphEdge `json:"edges"`
	Warnings    []string           `json:"warnings"`
}

func (c *Client) GetNetworkGraph(ctx context.Context) (NetworkGraph, error) {
	var graph NetworkGraph
	if err := c.getJSON(ctx, NetworkGraphPath, &graph); err != nil {
		return NetworkGraph{}, err
	}
	if graph.Nodes == nil || graph.Edges == nil || graph.Warnings == nil {
		return NetworkGraph{}, errors.New("HyperHive API returned an invalid network graph")
	}
	return graph, nil
}
