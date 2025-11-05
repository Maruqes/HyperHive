package services

import (
	"512SvMan/db"
	"512SvMan/env512"
	"512SvMan/extra"
	"512SvMan/protocol"
	"512SvMan/virsh"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"github.com/Maruqes/512SvMan/logger"
	"github.com/google/uuid"
)

var copyFileMu sync.Mutex

func copyFile(origin, dest, vmName string) error {
	copyFileMu.Lock()
	defer copyFileMu.Unlock()

	//actually write the file using buffered I/O with progress tracking
	input, err := os.Open(origin)
	if err != nil {
		return fmt.Errorf("cannot open source file during backup: %v", err)
	}
	defer input.Close()

	output, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("cannot create destination file: %v", err)
	}
	defer output.Close()

	// Get total file size for progress calculation
	fileInfo, err := input.Stat()
	if err != nil {
		return fmt.Errorf("cannot stat source file: %v", err)
	}
	totalSize := fileInfo.Size()

	// Progress tracking
	var copied int64
	buf := make([]byte, 32*1024*1024) // 32MB buffer

	for {
		n, err := input.Read(buf)
		if n > 0 {
			_, writeErr := output.Write(buf[:n])
			if writeErr != nil {
				return fmt.Errorf("error writing file: %v", writeErr)
			}
			copied += int64(n)

			// Calculate and log progress
			progress := float64(copied) / float64(totalSize) * 100
			extra.SendWebsocketMessage(extraGrpc.WebSocketsMessageType_BackUpVM, fmt.Sprintf("Backup progress for %s: %.2f%%", vmName, progress))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading file: %v", err)
		}
	}

	if err := output.Sync(); err != nil {
		return fmt.Errorf("failed to flush destination file: %v", err)
	}

	qemuUID, err := strconv.Atoi(env512.Qemu_UID)
	if err != nil {
		return fmt.Errorf("invalid qemu uid %s: %v", env512.Qemu_UID, err)
	}

	qemuGID, err := strconv.Atoi(env512.Qemu_GID)
	if err != nil {
		return fmt.Errorf("invalid qemu gid %s: %v", env512.Qemu_GID, err)
	}

	if err := output.Chown(qemuUID, qemuGID); err != nil {
		return fmt.Errorf("failed to set qemu ownership: %v", err)
	}

	if err := output.Chmod(0o777); err != nil {
		return fmt.Errorf("failed to set destination permissions: %v", err)
	}

	return nil
}

// returns file path or error
// checks if nfsShareId exists also and creates finalFile path
func (v *VirshService) ImportVmHelper(nfsId int, filename string) (string, error) {
	//get nfs share
	nfsShare, err := db.GetNFSShareByID(nfsId)
	if err != nil {
		return "", fmt.Errorf("failed to get NFS share by ID: %v", err)
	}
	if nfsShare == nil {
		return "", fmt.Errorf("NFS share with ID %d not found", nfsId)
	}

	var folder string
	if nfsShare.Target[len(nfsShare.Target)-1] != '/' {
		// mnt/ nfs / vmname / vmname.extension
		folder = nfsShare.Target + "/" + filename
	} else {
		folder = nfsShare.Target + filename
	}

	//if folder exists return err else create it
	exists, err := os.Stat(folder)
	if err == nil && exists.IsDir() {
		return "", fmt.Errorf("folder %s already exists", folder)
	}

	err = os.MkdirAll(folder, 0777)
	if err != nil {
		return "", fmt.Errorf("failed to create folder %s: %v", folder, err)
	}

	extenstion := ".qcow2"

	filePath := folder + "/" + filename + extenstion

	return filePath, nil
}

