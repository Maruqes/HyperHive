package services

import (
	"512SvMan/db"
	"512SvMan/env512"
	"512SvMan/extra"
	"512SvMan/nots"
	"512SvMan/protocol"
	"512SvMan/virsh"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"github.com/Maruqes/512SvMan/logger"
	"github.com/google/uuid"
)

var copyFileMu sync.Mutex

func copyFile(origin, dest, vmName string) (err error) {

	defer func() {
		if err != nil {
			nots.SendGlobalNotification("Problem CopyFile", fmt.Sprint(err.Error()), "/", true)
		}
	}()

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
			extra.SendWebsocketMessage(extraGrpc.WebSocketsMessageType_BackUpVM, fmt.Sprintf("Backup progress for %s: %.2f%%", vmName, progress), "")
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

// helper to send important notifications
func sendImportantNotification(title string, err error) {
	if err == nil {
		return
	}
	nots.SendGlobalNotification(title, fmt.Sprint(err.Error()), "/", true)
}

// validate and remove a backup directory while avoiding accidental broad deletes
func removeBackupDirectory(dir string) error {
	if err := validateBackupDirectory(dir); err != nil {
		return err
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to remove backup folder %s: %v", dir, err)
	}

	return nil
}

func validateBackupDirectory(dir string) error {
	if dir == "" || dir == "/" || dir == "." {
		return fmt.Errorf("refusing to remove unsafe directory: %q", dir)
	}

	// guard against deleting unrelated folders
	if !strings.HasPrefix(filepath.Base(dir), "backup-") {
		return fmt.Errorf("refusing to remove non-backup directory: %q", dir)
	}
	return nil
}

// returns file path or error
// checks if nfsShareId exists also and creates finalFile path
func (v *VirshService) ImportVmHelper(ctx context.Context, nfsId int, filename string) (string, error) {
	//get nfs share
	nfsShare, err := db.GetNFSShareByID(ctx, nfsId)
	if err != nil {
		sendImportantNotification("ImportVmHelper: GetNFSShareByID failed", err)
		return "", fmt.Errorf("failed to get NFS share by ID: %v", err)
	}
	if nfsShare == nil {
		err := fmt.Errorf("NFS share with ID %d not found", nfsId)
		sendImportantNotification("ImportVmHelper: NFS share not found", err)
		return "", err
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
		err := fmt.Errorf("folder %s already exists", folder)
		sendImportantNotification("ImportVmHelper: folder exists", err)
		return "", err
	}

	err = os.MkdirAll(folder, 0777)
	if err != nil {
		sendImportantNotification("ImportVmHelper: failed to create folder", fmt.Errorf("%s: %v", folder, err))
		return "", fmt.Errorf("failed to create folder %s: %v", folder, err)
	}

	extenstion := ".qcow2"

	filePath := folder + "/" + filename + extenstion

	return filePath, nil
}

// Virtual machine needs to have "qemu-guest-agent" for live
// Virtual machine needs to have "qemu-guest-agent" for live
// Virtual machine needs to have "qemu-guest-agent" for live
func (v *VirshService) BackupVM(ctx context.Context, vmName string, nfsID int, automatic bool) error {
	//check if vmName exists and is turned off, check if nfsID exists
	vm, err := v.GetVmByName(vmName)
	if err != nil {
		sendImportantNotification("BackupVM: GetVmByName failed", err)
		return fmt.Errorf("problem getting vm: %v", err)
	}
	if vm == nil {
		err := fmt.Errorf("vm %s not found", vmName)
		sendImportantNotification("BackupVM: VM not found", err)
		return err
	}

	nfsShare, err := db.GetNFSShareByID(ctx, nfsID)
	if err != nil {
		sendImportantNotification("BackupVM: GetNFSShareByID failed", err)
		return fmt.Errorf("failed to get NFS share by ID: %v", err)
	}
	if nfsShare == nil {
		err := fmt.Errorf("NFS share not found with ID %d", nfsID)
		sendImportantNotification("BackupVM: NFS share not found", err)
		return err
	}

	// nfsShare.Target + "/backUpFolder" + uuid.string
	//generate uuid
	bakUUID := uuid.New()

	if nfsShare.Target[len(nfsShare.Target)-1] == '/' {
		nfsShare.Target = nfsShare.Target[:len(nfsShare.Target)-1]
	}

	//creating actual backUpFolder folder
	backUpFolder := nfsShare.Target + "/" + "backup-" + bakUUID.String()

	//create folder with all parent directories
	err = os.MkdirAll(backUpFolder, 0o777)
	if err != nil {
		sendImportantNotification("BackupVM: failed to create backup folder", err)
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
			err := fmt.Errorf("connection for vm %s is nil (slave %s likely down)", vmName, vm.MachineName)
			sendImportantNotification("BackupVM: connection missing", err)
			return err
		}

		logger.Info("Frezzing")
		err := virsh.FreezeDisk(conn.Connection, vm)
		if err != nil {
			sendImportantNotification("BackupVM: FreezeDisk failed", err)
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
			sendImportantNotification("BackupVM: copyFile failed", err)
			return err
		}

	} else {
		err = copyFile(vm.DiskPath, backup.Path, vmName)
		if err != nil {
			sendImportantNotification("BackupVM: copyFile failed", err)
			return err
		}
	}

	err = db.InsertVirshBackup(ctx, backup)
	if err != nil {
		sendImportantNotification("BackupVM: InsertVirshBackup failed", err)
		return fmt.Errorf("problems writing to db backup: %v", err)
	}

	nots.SendGlobalNotification("Backup successful", fmt.Sprintf("Backup %s created at %s", vmName, backup.Path), "/", false)
	return nil
}

