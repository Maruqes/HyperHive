package services

import (
	"512SvMan/db"
	"512SvMan/env512"
	"512SvMan/extra"
	"512SvMan/nfs"
	"512SvMan/nots"
	"512SvMan/protocol"
	"512SvMan/virsh"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	protoExtra "github.com/Maruqes/512SvMan/api/proto/extra"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Maruqes/512SvMan/logger"
	"libvirt.org/go/libvirt"
)

type VirshService struct {
	backupLoopRunning atomic.Bool
}

const longTaskTimeout = 7 * 24 * time.Hour

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
func (v *VirshService) CreateVM(ctx context.Context, machine_name string, name string, memory int32, vcpu int32, nfsShareId int, diskSizeGB int32, isoID int, network string, VNCPassword string, cpuXML string, autoStart bool, isWindows bool) error {

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
	nfsShare, err := db.GetNFSShareByID(ctx, nfsShareId)
	if err != nil {
		return fmt.Errorf("failed to get NFS share by ID: %v", err)
	}
	if nfsShare == nil {
		return fmt.Errorf("NFS share with ID %d not found", nfsShareId)
	}

	//get iso path from isoID
	iso, err := db.GetIsoByID(ctx, isoID)
	if err != nil {
		return fmt.Errorf("failed to get ISO by ID: %v", err)
	}
	if iso == nil {
		return fmt.Errorf("ISO with ID %d not found", isoID)
	}
	isoPath := iso.FilePath

	var qcowFile string
	fileExtension := ".qcow2"

	if nfsShare.Target[len(nfsShare.Target)-1] != '/' {
		// mnt/ nfs / vmname / vmname.extension
		qcowFile = nfsShare.Target + "/" + name + "/" + name + fileExtension
	} else {
		qcowFile = nfsShare.Target + name + "/" + name + fileExtension
	}

	var diskFolder string
	if nfsShare.Target[len(nfsShare.Target)-1] != '/' {
		// mnt/ nfs / vmname
		diskFolder = nfsShare.Target + "/" + name
	} else {
		diskFolder = nfsShare.Target + name
	}

	if err := virsh.CreateVM(slaveMachine.Connection, name, memory, vcpu, diskFolder, qcowFile, diskSizeGB, isoPath, network, VNCPassword, cpuXML, autoStart, isWindows); err != nil {
		return err
	}

	if err := v.AutoStart(ctx, name, autoStart); err != nil {
		return err
	}
	return nil
}

func (v *VirshService) CreateLiveVM(ctx context.Context, machine_name string, name string, memory int32, vcpu int32, nfsShareId int, diskSizeGB int32, isoID int, network string, VNCPassword string, cpuXml string, autoStart bool, isWindows bool) error {
	exists, err := db.DoesVmLiveExist(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to check if live VM exists in database: %v", err)
	}
	if exists {
		return fmt.Errorf("a live VM with the name %s already exists in the database", name)
	}

	//get disk path from nfsShareId
	nfsShare, err := db.GetNFSShareByID(ctx, nfsShareId)
	if err != nil {
		return fmt.Errorf("failed to get NFS share by ID: %v", err)
	}
	if nfsShare == nil {
		return fmt.Errorf("NFS share with ID %d not found", nfsShareId)
	}

	if nfsShare.HostNormalMount {
		return fmt.Errorf("cant have live VM on a HostNormalMount NFS true, use a nfs where HostNormalMount is false")
	}

	err = v.CreateVM(ctx, machine_name, name, memory, vcpu, nfsShareId, diskSizeGB, isoID, network, VNCPassword, cpuXml, autoStart, isWindows)
	if err != nil {
		return err
	}

	//add to db
	err = db.AddVmLive(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to add live VM to database: %v", err)
	}
	return nil
}

