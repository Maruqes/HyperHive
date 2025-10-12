package services

import (
	"512SvMan/db"
	"512SvMan/protocol"
	"512SvMan/virsh"
	"fmt"
	"sort"
	"strings"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
)

type VirshService struct {
}

func ClusterSafeFeatures(all [][]string) []string {
	if len(all) == 0 {
		return nil
	}
	// Start with unique+sorted features from the first host.
	base := uniqueSorted(all[0])

	// Fold the remaining hosts using a comm-like merge to keep only the common items.
	for i := 1; i < len(all); i++ {
		cur := uniqueSorted(all[i])
		_, _, common := commLike(base, cur) // keep the intersection
		base = common
		if len(base) == 0 {
			break // early exit if nothing is common anymore
		}
	}
	return base
}

// ----- helpers -----

// uniqueSorted trims, dedups, and returns a sorted copy of the input slice.
func uniqueSorted(in []string) []string {
	set := make(map[string]struct{}, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		set[s] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// commLike emulates the core of `comm` for two sorted, deduped slices.
// It returns (onlyA, onlyB, common).
func commLike(a, b []string) ([]string, []string, []string) {
	i, j := 0, 0
	var onlyA, onlyB, common []string
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			common = append(common, a[i])
			i++
			j++
			continue
		}
		if a[i] < b[j] {
			onlyA = append(onlyA, a[i])
			i++
		} else {
			onlyB = append(onlyB, b[j])
			j++
		}
	}
	for ; i < len(a); i++ {
		onlyA = append(onlyA, a[i])
	}
	for ; j < len(b); j++ {
		onlyB = append(onlyB, b[j])
	}
	return onlyA, onlyB, common
}

// ClusterDisableList returns all features that are NOT common to every host.
// Disabling these on the VM makes it migratable across the whole cluster.
func ClusterDisableList(all [][]string) []string {
	base := ClusterSafeFeatures(all)
	baseSet := make(map[string]struct{}, len(base))
	for _, f := range base {
		baseSet[f] = struct{}{}
	}
	union := make(map[string]struct{})
	for _, feats := range all {
		for _, f := range feats {
			f = strings.TrimSpace(f)
			if f != "" {
				union[f] = struct{}{}
			}
		}
	}
	var disable []string
	for f := range union {
		if _, ok := baseSet[f]; !ok {
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
		fmt.Println(features)
	}
	return ClusterDisableList(features), nil
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
