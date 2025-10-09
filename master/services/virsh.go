package services

import (
	"512SvMan/db"
	"512SvMan/protocol"
	"512SvMan/virsh"
	"fmt"
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

// vmReq.MachineName, vmReq.Name, vmReq.Memory, vmReq.Vcpu, vmReq.NfsShareId, vmReq.DiskSizeGB, vmReq.IsoID, vmReq.Network, vmReq.VNCPassword
func (v *VirshService) CreateVM(machine_name string, name string, memory int32, vcpu int32, nfsShareId int, diskSizeGB int32, isoID int, network string, VNCPassword string) error {
	slaveMachine := protocol.GetConnectionByMachineName(machine_name)
	if slaveMachine == nil {
		return fmt.Errorf("machine %s not found", machine_name)
	}

	//get disk path from nfsShareId
	nfsShare, err := db.GetNFSShareByID(nfsShareId)
	if err != nil {
		return fmt.Errorf("failed to get NFS share by ID: %v", err)
	}
	if nfsShare == nil {
		return fmt.Errorf("NFS share with ID %d not found", nfsShareId)
	}

	//get iso path from isoID
	iso, err := db.GetIsoByID(isoID)
	if err != nil {
		return fmt.Errorf("failed to get ISO by ID: %v", err)
	}
	if iso == nil {
		return fmt.Errorf("ISO with ID %d not found", isoID)
	}
	isoPath := iso.FilePath

	var qcowFile string
	if nfsShare.Target[len(nfsShare.Target)-1] != '/' {
		qcowFile = nfsShare.Target + "/" + name + ".qcow2"
	} else {
		qcowFile = nfsShare.Target + name + ".qcow2"
	}

	return virsh.CreateVM(slaveMachine.Connection, name, memory, vcpu, qcowFile, diskSizeGB, isoPath, network, VNCPassword)
}