func (v *VirshService) DeleteBackup(ctx context.Context, bakId int) error {
	bakup, err := db.GetVirshBackupById(ctx, bakId)
	if err != nil {
		sendImportantNotification("DeleteBackup: GetVirshBackupById failed", err)
		return err
	}

	if bakup == nil {
		err := fmt.Errorf("backupId %d not found", bakId)
		sendImportantNotification("DeleteBackup: backup not found", err)
		return err
	}

	dir := filepath.Dir(bakup.Path)

	if err := validateBackupDirectory(dir); err != nil {
		return err
	}

	err = db.DeleteVirshBackupById(ctx, bakId)
	if err != nil {
		sendImportantNotification("DeleteBackup: DeleteVirshBackupById failed", err)
		return err
	}

	fileErr := os.Remove(bakup.Path)
	if fileErr != nil && !os.IsNotExist(fileErr) {
		sendImportantNotification("DeleteBackup: failed to remove backup file", fileErr)
	}

	// remove the folder that contained the backup (even if the file removal had issues)
	dirErr := removeBackupDirectory(dir)
	if dirErr != nil {
		sendImportantNotification("DeleteBackup: failed to remove backup folder", dirErr)
		return dirErr
	}

	if fileErr != nil && !os.IsNotExist(fileErr) {
		return fileErr
	}

	return nil
}

// clonar bak para uma nova pasta e defenir
func (v *VirshService) UseBackup(ctx context.Context, bakID int, slaveName string, nfsId int, coldReq *grpcVirsh.ColdMigrationRequest) error {
	originConn := protocol.GetConnectionByMachineName(slaveName)
	if originConn == nil {
		err := fmt.Errorf("origin machine %s not found", slaveName)
		sendImportantNotification("UseBackup: origin machine not found", err)
		return err
	}

	backup, err := db.GetVirshBackupById(ctx, bakID)
	if err != nil {
		sendImportantNotification("UseBackup: GetVirshBackupById failed", err)
		return fmt.Errorf("failed to get backup by ID: %v", err)
	}
	if backup == nil {
		err := fmt.Errorf("backup with ID %d not found", bakID)
		sendImportantNotification("UseBackup: backup not found", err)
		return err
	}

	exists, err := virsh.DoesVMExist(coldReq.VmName)
	if err != nil {
		sendImportantNotification("UseBackup: DoesVMExist failed", err)
		return fmt.Errorf("error checking if VM exists: %v", err)
	}
	if exists {
		err := fmt.Errorf("a VM with the name %s already exists", coldReq.VmName)
		sendImportantNotification("UseBackup: target VM name already exists", err)
		return err
	}

	// Get NFS share
	nfsShare, err := db.GetNFSShareByID(ctx, nfsId)
	if err != nil {
		sendImportantNotification("UseBackup: GetNFSShareByID failed", err)
		return fmt.Errorf("failed to get NFS share by ID: %v", err)
	}
	if nfsShare == nil {
		err := fmt.Errorf("NFS share with ID %d not found", nfsId)
		sendImportantNotification("UseBackup: NFS share not found", err)
		return err
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
		err := fmt.Errorf("folder %s already exists", newFolder)
		sendImportantNotification("UseBackup: folder already exists", err)
		return err
	}

	// Create folder
	err = os.MkdirAll(newFolder, 0777)
	if err != nil {
		sendImportantNotification("UseBackup: failed to create new folder", err)
		return fmt.Errorf("failed to create folder %s: %v", newFolder, err)
	}

	newDiskPath := newFolder + "/" + coldReq.VmName + ".qcow2"
	err = copyFile(backup.Path, newDiskPath, coldReq.VmName)
	if err != nil {
		os.RemoveAll(newFolder)
		sendImportantNotification("UseBackup: failed to copy backup file", err)
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
		sendImportantNotification("UseBackup: ColdMigrateVm failed", err)
		return err
	}

	return nil
}