// Virtual machine needs to have "qemu-guest-agent" for live
// Virtual machine needs to have "qemu-guest-agent" for live
// Virtual machine needs to have "qemu-guest-agent" for live
func (v *VirshService) BackupVM(vmName string, nfsID int, automatic bool) error {
	//check if vmName exists and is turned off, check if nfsID exists
	vm, err := v.GetVmByName(vmName)
	if err != nil || vm == nil {
		return fmt.Errorf("problem getting vm it may not exist")
	}

	nfsShare, err := db.GetNFSShareByID(nfsID)
	if err != nil {
		return fmt.Errorf("failed to get NFS share by ID: %v", err)
	}
	if nfsShare == nil {
		return fmt.Errorf("%s", "NFS share not found with ID"+strconv.Itoa(nfsID))
	}

	// nfsShare.Target + "/backUpFolder" + uuid.string
	//generate uuid
	bakUUID := uuid.New()

	if nfsShare.Target[len(nfsShare.Target)-1] == '/' {
		nfsShare.Target = nfsShare.Target[:len(nfsShare.Target)-1]
	}

	//creating actual backUpFolder folder
	backUpFolder := nfsShare.Target + "/" + "backup-" + bakUUID.String()

	//if backUpFolder folder already exists
	_, err = os.Stat(backUpFolder)
	if err != nil {
		//check if err is already exists
		if !os.IsNotExist(err) {
			return fmt.Errorf("the uuid existed?!?!?! 0 in a quadrillion chance")
		}
	}

	//create folder
	err = os.Mkdir(backUpFolder, 0o777)
	if err != nil {
		return fmt.Errorf("could not create the backUpFolder folder")
	}

	//create struct with already extension file path
	backup := &db.VirshBackup{
		Name:      vmName,
		Path:      backUpFolder + "/" + vmName + ".qcow2",
		NfsId:     nfsID,
		Automatic: automatic,
	}

	if vm.State != grpcVirsh.VmState_SHUTOFF {

		conn := protocol.GetConnectionByMachineName(vm.MachineName)
		if conn == nil || conn.Connection == nil {
			return fmt.Errorf("conn of vm is nill shuld not hapen")
		}

		logger.Info("Frezzing")
		err := virsh.FreezeDisk(conn.Connection, vm)
		if err != nil {
			return err
		}

		defer func() {
			logger.Info("UnFrezzing")
			err = virsh.UnFreezeDisk(conn.Connection, vm)
			if err != nil {
				logger.Error("Cannot unfreeze machine " + vm.Name)
			}
		}()

		logger.Info("Copying")
		err = copyFile(vm.DiskPath, backup.Path, vmName)
		if err != nil {
			return err
		}

	} else {
		err = copyFile(vm.DiskPath, backup.Path, vmName)
		if err != nil {
			return err
		}
	}

	err = db.InsertVirshBackup(backup)
	if err != nil {
		return fmt.Errorf("problems writing to db backup: %v", err)
	}

	return nil
}

func (v *VirshService) DeleteBackup(bakId int) error {
	bakup, err := db.GetVirshBackupById(bakId)
	if err != nil {
		return err
	}

	if bakup == nil {
		return fmt.Errorf("backupId not found")
	}

	dir := filepath.Dir(bakup.Path)
	if dir == "" || dir == "/" || dir == "." {
		return fmt.Errorf("refusing to remove unsafe directory: %q", dir)
	}

	err = db.DeleteVirshBackupById(bakId)
	if err != nil {
		return err
	}

	err = os.Remove(bakup.Path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// remove the folder that contained the backup
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to remove backup folder %s: %v", dir, err)
	}

	return nil
}

