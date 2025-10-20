package services

import (
	"512SvMan/db"
	"512SvMan/protocol"
	"512SvMan/virsh"
	"context"
	"fmt"
	"sort"
	"strings"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"libvirt.org/go/libvirt"
)

type VirshService struct {
}

func ClusterSafeFeatures(all [][]string) []string {
	if len(all) == 0 {
		return nil
	}
	base := uniqueSorted(all[0])

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

func ComputeBaseline(xmls []string) (string, error) {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	flags := libvirt.CONNECT_BASELINE_CPU_MIGRATABLE
	baselineXML, err := conn.BaselineCPU(xmls, flags)
	if err != nil {
		return "", err
	}
	return baselineXML, nil
}

func (v *VirshService) GetCpuDisableFeatures(conns []string) (string, error) {
	// var features [][]string
	// for _, conn := range protocol.GetAllGRPCConnections() {
	// 	features_conn := virsh.GetCpuFeatures(conn)
	// 	features = append(features, features_conn)
	// 	fmt.Println(features)
	// }
	// return ClusterDisableList(features), nil

	var xmls []string
	for _, machineName := range conns {
		conn := protocol.GetConnectionByMachineName(machineName)
		if conn == nil {
			return "", fmt.Errorf("machine %s not found", machineName)
		}
		cpuXML, err := virsh.GetCPUXML(conn.Connection)
		if err != nil {
			return "", err
		}
		xmls = append(xmls, cpuXML)
	}
	baselineXML, err := ComputeBaseline(xmls)
	if err != nil {
		return "", err
	}

	return baselineXML, nil
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

func (v *VirshService) CreateLiveVM(machine_name string, name string, memory int32, vcpu int32, nfsShareId int, diskSizeGB int32, isoID int, network string, VNCPassword string, cpuXml string) error {
	exists, err := db.DoesVmLiveExist(name)
	if err != nil {
		return fmt.Errorf("failed to check if live VM exists in database: %v", err)
	}
	if exists {
		return fmt.Errorf("a live VM with the name %s already exists in the database", name)
	}

	//get all vms cant have same name
	//cant have two vms with the same name
	exists, err = virsh.DoesVMExist(name)
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

	err = virsh.CreateLiveVM(slaveMachine.Connection, name, memory, vcpu, diskFolder, qcowFile, diskSizeGB, isoPath, network, VNCPassword, cpuXml)
	if err != nil {
		return err
	}

	//add to db
	err = db.AddVmLive(name)
	if err != nil {
		return fmt.Errorf("failed to add live VM to database: %v", err)
	}
	return nil
}

func (v *VirshService) MigrateVm(ctx context.Context, originMachine string, destMachine string, vmName string, live bool) error {
	exists, err := db.DoesVmLiveExist(vmName)
	if err != nil {
		return fmt.Errorf("failed to check if live VM exists in database: %v", err)
	}
	if !exists {
		return fmt.Errorf("a live VM with the name %s does not exist in the database", vmName)
	}

	if originMachine == destMachine {
		return fmt.Errorf("origin and destination machines cannot be the same")
	}

	//Get Connections
	originConn := protocol.GetConnectionByMachineName(originMachine)
	if originConn == nil {
		return fmt.Errorf("origin machine %s not found", originMachine)
	}

	destConn := protocol.GetConnectionByMachineName(destMachine)
	if destConn == nil {
		return fmt.Errorf("destination machine %s not found", destMachine)
	}

	//Check Vms existance and Get vm
	exists, err = virsh.DoesVMExist(vmName)
	if err != nil {
		return fmt.Errorf("error checking if VM exists: %v", err)
	}
	if !exists {
		return fmt.Errorf("a VM with the name %s does not exist", vmName)
	}

	//check if vm is running on origin machine
	vm, err := virsh.GetVmByName(originConn.Connection, &grpcVirsh.GetVmByNameRequest{Name: vmName})
	if err != nil || vm == nil {
		return fmt.Errorf("VM %s not found on origin machine %s", vmName, originMachine)
	}

	//check if vm is running on origin machine
	if vm.MachineName != originMachine {
		return fmt.Errorf("VM %s is not running on origin machine %s", vmName, originMachine)
	}

	return virsh.MigrateVm(ctx, originConn.Connection, vmName, destConn.Addr, live)
}

func (v *VirshService) UpdateCpuXml(machine_name string, vmName string, cpuXml string) error {
	slaveMachine := protocol.GetConnectionByMachineName(machine_name)
	if slaveMachine == nil {
		return fmt.Errorf("machine %s not found", machine_name)
	}

	//it needs to be live vm
	exists, err := db.DoesVmLiveExist(vmName)
	if err != nil {
		return fmt.Errorf("failed to check if live VM exists in database: %v", err)
	}
	if !exists {
		return fmt.Errorf("a live VM with the name %s does not exist in the database", vmName)
	}

	//get vm by name
	vm, err := virsh.GetVmByName(slaveMachine.Connection, &grpcVirsh.GetVmByNameRequest{Name: vmName})
	if err != nil {
		return fmt.Errorf("failed to get VM by name: %v", err)
	}
	if vm == nil {
		return fmt.Errorf("VM %s not found on machine %s", vmName, machine_name)
	}

	err = virsh.UpdateVMCPUXml(slaveMachine.Connection, vmName, cpuXml)
	if err != nil {
		return fmt.Errorf("failed to update VM CPU XML: %v", err)
	}

	return nil
}

func (v *VirshService) GetCpuXML(machine_name string, vmName string) (string, error) {
	slaveMachine := protocol.GetConnectionByMachineName(machine_name)
	if slaveMachine == nil {
		return "", fmt.Errorf("machine %s not found", machine_name)
	}

	//get vm by name
	vm, err := virsh.GetVmByName(slaveMachine.Connection, &grpcVirsh.GetVmByNameRequest{Name: vmName})
	if err != nil {
		return "", fmt.Errorf("failed to get VM by name: %v", err)
	}
	if vm == nil {
		return "", fmt.Errorf("VM %s not found on machine %s", vmName, machine_name)
	}

	cpuXml, err := virsh.GetVMCPUXml(slaveMachine.Connection, vmName)
	if err != nil {
		return "", fmt.Errorf("failed to get VM CPU XML: %v", err)
	}

	return cpuXml, nil
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

			//remove from db if live vm
			exists, err := db.DoesVmLiveExist(name)
			if err != nil {
				return fmt.Errorf("failed to check if live VM exists in database: %v", err)
			}
			if exists {
				err = db.RemoveVmLive(name)
				if err != nil {
					return fmt.Errorf("failed to remove live VM from database: %v", err)
				}
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

type VmType struct {
	*grpcVirsh.Vm
	IsLive bool
}

func (v *VirshService) GetAllVms() ([]VmType, []error) {
	var allVms []VmType
	var errors []error

	con := protocol.GetAllGRPCConnections()
	for _, conn := range con {
		vms, err := virsh.GetAllVms(conn, &grpcVirsh.Empty{})
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to get VMs from a machine: %v", err))
		}
		if vms == nil {
			continue
		}
		for _, vm := range vms.Vms {
			isLive, err := db.DoesVmLiveExist(vm.Name)
			if err != nil {
				errors = append(errors, fmt.Errorf("failed to check if live VM exists in database: %v", err))
				continue
			}
			//if name already in allVms skip
			found := false
			for _, v := range allVms {
				if v.Name == vm.Name {
					found = true
					break
				}
			}
			if found {
				continue
			}
			allVms = append(allVms, VmType{Vm: vm, IsLive: isLive})
		}

	}
	return allVms, errors
}

// nfsSharePathTarget -> /mnt/...
func (v *VirshService) GetAllVmsByOnNfsShare(nfsSharePathTarget string) ([]VmType, error) {
	allVms, errors := v.GetAllVms()
	if len(errors) > 0 {
		return nil, fmt.Errorf("failed to get all VMs: %v", errors)
	}
	var vmsOnShare []VmType
	//if vm include in nfsShareId
	for _, vm := range allVms {
		if strings.Contains(vm.DiskPath, nfsSharePathTarget) {
			vmsOnShare = append(vmsOnShare, vm)
		}
	}
	return vmsOnShare, nil
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

func (v *VirshService) RemoveIso(vmName string) error {
	//find vm by name
	exists, err := virsh.DoesVMExist(vmName)
	if err != nil {
		return fmt.Errorf("error checking if VM exists: %v", err)
	}
	if !exists {
		return fmt.Errorf("a VM with the name %s does not exist", vmName)
	}

	con := protocol.GetAllGRPCConnections()
	for _, conn := range con {
		vm, err := virsh.GetVmByName(conn, &grpcVirsh.GetVmByNameRequest{Name: vmName})
		if err == nil && vm != nil {
			//found the vm
			err = virsh.RemoveIso(conn, vm)
			if err != nil {
				return fmt.Errorf("failed to remove ISO from VM %s: %v", vmName, err)
			}
			return nil
		}
	}
	return fmt.Errorf("failed to find VM %s on any machine", vmName)
}

func (v *VirshService) PauseVM(name string) error {
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
			err = virsh.PauseVM(conn, vm)
			if err != nil {
				return fmt.Errorf("failed to pause VM %s: %v", name, err)
			}
			return nil
		}
	}
	return fmt.Errorf("failed to find VM %s on any machine", name)
}

func (v *VirshService) ResumeVM(name string) error {
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
			err = virsh.ResumeVM(conn, vm)
			if err != nil {
				return fmt.Errorf("failed to resume VM %s: %v", name, err)
			}
			return nil
		}
	}
	return fmt.Errorf("failed to find VM %s on any machine", name)
}