func (v *VirshService) ColdMigrateVm(ctx context.Context, slaveName string, machine *grpcVirsh.ColdMigrationRequest) error {
	originConn := protocol.GetConnectionByMachineName(slaveName)
	if originConn == nil {
		return fmt.Errorf("origin machine %s not found", slaveName)
	}

	// chmod 777 and give qemu ownership with machine.DiskPath
	qemuUID, err := strconv.Atoi(env512.Qemu_UID)
	if err != nil {
		return fmt.Errorf("invalid qemu uid %s: %v", env512.Qemu_UID, err)
	}

	qemuGID, err := strconv.Atoi(env512.Qemu_GID)
	if err != nil {
		return fmt.Errorf("invalid qemu gid %s: %v", env512.Qemu_GID, err)
	}

	if err := os.Chown(machine.DiskPath, qemuUID, qemuGID); err != nil {
		return fmt.Errorf("failed to set qemu ownership on %s: %v", machine.DiskPath, err)
	}

	if err := os.Chmod(machine.DiskPath, 0o777); err != nil {
		return fmt.Errorf("failed to chmod %s: %v", machine.DiskPath, err)
	}

	//flush before to make sure everything is on disk
	err = nfs.Sync(originConn.Connection)
	if err != nil {
		return err
	}

	err = virsh.ColdMigrateVm(ctx, originConn.Connection, machine)
	if err != nil {
		return err
	}

	if machine.Live {
		db.AddVmLive(ctx, machine.VmName)
	}

	//flush after also for redundancy
	nfs.Sync(originConn.Connection)

	return nil
}

func (v *VirshService) MigrateVm(ctx context.Context, originMachine string, destMachine string, vmName string, live bool, timeoutSeconds int) error {
	logErr := func(e error) error {
		logger.Error(e.Error())
		return e
	}

	exists, err := db.DoesVmLiveExist(ctx, vmName)
	if err != nil {
		return logErr(fmt.Errorf("failed to check if live VM exists in database: %v", err))
	}
	if !exists {
		return logErr(fmt.Errorf("a live VM with the name %s does not exist in the database", vmName))
	}

	if originMachine == destMachine {
		return logErr(fmt.Errorf("origin and destination machines cannot be the same"))
	}

	//Get Connections
	originConn := protocol.GetConnectionByMachineName(originMachine)
	if originConn == nil {
		return logErr(fmt.Errorf("origin machine %s not found", originMachine))
	}

	destConn := protocol.GetConnectionByMachineName(destMachine)
	if destConn == nil {
		return logErr(fmt.Errorf("destination machine %s not found", destMachine))
	}

	//Check Vms existance and Get vm
	exists, err = virsh.DoesVMExist(vmName)
	if err != nil {
		return logErr(fmt.Errorf("error checking if VM exists: %v", err))
	}
	if !exists {
		return logErr(fmt.Errorf("a VM with the name %s does not exist", vmName))
	}

	//check if vm is running on origin machine
	vm, err := virsh.GetVmByName(originConn.Connection, &grpcVirsh.GetVmByNameRequest{Name: vmName})
	if err != nil || vm == nil {
		return logErr(fmt.Errorf("VM %s not found on origin machine %s", vmName, originMachine))
	}

	//check if vm is running on origin machine
	if vm.MachineName != originMachine {
		return logErr(fmt.Errorf("VM %s is not running on origin machine %s", vmName, originMachine))
	}

	go func() {
		ctxTimeout, cancel := context.WithTimeout(context.Background(), longTaskTimeout)
		defer cancel()
		err := virsh.MigrateVm(ctxTimeout, originConn.Connection, vmName, destConn.Addr, live, timeoutSeconds)
		if err != nil {
			logger.Errorf("%v", err)
			logger.Error(err.Error())
			extra.SendWebsocketMessage(protoExtra.WebSocketsMessageType_Error, fmt.Sprintf("MigrateVm failed for %s: %v", vmName, err), vmName)
			sendImportantNotification("MigrateVm failed", err)
		}
	}()

	return nil
}

func (v *VirshService) UpdateCpuXml(ctx context.Context, machine_name string, vmName string, cpuXml string) error {
	slaveMachine := protocol.GetConnectionByMachineName(machine_name)
	if slaveMachine == nil {
		return fmt.Errorf("machine %s not found", machine_name)
	}

	//it needs to be live vm
	exists, err := db.DoesVmLiveExist(ctx, vmName)
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

func (v *VirshService) DeleteVM(ctx context.Context, name string) error {
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
			exists, err := db.DoesVmLiveExist(ctx, name)
			if err != nil {
				return fmt.Errorf("failed to check if live VM exists in database: %v", err)
			}
			if exists {
				err = db.RemoveVmLive(ctx, name)
				if err != nil {
					return fmt.Errorf("failed to remove live VM from database: %v", err)
				}
			}

			// remove automatic backup schedule for this VM, if present
			if err := db.RemoveAutomaticBackup(ctx, name); err != nil {
				return fmt.Errorf("failed to remove automatic backup for VM %s: %v", name, err)
			}

			// remove autostart entry for this VM, if present
			if err := db.RemoveAutoStart(ctx, name); err != nil {
				return fmt.Errorf("failed to remove autostart for VM %s: %v", name, err)
			}

			return nil
		}
	}
	return fmt.Errorf("failed to find VM %s on any machine", name)
}