// clonar bak para uma nova pasta e defenir
func (v *VirshService) UseBackup(ctx context.Context, bakID int, slaveName string, nfsId int, coldReq *grpcVirsh.ColdMigrationRequest) error {
	originConn := protocol.GetConnectionByMachineName(slaveName)
	if originConn == nil {
		return fmt.Errorf("origin machine %s not found", slaveName)
	}

	backup, err := db.GetVirshBackupById(bakID)
	if err != nil {
		return fmt.Errorf("failed to get backup by ID: %v", err)
	}
	if backup == nil {
		return fmt.Errorf("backup with ID %d not found", bakID)
	}

	exists, err := virsh.DoesVMExist(coldReq.VmName)
	if err != nil {
		return fmt.Errorf("error checking if VM exists: %v", err)
	}
	if exists {
		return fmt.Errorf("a VM with the name %s already exists", coldReq.VmName)
	}

	// Get NFS share
	nfsShare, err := db.GetNFSShareByID(nfsId)
	if err != nil {
		return fmt.Errorf("failed to get NFS share by ID: %v", err)
	}
	if nfsShare == nil {
		return fmt.Errorf("NFS share with ID %d not found", nfsId)
	}

	// Create new folder for the VM
	var newFolder string
	if nfsShare.Target[len(nfsShare.Target)-1] != '/' {
		newFolder = nfsShare.Target + "/" + coldReq.VmName
	} else {
		newFolder = nfsShare.Target + coldReq.VmName
	}

	// Check if folder exists
	_, err = os.Stat(newFolder)
	if err == nil {
		return fmt.Errorf("folder %s already exists", newFolder)
	}

	// Create folder
	err = os.MkdirAll(newFolder, 0777)
	if err != nil {
		return fmt.Errorf("failed to create folder %s: %v", newFolder, err)
	}

	newDiskPath := newFolder + "/" + coldReq.VmName + ".qcow2"
	err = copyFile(backup.Path, newDiskPath, coldReq.VmName)
	if err != nil {
		os.RemoveAll(newFolder)
		return fmt.Errorf("failed to copy backup file: %v", err)
	}

	coldReq.DiskPath = newDiskPath

	//fazer cold migration
	err = v.ColdMigrateVm(
		ctx,
		slaveName,
		coldReq,
	)

	if err != nil {
		return err
	}

	return nil
}

func (v *VirshService) CreateAutoBak(bak db.AutomaticBackup) error {
	//check vmName
	exists, err := virsh.DoesVMExist(bak.VmName)
	if err != nil {
		return fmt.Errorf("error checking if VM exists: %v", err)
	}

	if !exists {
		return fmt.Errorf("vm does not exist")
	}

	err = bak.MaxTime.Validate()
	if err != nil {
		return err
	}

	err = bak.MinTime.Validate()
	if err != nil {
		return err
	}

	if calculateWindowDuration(bak.MinTime, bak.MaxTime) < (time.Minute * 45) {
		return fmt.Errorf("time window between MinTime and MaxTime must be at least 45 minutes")
	}

	//check nfs mount
	nfsShare, err := db.GetNFSShareByID(bak.NfsMountId)
	if err != nil {
		return fmt.Errorf("failed to get NFS share by ID: %v", err)
	}
	if nfsShare == nil {
		return fmt.Errorf("NFS share not found with ID %d", bak.NfsMountId)
	}

	//validate frequency and retention
	if bak.FrequencyDays < 1 {
		return fmt.Errorf("frequency days must be at least 1")
	}
	if bak.MaxBackupsRetain < 1 {
		return fmt.Errorf("max backups retain must be at least 1")
	}

	//add to database
	err = db.AddAutomaticBackup(&bak)
	if err != nil {
		return fmt.Errorf("failed to add automatic backup: %v", err)
	}

	return nil
}

func (v *VirshService) UpdateAutoBak(id int, bak db.AutomaticBackup) error {
	//check if automatic backup exists
	existingBak, err := db.GetAutomaticBackupById(id)
	if err != nil {
		return fmt.Errorf("failed to get automatic backup by ID: %v", err)
	}
	if existingBak == nil {
		return fmt.Errorf("automatic backup with ID %d not found", id)
	}

	//check vmName
	exists, err := virsh.DoesVMExist(bak.VmName)
	if err != nil {
		return fmt.Errorf("error checking if VM exists: %v", err)
	}
	if !exists {
		return fmt.Errorf("vm does not exist")
	}

	err = bak.MaxTime.Validate()
	if err != nil {
		return err
	}

	err = bak.MinTime.Validate()
	if err != nil {
		return err
	}

	if calculateWindowDuration(bak.MinTime, bak.MaxTime) < (time.Minute * 45) {
		return fmt.Errorf("time window between MinTime and MaxTime must be at least 45 minutes")
	}
	//check nfs mount
	nfsShare, err := db.GetNFSShareByID(bak.NfsMountId)
	if err != nil {
		return fmt.Errorf("failed to get NFS share by ID: %v", err)
	}
	if nfsShare == nil {
		return fmt.Errorf("NFS share not found with ID %d", bak.NfsMountId)
	}

	//validate frequency and retention
	if bak.FrequencyDays < 1 {
		return fmt.Errorf("frequency days must be at least 1")
	}
	if bak.MaxBackupsRetain < 1 {
		return fmt.Errorf("max backups retain must be at least 1")
	}

	//set the ID to ensure we update the correct record
	bak.Id = id

	//update in database
	err = db.UpdateAutomaticBackup(&bak)
	if err != nil {
		return fmt.Errorf("failed to update automatic backup: %v", err)
	}

	return nil
}