func (v *VirshService) CreateAutoBak(ctx context.Context, bak db.AutomaticBackup) error {
	//check vmName
	exists, err := virsh.DoesVMExist(bak.VmName)
	if err != nil {
		return fmt.Errorf("error checking if VM exists: %v", err)
	}

	if !exists {
		err := fmt.Errorf("vm %s does not exist", bak.VmName)
		sendImportantNotification("CreateAutoBak: VM does not exist", err)
		return err
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
	nfsShare, err := db.GetNFSShareByID(ctx, bak.NfsMountId)
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
	err = db.AddAutomaticBackup(ctx, &bak)
	if err != nil {
		sendImportantNotification("CreateAutoBak: AddAutomaticBackup failed", err)
		return fmt.Errorf("failed to add automatic backup: %v", err)
	}

	return nil
}

func (v *VirshService) UpdateAutoBak(ctx context.Context, id int, bak db.AutomaticBackup) error {
	//check if automatic backup exists
	existingBak, err := db.GetAutomaticBackupById(ctx, id)
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
		err := fmt.Errorf("vm %s does not exist", bak.VmName)
		sendImportantNotification("UpdateAutoBak: VM does not exist", err)
		return err
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
	nfsShare, err := db.GetNFSShareByID(ctx, bak.NfsMountId)
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
	err = db.UpdateAutomaticBackup(ctx, &bak)
	if err != nil {
		sendImportantNotification("UpdateAutoBak: UpdateAutomaticBackup failed", err)
		return fmt.Errorf("failed to update automatic backup: %v", err)
	}

	return nil
}

func (v *VirshService) DeleteAutoBak(ctx context.Context, id int) error {
	//check if automatic backup exists
	existingBak, err := db.GetAutomaticBackupById(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get automatic backup by ID: %v", err)
	}
	if existingBak == nil {
		err := fmt.Errorf("automatic backup with ID %d not found", id)
		sendImportantNotification("DeleteAutoBak: not found", err)
		return err
	}

	//remove from database
	err = db.RemoveAutomaticBackupById(ctx, id)
	if err != nil {
		sendImportantNotification("DeleteAutoBak: RemoveAutomaticBackupById failed", err)
		return fmt.Errorf("failed to delete automatic backup: %v", err)
	}

	return nil
}

func (v *VirshService) EnableAutoBak(ctx context.Context, id int) error {
	//check if automatic backup exists
	existingBak, err := db.GetAutomaticBackupById(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get automatic backup by ID: %v", err)
	}
	if existingBak == nil {
		err := fmt.Errorf("automatic backup with ID %d not found", id)
		sendImportantNotification("EnableAutoBak: not found", err)
		return err
	}

	//check if already enabled
	if existingBak.Enabled {
		return fmt.Errorf("automatic backup is already enabled")
	}

	//enable in database
	err = db.EnableAutomaticBackupById(ctx, id)
	if err != nil {
		sendImportantNotification("EnableAutoBak: EnableAutomaticBackupById failed", err)
		return fmt.Errorf("failed to enable automatic backup: %v", err)
	}

	return nil
}

func (v *VirshService) DisableAutoBak(ctx context.Context, id int) error {
	//check if automatic backup exists
	existingBak, err := db.GetAutomaticBackupById(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get automatic backup by ID: %v", err)
	}
	if existingBak == nil {
		err := fmt.Errorf("automatic backup with ID %d not found", id)
		sendImportantNotification("DisableAutoBak: not found", err)
		return err
	}

	//check if already disabled
	if !existingBak.Enabled {
		return fmt.Errorf("automatic backup is already disabled")
	}

	//disable in database
	err = db.DisableAutomaticBackupById(ctx, id)
	if err != nil {
		sendImportantNotification("DisableAutoBak: DisableAutomaticBackupById failed", err)
		return fmt.Errorf("failed to disable automatic backup: %v", err)
	}

	return nil
}

// fazer backups
// se sucesso eliminar com GetAutomaticBackups
// ja elimina backups antigos e mantem MaxBackupsRetain
func (v *VirshService) createAutoBak(ctx context.Context, bak db.AutomaticBackup) error {
	if err := v.BackupVM(ctx, bak.VmName, bak.NfsMountId, true); err != nil {
		sendImportantNotification("createAutoBak: BackupVM failed", err)
		return err
	}

	completedAt := time.Now().UTC().Format(time.RFC3339)
	if err := db.UpdateAutomaticBackupTimes(ctx, bak.Id, &completedAt); err != nil {
		sendImportantNotification("createAutoBak: UpdateAutomaticBackupTimes failed", err)
		return fmt.Errorf("failed to update backup timestamp: %v", err)
	}

	//eliminar baks antigos
	baksVm, err := db.GetAutomaticBackups(ctx, bak.VmName)
	if err != nil {
		sendImportantNotification("createAutoBak: GetAutomaticBackups failed", err)
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
			err := v.DeleteBackup(ctx, baksVm[i].Id)
			if err != nil {
				logger.Errorf("failed to delete old backup %d: %v", baksVm[i].Id, err)
				sendImportantNotification(fmt.Sprintf("createAutoBak: failed to delete old backup %d", baksVm[i].Id), err)
			}
		}
	}

	return nil
}
func (v *VirshService) LoopAutomaticBaks(ctx context.Context) {
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
				logger.Errorf("Automatic backup loop panic: %v", r)
				// Optionally restart the loop after a delay
				time.Sleep(time.Minute * 5)
				v.backupLoopRunning.Store(false)
				v.LoopAutomaticBaks(ctx)
			}
		}()

		for {
			logger.Info("running automatic baks")
			nowTime := time.Now()
			currentClock := db.Clock{Hours: nowTime.Hour(), Minutes: nowTime.Minute()}

			baks, err := db.GetEnabledAutomaticBackupsAt(ctx, currentClock)
			if err != nil {
				logger.Error("Error getting automatic backups: " + err.Error())
				sendImportantNotification("LoopAutomaticBaks: GetEnabledAutomaticBackupsAt failed", err)
				time.Sleep(time.Minute * time.Duration(timeDiff))
				continue
			}

			logger.Infof("Processing %d automatic backup(s)", len(baks))

			for _, bak := range baks {
				vm, err := v.GetVmByName(bak.VmName)
				if err != nil {
					logger.Errorf("Failed to resolve VM %s for automatic backup: %v", bak.VmName, err)
					continue
				}
				if vm == nil {
					logger.Warnf("Skipping automatic backup for VM %s because it no longer exists", bak.VmName)
					continue
				}

				machineCon := protocol.GetConnectionByMachineName(vm.MachineName)
				if machineCon == nil || machineCon.Connection == nil {
					logger.Errorf("Failed to get connection for VM %s, slave %s is down", bak.VmName, vm.MachineName)
					sendImportantNotification("LoopAutomaticBaks: slave down for VM", fmt.Errorf("VM %s on slave %s is down", bak.VmName, vm.MachineName))
					continue
				}

				// Verify we're actually within the backup's time window
				if !currentClock.IsBetween(bak.MinTime, bak.MaxTime) {
					logger.Infof("Skipping backup for VM %s - outside time window (current: %s, window: %s-%s)",
						bak.VmName, currentClock.String(), bak.MinTime.String(), bak.MaxTime.String())
					continue
				}

				minimalLastBak := nowTime.Add(-time.Duration(bak.FrequencyDays) * 24 * time.Hour)

				shouldBackup := false

				if bak.LastBackupTime == nil {
					shouldBackup = true
				} else {
					lastBakTime, ok := parseBackupTimestamp(*bak.LastBackupTime)
					if !ok {
						logger.Errorf("Error parsing last backup time for VM %s: %s", bak.VmName, *bak.LastBackupTime)
						continue
					}

					if lastBakTime.Before(minimalLastBak) {
						shouldBackup = true
					}
				}

				if shouldBackup {
					logger.Infof("Creating automatic backup for VM %s", bak.VmName)
					err = v.createAutoBak(ctx, bak)
					if err != nil {
						logger.Errorf("Failed to create automatic backup for VM %s: %v", bak.VmName, err)
						sendImportantNotification("LoopAutomaticBaks: createAutoBak failed", fmt.Errorf("VM %s: %v", bak.VmName, err))
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
