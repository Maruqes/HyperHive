package npm

import (
	"512SvMan/db"
	"context"
)

type UpstreamMethodCapability struct {
	Name            string   `json:"name"`
	Status          string   `json:"status"`
	RequiresHashKey bool     `json:"requires_hash_key"`
	HashKeys        []string `json:"hash_keys,omitempty"`
}
type UpstreamCapabilityResponse struct {
	RecoveryID           int64                                 `json:"recovery_id,omitempty"`
	Image                string                                `json:"image_digest"`
	Status               string                                `json:"status"`
	Contexts             map[string][]UpstreamMethodCapability `json:"contexts"`
	ServerParameters     []string                              `json:"server_parameters"`
	Unsupported          []string                              `json:"unsupported"`
	SharedZone           bool                                  `json:"shared_zone"`
	ConnectionLimitScope string                                `json:"connection_limit_scope"`
	MaxMembers           int                                   `json:"max_members"`
}

func UpstreamCapabilities(ctx context.Context) (UpstreamCapabilityResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, upstreamReadTimeout)
	defer cancel()
	if err := lockUpstreamRead(ctx); err != nil {
		return UpstreamCapabilityResponse{}, err
	}
	defer upstreamCoordinator.Unlock()
	status := "verified"
	if err := upstreamCoordinator.runtime.Verify(ctx); err != nil {
		status = "unknown"
	}
	if db.DB != nil && !upstreamCoordinator.ready {
		status = "reconciliation_required"
	}
	if upstreamCoordinator.recovery != 0 {
		status = "recovery_required"
	}
	r := UpstreamCapabilityResponse{Image: UpstreamImageDigest, Status: status, RecoveryID: upstreamCoordinator.recovery, Contexts: map[string][]UpstreamMethodCapability{},
		ServerParameters: []string{"weight", "max_conns", "max_fails", "fail_timeout", "backup", "down"}, Unsupported: []string{"least_time", "active_health_checks", "dns_members", "unix_members"},
		SharedZone: true, ConnectionLimitScope: "shared_across_workers", MaxMembers: MaxUpstreamMembers}
	for _, context := range []string{"http", "stream"} {
		for _, method := range []string{"round_robin", "least_conn", "random", "random_two", "hash", "hash_consistent", "ip_hash"} {
			if context == "stream" && method == "ip_hash" {
				continue
			}
			m := UpstreamMethodCapability{Name: method, Status: status}
			if method == "hash" || method == "hash_consistent" {
				m.RequiresHashKey = true
				m.HashKeys = []string{"$remote_addr"}
				if context == "http" {
					m.HashKeys = append(m.HashKeys, "$request_uri", "$uri", "$host", "$remote_addr$uri")
				}
			}
			r.Contexts[context] = append(r.Contexts[context], m)
		}
	}
	return r, nil
}