func (v *VirshService) DeleteAutoBak(id int) error {
	//check if automatic backup exists
	existingBak, err := db.GetAutomaticBackupById(id)
	if err != nil {
		return fmt.Errorf("failed to get automatic backup by ID: %v", err)
	}
	if existingBak == nil {
		return fmt.Errorf("automatic backup with ID %d not found", id)
	}

	//remove from database
	err = db.RemoveAutomaticBackupById(id)
	if err != nil {
		return fmt.Errorf("failed to delete automatic backup: %v", err)
	}

	return nil
}

func (v *VirshService) EnableAutoBak(id int) error {
	//check if automatic backup exists
	existingBak, err := db.GetAutomaticBackupById(id)
	if err != nil {
		return fmt.Errorf("failed to get automatic backup by ID: %v", err)
	}
	if existingBak == nil {
		return fmt.Errorf("automatic backup with ID %d not found", id)
	}

	//check if already enabled
	if existingBak.Enabled {
		return fmt.Errorf("automatic backup is already enabled")
	}

	//enable in database
	err = db.EnableAutomaticBackupById(id)
	if err != nil {
		return fmt.Errorf("failed to enable automatic backup: %v", err)
	}

	return nil
}

func (v *VirshService) DisableAutoBak(id int) error {
	//check if automatic backup exists
	existingBak, err := db.GetAutomaticBackupById(id)
	if err != nil {
		return fmt.Errorf("failed to get automatic backup by ID: %v", err)
	}
	if existingBak == nil {
		return fmt.Errorf("automatic backup with ID %d not found", id)
	}

	//check if already disabled
	if !existingBak.Enabled {
		return fmt.Errorf("automatic backup is already disabled")
	}

	//disable in database
	err = db.DisableAutomaticBackupById(id)
	if err != nil {
		return fmt.Errorf("failed to disable automatic backup: %v", err)
	}

	return nil
}

