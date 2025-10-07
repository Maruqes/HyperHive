package services

import (
	"512SvMan/protocol"
	"512SvMan/virsh"
	"sort"
)

type VirshService struct {
}

func getDisableFeatures(allFeatures [][]string) []string {
	featureCount := make(map[string]int)
	machines := 0

	// Count each feature at most once per machine
	for _, feats := range allFeatures {
		if len(feats) == 0 {
			continue
		}
		machines++
		seen := make(map[string]struct{}, len(feats))
		for _, f := range feats {
			if _, ok := seen[f]; ok {
				continue 
			}
			seen[f] = struct{}{}
		}
		for f := range seen {
			featureCount[f]++
		}
	}

	// With 0 or 1 machine, there's nothing to "disable"
	if machines <= 1 {
		return []string{}
	}

	// A feature is "disabled" if it doesn't appear on every machine
	disable := make([]string, 0)
	for f, c := range featureCount {
		if c < machines {
			disable = append(disable, f)
		}
	}

	sort.Strings(disable)
	return disable
}

func (v *VirshService) GetCpuDisableFeatures() ([]string, error) {
	var features [][]string
	for _, conn := range protocol.GetAllGRPCConnections() {
		features_conn := virsh.GetCpuFeatures(conn)
		features = append(features, features_conn)
	}
	return getDisableFeatures(features), nil
}
