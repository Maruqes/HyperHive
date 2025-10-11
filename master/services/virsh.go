package services

import (
	"512SvMan/db"
	"512SvMan/protocol"
	"512SvMan/virsh"
	"fmt"
	"sort"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
)

type VirshService struct {
}

// func getDisableFeatures(allFeatures [][]string) []string {
// 	featureCount := make(map[string]int)
// 	machines := 0

// 	// Count each feature at most once per machine
// 	for _, feats := range allFeatures {
// 		if len(feats) == 0 {
// 			continue
// 		}
// 		machines++
// 		seen := make(map[string]struct{}, len(feats))
// 		for _, f := range feats {
// 			if _, ok := seen[f]; ok {
// 				continue
// 			}
// 			seen[f] = struct{}{}
// 		}
// 		for f := range seen {
// 			featureCount[f]++
// 		}
// 	}

// 	// With 0 or 1 machine, there's nothing to "disable"
// 	if machines <= 1 {
// 		return []string{}
// 	}

// 	// A feature is "disabled" if it doesn't appear on every machine
// 	disable := make([]string, 0)
// 	for f, c := range featureCount {
// 		if c < machines {
// 			disable = append(disable, f)
// 		}
// 	}

// 	sort.Strings(disable)
// 	return disable
// }

func getCommonFeatures(all [][]string) []string {
	if len(all) == 0 {
		return nil
	}
	count := map[string]int{}
	m := 0
	for _, feats := range all {
		if len(feats) == 0 {
			continue
		}
		m++
		seen := map[string]struct{}{}
		for _, f := range feats {
			if _, ok := seen[f]; ok {
				continue
			}
			seen[f] = struct{}{}
		}
		for f := range seen {
			count[f]++
		}
	}
	if m == 0 {
		return nil
	}
	var common []string
	for f, c := range count {
		if c == m {
			common = append(common, f)
		}
	}
	sort.Strings(common)
	return common
}