// fazer backups
// se sucesso eliminar com GetAutomaticBackups
// ja elimina backups antigos e mantem MaxBackupsRetain
func (v *VirshService) createAutoBak(bak db.AutomaticBackup) error {
	if err := v.BackupVM(bak.VmName, bak.NfsMountId, true); err != nil {
		return err
	}

	completedAt := time.Now().UTC().Format(time.RFC3339)
	if err := db.UpdateAutomaticBackupTimes(bak.Id, &completedAt); err != nil {
		return fmt.Errorf("failed to update backup timestamp: %v", err)
	}

	//eliminar baks antigos
	baksVm, err := db.GetAutomaticBackups(bak.VmName)
	if err != nil {
		return err
	}

	// Sort backups by CreatedAt (newest first)
	sort.SliceStable(baksVm, func(i, j int) bool {
		timeI, okI := parseBackupTimestamp(baksVm[i].CreatedAt)
		timeJ, okJ := parseBackupTimestamp(baksVm[j].CreatedAt)

		switch {
		case okI && okJ:
			return timeI.After(timeJ)
		case okI:
			return true
		case okJ:
			return false
		default:
			return baksVm[i].CreatedAt > baksVm[j].CreatedAt
		}
	})

	// Delete oldest backups if we exceed MaxBackupsRetain
	if len(baksVm) > bak.MaxBackupsRetain {
		for i := bak.MaxBackupsRetain; i < len(baksVm); i++ {
			err := v.DeleteBackup(baksVm[i].Id)
			if err != nil {
				logger.Error(fmt.Sprintf("failed to delete old backup %d: %v", baksVm[i].Id, err))
			}
		}
	}

	return nil
}
func (v *VirshService) LoopAutomaticBaks() {
	// Prevent multiple instances from running simultaneously
	if !v.backupLoopRunning.CompareAndSwap(false, true) {
		logger.Warn("Automatic backup loop already running")
		return
	}

	timeDiff := 20
	go func() {
		defer v.backupLoopRunning.Store(false)
		defer func() {
			if r := recover(); r != nil {
				logger.Error(fmt.Sprintf("Automatic backup loop panic: %v", r))
				// Optionally restart the loop after a delay
				time.Sleep(time.Minute * 5)
				v.backupLoopRunning.Store(false)
				v.LoopAutomaticBaks()
			}
		}()

		for {
			logger.Info("running automatic baks")
			nowTime := time.Now()
			currentClock := db.Clock{Hours: nowTime.Hour(), Minutes: nowTime.Minute()}

			baks, err := db.GetEnabledAutomaticBackupsAt(currentClock)
			if err != nil {
				logger.Error("Error getting automatic backups: " + err.Error())
				time.Sleep(time.Minute * time.Duration(timeDiff))
				continue
			}

			logger.Info(fmt.Sprintf("Processing %d automatic backup(s)", len(baks)))

			for _, bak := range baks {
				vm, err := v.GetVmByName(bak.VmName)
				if err != nil {
					logger.Error(fmt.Sprintf("Failed to resolve VM %s for automatic backup: %v", bak.VmName, err))
					continue
				}
				if vm == nil {
					logger.Warn(fmt.Sprintf("Skipping automatic backup for VM %s because it no longer exists", bak.VmName))
					continue
				}

				machineCon := protocol.GetConnectionByMachineName(vm.MachineName)
				if machineCon == nil || machineCon.Connection == nil {
					logger.Error(fmt.Sprintf("Failed to get connection for VM %s, slave %s is down", bak.VmName, vm.MachineName))
					continue
				}

				// Verify we're actually within the backup's time window
				if !currentClock.IsBetween(bak.MinTime, bak.MaxTime) {
					logger.Info(fmt.Sprintf("Skipping backup for VM %s - outside time window (current: %s, window: %s-%s)",
						bak.VmName, currentClock.String(), bak.MinTime.String(), bak.MaxTime.String()))
					continue
				}

				minimalLastBak := nowTime.Add(-time.Duration(bak.FrequencyDays) * 24 * time.Hour)

				shouldBackup := false

				if bak.LastBackupTime == nil {
					shouldBackup = true
				} else {
					lastBakTime, ok := parseBackupTimestamp(*bak.LastBackupTime)
					if !ok {
						logger.Error(fmt.Sprintf("Error parsing last backup time for VM %s: %s", bak.VmName, *bak.LastBackupTime))
						continue
					}

					if lastBakTime.Before(minimalLastBak) {
						shouldBackup = true
					}
				}

				if shouldBackup {
					logger.Info(fmt.Sprintf("Creating automatic backup for VM %s", bak.VmName))
					err = v.createAutoBak(bak)
					if err != nil {
						logger.Error(fmt.Sprintf("Failed to create automatic backup for VM %s: %v", bak.VmName, err))
					}
				}
			}
			time.Sleep(time.Minute * time.Duration(timeDiff))
		}
	}()
}

func parseBackupTimestamp(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}

	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts, true
	}

	if ts, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.UTC); err == nil {
		return ts, true
	}

	return time.Time{}, false
}

func calculateWindowDuration(minClock, maxClock db.Clock) time.Duration {
	minTime := minClock.GetTodayTime()
	maxTime := maxClock.GetTodayTime()

	duration := maxTime.Sub(minTime)
	if duration <= 0 {
		duration += 24 * time.Hour
	}

	return duration
}