func (v *VirshService) StartVM(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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
		if err := ctx.Err(); err != nil {
			return err
		}
		vm, err := virsh.GetVmByName(conn, &grpcVirsh.GetVmByNameRequest{Name: name})
		if err == nil && vm != nil {
			//found the vm
			err = virsh.StartVm(ctx, conn, vm)
			if err == nil {
				return nil
			}
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

func (v *VirshService) GetAllVms(ctx context.Context) ([]VmType, []string, error) {
	var allVms []VmType
	var warningErrors []string

	var erros []error

	var wg sync.WaitGroup
	var mu sync.Mutex

	addToAllVms := func(conn *grpc.ClientConn) {
		defer wg.Done() //settar finishado no waitgroup meus caros

		vms, err := virsh.GetAllVms(conn, &grpcVirsh.Empty{})
		if err != nil {
			mu.Lock()
			erros = append(erros, fmt.Errorf("failed to get VMs from a machine: %v", err))
			mu.Unlock()
			return
		}

		if len(vms.Warnings) > 0 {
			mu.Lock()
			for _, warning := range vms.Warnings {
				logger.Warn(warning)
				warningErrors = append(warningErrors, warning)
			}
			mu.Unlock()
		}

		for _, vm := range vms.Vms {
			isLive, err := db.DoesVmLiveExist(ctx, vm.Name)
			if err != nil {
				mu.Lock()
				erros = append(erros, fmt.Errorf("failed to check if live VM exists in database: %v", err))
				mu.Unlock()
			}

			//if name already in allVms skip (check under mutex)
			mu.Lock()
			found := false
			for _, v := range allVms {
				if v.Name == vm.Name {
					found = true
					break
				}
			}
			if found {
				logger.Error("DOUBLE VM IN GetALLVMS")
				nots.SendGlobalNotification("DOUBLE VM IN GetALLVMS", "if there is a duplicate VM delete one of them manually with CLI virsh", "", true)
			}
			allVms = append(allVms, VmType{Vm: vm, IsLive: isLive})
			mu.Unlock()
		}
	}

	con := protocol.GetAllGRPCConnections()
	for _, conn := range con {
		wg.Add(1)
		go addToAllVms(conn)
	}
	wg.Wait()

	if len(erros) > 0 {
		return allVms, warningErrors, fmt.Errorf("encountered %d errors; first: %v", len(erros), erros[0])
	}
	return allVms, warningErrors, nil
}

// nfsSharePathTarget -> /mnt/...
func (v *VirshService) GetAllVmsByOnNfsShare(ctx context.Context, nfsSharePathTarget string) ([]VmType, error) {
	allVms, _, err := v.GetAllVms(ctx)
	if err != nil {
		return nil, err
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

func (v *VirshService) GetNfsByVM(ctx context.Context, vm *grpcVirsh.Vm) (int, error) {
	if vm == nil {
		return 0, fmt.Errorf("vm not found problem in GetNfsByVM")
	}

	diskPath := strings.TrimSpace(vm.DiskPath)
	if diskPath == "" {
		return 0, fmt.Errorf("vm %s has no disk path", vm.Name)
	}
	diskPath = filepath.Clean(diskPath)

	shares, err := db.GetAllNFShares(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get NFS shares: %v", err)
	}

	cleanPath := func(p string) string {
		p = strings.TrimSpace(p)
		if p == "" {
			return ""
		}
		return filepath.Clean(p)
	}

	isPathWithin := func(path, base string) bool {
		if base == "" {
			return false
		}
		if base == "/" {
			return strings.HasPrefix(path, "/")
		}
		if !strings.HasPrefix(path, base) {
			return false
		}
		if len(path) == len(base) {
			return true
		}
		return path[len(base)] == '/'
	}

	var (
		matchedID  int
		longestLen int
		found      bool
	)

	for _, share := range shares {
		target := cleanPath(share.Target)
		folderPath := cleanPath(share.FolderPath)

		bestLen := 0
		matched := false

		if isPathWithin(diskPath, target) {
			bestLen = len(target)
			matched = true
		}
		if isPathWithin(diskPath, folderPath) && len(folderPath) > bestLen {
			bestLen = len(folderPath)
			matched = true
		}

		if !matched {
			continue
		}

		if !found || bestLen > longestLen {
			matchedID = share.Id
			longestLen = bestLen
			found = true
		}
	}

	if !found {
		return 0, fmt.Errorf("failed to resolve NFS share for VM %s", vm.Name)
	}

	return matchedID, nil
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
		if err != nil || vm == nil {
			continue
		}
		if cpuCount > 0 {
			vm.CpuCount = int32(cpuCount)
		}
		if memory > 0 {
			vm.MemoryMB = int32(memory)
		}
		if diskSizeGB > 0 {
			vm.DiskSizeGB = int32(diskSizeGB)
		}
		// found the vm
		err = virsh.EditVm(conn, vm)
		if err != nil {
			return fmt.Errorf("failed to edit VM %s: %v", name, err)
		}
		return nil
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

func (v *VirshService) ChangeNetwork(vmName string, newNetwork string) error {
	if newNetwork != "default" && newNetwork != "512rede" {
		return fmt.Errorf("network must be either 'default' or '512rede', got '%s'", newNetwork)
	}

	vm, err := v.GetVmByName(vmName)
	if err != nil {
		return err
	}
	if vm == nil {
		return fmt.Errorf("vm %s does not exist", vmName)
	}

	slave := protocol.GetConnectionByMachineName(vm.MachineName)
	if slave == nil || slave.Connection == nil {
		return fmt.Errorf("slave %s no connected", vm.MachineName)
	}

	err = virsh.ChangeNetwork(slave.Connection, &grpcVirsh.ChangeNetworkReq{VmName: vmName, NewNetwork: newNetwork})
	return err
}

func (v *VirshService) ChangeVncPassword(vmName string, newPassword string) error {
	if strings.TrimSpace(newPassword) == "" {
		return fmt.Errorf("new password cannot be empty")
	}

	vm, err := v.GetVmByName(vmName)
	if err != nil {
		return err
	}
	if vm == nil {
		return fmt.Errorf("vm %s does not exist", vmName)
	}

	slave := protocol.GetConnectionByMachineName(vm.MachineName)
	if slave == nil || slave.Connection == nil {
		return fmt.Errorf("slave %s no connected", vm.MachineName)
	}

	req := &grpcVirsh.ChangeVncPassword{
		VmName:      vmName,
		NewPassword: newPassword,
	}
	return virsh.ChangeVncPassword(slave.Connection, req)
}
func (v *VirshService) AddNoVNCVideo(vmName string) error {
	vm, err := v.GetVmByName(vmName)
	if err != nil {
		return err
	}
	if vm == nil {
		return fmt.Errorf("vm %s does not exist", vmName)
	}

	if vm.State != grpcVirsh.VmState_SHUTOFF {
		return fmt.Errorf("vm %s needs to be shutdown", vmName)
	}

	slave := protocol.GetConnectionByMachineName(vm.MachineName)
	if slave == nil || slave.Connection == nil {
		return fmt.Errorf("slave %s no connected", vm.MachineName)
	}

	return virsh.AddNoVNCVideo(slave.Connection, vmName)
}

func (v *VirshService) RemoveNoVNCVideo(vmName string) error {
	vm, err := v.GetVmByName(vmName)
	if err != nil {
		return err
	}
	if vm == nil {
		return fmt.Errorf("vm %s does not exist", vmName)
	}

	if vm.State != grpcVirsh.VmState_SHUTOFF {
		return fmt.Errorf("vm %s needs to be shutdown", vmName)
	}

	slave := protocol.GetConnectionByMachineName(vm.MachineName)
	if slave == nil || slave.Connection == nil {
		return fmt.Errorf("slave %s no connected", vm.MachineName)
	}

	return virsh.RemoveNoVNCVideo(slave.Connection, vmName)
}

func (v *VirshService) GetNoVNCVideo(vmName string) (*grpcVirsh.GetNoVNCVideoResponse, error) {
	vm, err := v.GetVmByName(vmName)
	if err != nil {
		return nil, err
	}
	if vm == nil {
		return nil, fmt.Errorf("vm %s does not exist", vmName)
	}

	slave := protocol.GetConnectionByMachineName(vm.MachineName)
	if slave == nil || slave.Connection == nil {
		return nil, fmt.Errorf("slave %s no connected", vm.MachineName)
	}

	return virsh.GetNoVNCVideo(slave.Connection, vmName)
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

func (v *VirshService) AutoStart(ctx context.Context, vmName string, autoStart bool) error {
	vm, err := v.GetVmByName(vmName)
	if err != nil {
		return err
	}

	if vm == nil {
		return fmt.Errorf("failed to get vm %s", vmName)
	}

	if autoStart {
		// Check if VM already exists in auto_start table
		exists, err := db.DoesAutoStartExist(ctx, vmName)
		if err != nil {
			return fmt.Errorf("failed to check if VM exists in auto_start: %v", err)
		}

		// Only add if it doesn't exist
		if !exists {
			err = db.AddAutoStart(ctx, vmName)
			if err != nil {
				return fmt.Errorf("failed to add VM to auto_start: %v", err)
			}
		}
	} else {
		// Check if VM exists in auto_start table before removing
		exists, err := db.DoesAutoStartExist(ctx, vmName)
		if err != nil {
			return fmt.Errorf("failed to check if VM exists in auto_start: %v", err)
		}

		// Only remove if it exists
		if exists {
			err = db.RemoveAutoStart(ctx, vmName)
			if err != nil {
				return fmt.Errorf("failed to remove VM from auto_start: %v", err)
			}
		}
	}

	return nil
}

func (v *VirshService) SetVmLive(ctx context.Context, vmName string, enable bool) error {
	vm, err := v.GetVmByName(vmName)
	if err != nil {
		return err
	}

	if vm == nil {
		return fmt.Errorf("failed to get vm %s", vmName)
	}

	exists, err := db.DoesVmLiveExist(ctx, vmName)
	if err != nil {
		return fmt.Errorf("failed to check if VM exists in vm_live: %v", err)
	}

	if enable {
		if !exists {
			if err := db.AddVmLive(ctx, vmName); err != nil {
				return fmt.Errorf("failed to add VM to vm_live: %v", err)
			}
		}
		return nil
	}

	if exists {
		if err := db.RemoveVmLive(ctx, vmName); err != nil {
			return fmt.Errorf("failed to remove VM from vm_live: %v", err)
		}
	}

	return nil
}

func checkNFSReadWrite(conn *grpc.ClientConn, path string, maxTries int, delay time.Duration) error {
	if maxTries <= 0 {
		maxTries = 1
	}
	if delay < 0 {
		delay = 0
	}

	var lastErr error
	for i := 0; i < maxTries; i++ {
		if err := nfs.CheckReadWrite(conn, path); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if i < maxTries-1 && delay > 0 {
			time.Sleep(delay)
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("read/write check failed")
}

func (v *VirshService) StartAutoStartVms(ctx context.Context) error {
	const nfsMountPrefix = "/mnt/512SvMan/shared/"
	const nfsReadWriteTries = 10
	const nfsReadWriteDelay = 2 * time.Second

	autoStart, err := db.GetAllAutoStart(ctx)
	if err != nil {
		return err
	}

	for _, auto := range autoStart {
		vm, err := v.GetVmByName(auto.VmName)
		if err != nil {
			logger.Error("auto start vm does not exist: " + auto.VmName + " err: " + err.Error())
			continue
		}
		if vm == nil {
			logger.Error("auto start vm does not exist: " + auto.VmName)
			continue
		}

		if vm.State == grpcVirsh.VmState_RUNNING {
			continue
		}

		tries := 0
		for {

			//getting conn
			conn := protocol.GetConnectionByMachineName(vm.MachineName)
			if conn == nil || conn.Connection == nil {
				logger.Error("wtf how is not conn and found vm autostart bug wtfwtf")
				time.Sleep(10 * time.Second)
				continue
			}

			tries++
			// 30*60(sec of min) = 1800    / 10(sleep time) =180, so this tries every 10 seconds for half an hour
			if tries == 180 {
				logger.Error("Tried to start vm " + vm.Name + " 180 times without success")
				break
			}

			diskPath := strings.TrimSpace(vm.DiskPath)
			if diskPath != "" && strings.HasPrefix(diskPath, nfsMountPrefix) {
				logger.Infof("autostart vm %s: checking NFS disk path on %s (%s)", vm.Name, vm.MachineName, diskPath)
				found, err := nfs.CanFindFileOrDir(conn.Connection, diskPath)
				if err != nil {
					logger.Errorf("disk path check failed for vm %s on %s (%s): %v", vm.Name, vm.MachineName, diskPath, err)
					time.Sleep(10 * time.Second)
					continue
				}
				if !found {
					logger.Warnf("disk path not ready for vm %s on %s (%s)", vm.Name, vm.MachineName, diskPath)
					time.Sleep(10 * time.Second)
					continue
				}
				logger.Infof("autostart vm %s: disk path found on %s (%s)", vm.Name, vm.MachineName, diskPath)
				if err := checkNFSReadWrite(conn.Connection, diskPath, nfsReadWriteTries, nfsReadWriteDelay); err != nil {
					logger.Warnf("nfs read/write not ready for vm %s on %s (%s): %v", vm.Name, vm.MachineName, diskPath, err)
					time.Sleep(10 * time.Second)
					continue
				}
				logger.Infof("autostart vm %s: NFS read/write ok on %s (%s)", vm.Name, vm.MachineName, diskPath)
			}

			//start vm, if after 10 secs is not start again for 30 mins
			logger.Info("start vm: " + vm.Name)
			if err := virsh.StartVm(context.Background(), conn.Connection, vm); err != nil {
				if strings.Contains(err.Error(), "domain is already running") {
					continue
				}
				logger.Error("cannot start vm auto start: " + vm.Name + " err: " + err.Error())
				time.Sleep(10 * time.Second)
				continue
			}

			time.Sleep(10 * time.Second)

			vm, err = v.GetVmByName(auto.VmName)
			if err != nil {
				logger.Error("auto start vm does not exist: " + auto.VmName + " err: " + err.Error())
				continue
			}
			if vm == nil {
				logger.Error("auto start vm does not exist: " + auto.VmName)
				continue
			}
			if vm.State == grpcVirsh.VmState_RUNNING {
				break
			}
		}
	}
	return nil
}

func (v *VirshService) isVmLive(ctx context.Context, vmName string) (bool, error) {
	// Check if it's a live VM before deleting
	liveQuestion := false
	_, err := db.GetVmLiveByName(ctx, vmName)
	if err != nil {
		if err != sql.ErrNoRows {
			return false, err
		}
	} else {
		liveQuestion = true
	}
	return liveQuestion, nil
}

func (v *VirshService) MoveDisk(ctx context.Context, vmName string, nfsId int, newName string) error {
	logErr := func(e error) error {
		logger.Error(e.Error())
		return e
	}

	//copy disk, undefine vm, define again with migrateColdVm
	vm, err := v.GetVmByName(vmName)
	if err != nil {
		return logErr(err)
	}
	if vm == nil {
		return logErr(fmt.Errorf("vm %s does not exist", vmName))
	}

	newName = strings.TrimSpace(newName)
	if newName == "" {
		return logErr(fmt.Errorf("new VM name is required"))
	}
	if newName == vmName {
		return logErr(fmt.Errorf("new VM name must differ from the source VM name"))
	}

	exists, err := virsh.DoesVMExist(newName)
	if err != nil {
		return logErr(fmt.Errorf("error checking if VM exists: %v", err))
	}
	if exists {
		return logErr(fmt.Errorf("a VM with the name %s already exists", newName))
	}

	if vm.State != grpcVirsh.VmState_SHUTOFF {
		return logErr(fmt.Errorf("vm %s needs to be shutdown", vmName))
	}
	vmNfs, err := v.GetNfsByVM(ctx, vm)
	if err != nil {
		return logErr(err)
	}
	if vmNfs == nfsId {
		return logErr(fmt.Errorf("cannot move disk to same nfs"))
	}

	// Check if it's a live VM before deleting
	liveQuestion, err := v.isVmLive(ctx, vmName)
	if err != nil {
		return logErr(err)
	}

	autoStartQuestion, err := db.GetAutoStartByName(ctx, vmName)
	if err != nil {
		return logErr(err)
	}

	//create folder for new vm
	//checks if nfsShareId exists also and creates finalFile path
	finalFile, err := v.ImportVmHelper(ctx, nfsId, newName)
	if err != nil {
		return logErr(err)
	}

	coldMigr := grpcVirsh.ColdMigrationRequest{
		VmName:      newName,
		DiskPath:    finalFile,
		Memory:      vm.DefinedRam,
		VCpus:       vm.DefinedCPUS,
		Network:     vm.Network,
		VncPassword: vm.VNCPassword,
		CpuXML:      vm.CPUXML,
		Live:        liveQuestion,
	}

	marshaler := protojson.MarshalOptions{EmitUnpopulated: true, Indent: "  "}
	data, err := marshaler.Marshal(&coldMigr)
	if err != nil {
		logger.Debug("failed to marshal coldMigr: " + err.Error())
	} else {
		logger.Debug(string(data))
	}

	go func() {
		taskCtx, cancel := context.WithTimeout(context.Background(), longTaskTimeout)
		defer cancel()

		err := func() error {
			if err := copyFile(vm.DiskPath, finalFile, newName); err != nil {
				return fmt.Errorf("copy disk: %w", err)
			}

			if err := v.ColdMigrateVm(taskCtx, vm.MachineName, &coldMigr); err != nil {
				return fmt.Errorf("ColdMigrateVm: %w", err)
			}

			if err := v.DeleteVM(taskCtx, vmName); err != nil {
				return fmt.Errorf("DeleteVM: %w", err)
			}

			if autoStartQuestion != nil {
				if err := db.AddAutoStart(taskCtx, newName); err != nil {
					return fmt.Errorf("AddAutoStart: %w", err)
				}
			}

			return nil
		}()

		if err != nil {
			logger.Error(err.Error())
			extra.SendWebsocketMessage(protoExtra.WebSocketsMessageType_Error, fmt.Sprintf("MoveDisk failed for %s: %v", vmName, err), vmName)
			sendImportantNotification("MoveDisk failed", err)
		}
	}()

	return nil
}

func (v *VirshService) ColdMigrate(ctx context.Context, vmName string, destinationMachine string) error {
	logErr := func(e error) error {
		logger.Error(e.Error())
		return e
	}

	//undefine vm, define again with migrateCOldVM
	vm, err := v.GetVmByName(vmName)
	if err != nil {
		return logErr(err)
	}

	if vm == nil {
		return logErr(fmt.Errorf("vm %s does not exist", vmName))
	}

	if vm.State != grpcVirsh.VmState_SHUTOFF {
		return logErr(fmt.Errorf("vm %s needs to be shutdown", vmName))
	}

	liveQuestion, err := v.isVmLive(ctx, vmName)
	if err != nil {
		return logErr(err)
	}

	conn := protocol.GetConnectionByMachineName(vm.MachineName)
	if conn == nil || conn.Connection == nil {
		return logErr(fmt.Errorf("machine %s is not connected", vm.MachineName))
	}

	if vm.MachineName == destinationMachine {
		return logErr(fmt.Errorf("destinationMachine can not be the same as origin machine"))
	}

	//check if it exists

	coldMigr := grpcVirsh.ColdMigrationRequest{
		VmName:      vmName,
		DiskPath:    vm.DiskPath,
		Memory:      vm.DefinedRam,
		VCpus:       vm.DefinedCPUS,
		Network:     vm.Network,
		VncPassword: vm.VNCPassword,
		CpuXML:      vm.CPUXML,
		Live:        liveQuestion,
	}

	marshaler := protojson.MarshalOptions{EmitUnpopulated: true, Indent: "  "}
	data, err := marshaler.Marshal(&coldMigr)
	if err != nil {
		logger.Debug("failed to marshal coldMigr: " + err.Error())
	} else {
		logger.Debug(string(data))
	}

	go func() {
		taskCtx, cancel := context.WithTimeout(context.Background(), longTaskTimeout)
		defer cancel()

		err := func() error {
			if err := v.ColdMigrateVm(taskCtx, destinationMachine, &coldMigr); err != nil {
				return err
			}

			if err := virsh.UndefineVM(conn.Connection, vm); err != nil {
				return err
			}
			return nil
		}()

		if err != nil {
			logger.Error(err.Error())
			extra.SendWebsocketMessage(protoExtra.WebSocketsMessageType_Error, fmt.Sprintf("ColdMigrate failed for %s: %v", vmName, err), vmName)
			sendImportantNotification("ColdMigrate failed", err)
		}
	}()

	return nil
}
func (v *VirshService) CloneVM(ctx context.Context, vmName string, newName string, destinationMachine string, nfsId int) error {
	logErr := func(e error) error {
		logger.Error(e.Error())
		return e
	}

	//copy disk, define

	vm, err := v.GetVmByName(vmName)
	if err != nil {
		return logErr(err)
	}

	if vm == nil {
		return logErr(fmt.Errorf("vm %s does not exist", vmName))
	}

	newName = strings.TrimSpace(newName)
	if newName == "" {
		return logErr(fmt.Errorf("new VM name is required"))
	}
	if newName == vmName {
		return logErr(fmt.Errorf("new VM name must differ from the source VM name"))
	}

	exists, err := virsh.DoesVMExist(newName)
	if err != nil {
		return logErr(fmt.Errorf("error checking if VM exists: %v", err))
	}
	if exists {
		return logErr(fmt.Errorf("a VM with the name %s already exists", newName))
	}

	liveQuestion, err := v.isVmLive(ctx, vmName)
	if err != nil {
		return logErr(err)
	}

	//create folder for new vm
	//checks if nfsShareId exists also and creates finalFile path
	finalFile, err := v.ImportVmHelper(ctx, nfsId, newName)
	if err != nil {
		return logErr(err)
	}

	coldMigr := grpcVirsh.ColdMigrationRequest{
		VmName:      newName,
		DiskPath:    finalFile,
		Memory:      vm.DefinedRam,
		VCpus:       vm.DefinedCPUS,
		Network:     vm.Network,
		VncPassword: vm.VNCPassword,
		CpuXML:      vm.CPUXML,
		Live:        liveQuestion,
	}

	marshaler := protojson.MarshalOptions{EmitUnpopulated: true, Indent: "  "}
	data, err := marshaler.Marshal(&coldMigr)
	if err != nil {
		logger.Debug("failed to marshal coldMigr: " + err.Error())
	} else {
		logger.Debug(string(data))
	}

	go func() {
		taskCtx, cancel := context.WithTimeout(context.Background(), longTaskTimeout)
		defer cancel()

		err := func() error {
			if vm.State != grpcVirsh.VmState_SHUTOFF {
				conn := protocol.GetConnectionByMachineName(vm.MachineName)
				if conn == nil || conn.Connection == nil {
					return fmt.Errorf("conn of vm is nil should not happen")
				}

				logger.Info("Frezzing")
				if err := virsh.FreezeDisk(conn.Connection, vm); err != nil {
					return err
				}

				defer func() {
					logger.Info("UnFrezzing")
					if err := virsh.UnFreezeDisk(conn.Connection, vm); err != nil {
						logger.Error("Cannot unfreeze machine " + vm.Name)
					}
				}()

				logger.Info("Copying")
				if err := copyFile(vm.DiskPath, finalFile, vmName); err != nil {
					return err
				}
			} else {
				if err := copyFile(vm.DiskPath, finalFile, vmName); err != nil {
					return err
				}
			}

			if err := v.ColdMigrateVm(taskCtx, destinationMachine, &coldMigr); err != nil {
				return err
			}
			return nil
		}()

		if err != nil {
			logger.Error(err.Error())
			extra.SendWebsocketMessage(protoExtra.WebSocketsMessageType_Error, fmt.Sprintf("CloneVM failed for %s: %v", newName, err), newName)
			sendImportantNotification("CloneVM failed", err)
		}
	}()

	return nil
}