func diff(from, to []string) []string { // from \ to
	toSet := make(map[string]struct{}, len(to))
	for _, f := range to {
		toSet[f] = struct{}{}
	}
	seen := map[string]struct{}{}
	var out []string
	for _, f := range from {
		if _, dup := seen[f]; dup {
			continue
		}
		seen[f] = struct{}{}
		if _, ok := toSet[f]; !ok {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func (v *VirshService) GetCpuDisableFeatures() ([]string, error) {
	var features [][]string
	for _, conn := range protocol.GetAllGRPCConnections() {
		features_conn := virsh.GetCpuFeatures(conn)
		features = append(features, features_conn)
	}
	return getCommonFeatures(features), nil
}

// vmReq.MachineName, vmReq.Name, vmReq.Memory, vmReq.Vcpu, vmReq.NfsShareId, vmReq.DiskSizeGB, vmReq.IsoID, vmReq.Network, vmReq.VNCPassword
func (v *VirshService) CreateVM(machine_name string, name string, memory int32, vcpu int32, nfsShareId int, diskSizeGB int32, isoID int, network string, VNCPassword string) error {

	//get all vms cant have same name
	//cant have two vms with the same name
	exists, err := virsh.DoesVMExist(name)
	if err != nil {
		return fmt.Errorf("error checking if VM exists: %v", err)
	}
	if exists {
		return fmt.Errorf("a VM with the name %s already exists", name)
	}

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
		// mnt/ nfs / vmname / vmname.qcow2
		qcowFile = nfsShare.Target + "/" + name + "/" + name + ".qcow2"
	} else {
		// mnt/ nfs / vmname / vmname.qcow2
		qcowFile = nfsShare.Target + name + "/" + name + ".qcow2"
	}

	var diskFolder string
	if nfsShare.Target[len(nfsShare.Target)-1] != '/' {
		// mnt/ nfs / vmname / vmname.qcow2
		diskFolder = nfsShare.Target + "/" + name
	} else {
		// mnt/ nfs / vmname / vmname.qcow2
		diskFolder = nfsShare.Target + name
	}

	return virsh.CreateVM(slaveMachine.Connection, name, memory, vcpu, diskFolder, qcowFile, diskSizeGB, isoPath, network, VNCPassword)
}

func (v *VirshService) DeleteVM(name string) error {
	//find vm by name
	exists, err := virsh.DoesVMExist(name)
	if err != nil {
		return fmt.Errorf("error checking if VM exists: %v", err)
	}
	if !exists {
		return fmt.Errorf("a VM with the name %s does not exist", name)
	}

	con := protocol.GetAllGRPCConnections()
	for _, conn := range con {
		vm, err := virsh.GetVmByName(conn, &grpcVirsh.GetVmByNameRequest{Name: name})
		if err == nil && vm != nil {
			//found the vm
			err = virsh.RemoveVM(conn, vm)
			if err != nil {
				return fmt.Errorf("failed to delete VM %s: %v", name, err)
			}
			return nil
		}
	}
	return fmt.Errorf("failed to find VM %s on any machine", name)
}

func (v *VirshService) StartVM(name string) error {
	//find vm by name
	exists, err := virsh.DoesVMExist(name)
	if err != nil {
		return fmt.Errorf("error checking if VM exists: %v", err)
	}
	if !exists {
		return fmt.Errorf("a VM with the name %s does not exist", name)
	}

	con := protocol.GetAllGRPCConnections()
	for _, conn := range con {
		vm, err := virsh.GetVmByName(conn, &grpcVirsh.GetVmByNameRequest{Name: name})
		if err == nil && vm != nil {
			//found the vm
			err = virsh.StartVm(conn, vm)
			if err != nil {
				return fmt.Errorf("failed to start VM %s: %v", name, err)
			}
			return nil
		}
	}
	return fmt.Errorf("failed to find VM %s on any machine", name)
}

func (v *VirshService) ShutdownVM(name string) error {
	//find vm by name
	exists, err := virsh.DoesVMExist(name)
	if err != nil {
		return fmt.Errorf("error checking if VM exists: %v", err)
	}
	if !exists {
		return fmt.Errorf("a VM with the name %s does not exist", name)
	}

	con := protocol.GetAllGRPCConnections()
	for _, conn := range con {
		vm, err := virsh.GetVmByName(conn, &grpcVirsh.GetVmByNameRequest{Name: name})
		if err == nil && vm != nil {
			//found the vm
			err = virsh.ShutdownVM(conn, vm)
			if err != nil {
				return fmt.Errorf("failed to stop VM %s: %v", name, err)
			}
			return nil
		}
	}
	return fmt.Errorf("failed to find VM %s on any machine", name)
}

func (v *VirshService) ForceShutdownVM(name string) error {
	//find vm by name
	exists, err := virsh.DoesVMExist(name)
	if err != nil {
		return fmt.Errorf("error checking if VM exists: %v", err)
	}
	if !exists {
		return fmt.Errorf("a VM with the name %s does not exist", name)
	}

	con := protocol.GetAllGRPCConnections()
	for _, conn := range con {
		vm, err := virsh.GetVmByName(conn, &grpcVirsh.GetVmByNameRequest{Name: name})
		if err == nil && vm != nil {
			//found the vm
			err = virsh.ForceShutdownVM(conn, vm)
			if err != nil {
				return fmt.Errorf("failed to force stop VM %s: %v", name, err)
			}
			return nil
		}
	}
	return fmt.Errorf("failed to find VM %s on any machine", name)
}

func (v *VirshService) RestartVM(name string) error {
	//find vm by name
	exists, err := virsh.DoesVMExist(name)
	if err != nil {
		return fmt.Errorf("error checking if VM exists: %v", err)
	}
	if !exists {
		return fmt.Errorf("a VM with the name %s does not exist", name)
	}

	con := protocol.GetAllGRPCConnections()
	for _, conn := range con {
		vm, err := virsh.GetVmByName(conn, &grpcVirsh.GetVmByNameRequest{Name: name})
		if err == nil && vm != nil {
			//found the vm
			err = virsh.RestartVM(conn, vm)
			if err != nil {
				return fmt.Errorf("failed to restart VM %s: %v", name, err)
			}
			return nil
		}
	}
	return fmt.Errorf("failed to find VM %s on any machine", name)
}

func (v *VirshService) GetVmByName(name string) (*grpcVirsh.Vm, error) {
	//find vm by name
	exists, err := virsh.DoesVMExist(name)
	if err != nil {
		return nil, fmt.Errorf("error checking if VM exists: %v", err)
	}
	if !exists {
		return nil, nil
	}

	con := protocol.GetAllGRPCConnections()
	for _, conn := range con {
		vm, err := virsh.GetVmByName(conn, &grpcVirsh.GetVmByNameRequest{Name: name})
		if err == nil && vm != nil {
			//found the vm
			return vm, nil
		}
	}
	return nil, fmt.Errorf("failed to find VM %s on any machine", name)
}

func (v *VirshService) GetAllVms() ([]*grpcVirsh.Vm, error) {
	var allVms []*grpcVirsh.Vm
	con := protocol.GetAllGRPCConnections()
	for _, conn := range con {
		vms, err := virsh.GetAllVms(conn, &grpcVirsh.Empty{})
		if err != nil {
			return nil, fmt.Errorf("failed to get VMs from a machine: %v", err)
		}
		allVms = append(allVms, vms.Vms...)
	}
	return allVms, nil
}
func (v *VirshService) EditVM(name string, cpuCount, memory int, diskSizeGB int) error {
	//find vm by name
	exists, err := virsh.DoesVMExist(name)
	if err != nil {
		return fmt.Errorf("error checking if VM exists: %v", err)
	}
	if !exists {
		return fmt.Errorf("a VM with the name %s does not exist", name)
	}

	con := protocol.GetAllGRPCConnections()
	for _, conn := range con {
		vm, err := virsh.GetVmByName(conn, &grpcVirsh.GetVmByNameRequest{Name: name})
		if cpuCount > 0 {
			vm.CpuCount = int32(cpuCount)
		}
		if memory > 0 {
			vm.MemoryMB = int32(memory)
		}
		if diskSizeGB > 0 {
			vm.DiskSizeGB = int32(diskSizeGB)
		}
		if err == nil && vm != nil {
			//found the vm
			err = virsh.EditVm(conn, vm)
			if err != nil {
				return fmt.Errorf("failed to edit VM %s: %v", name, err)
			}
			return nil
		}
	}
	return fmt.Errorf("failed to find VM %s on any machine", name)
}
