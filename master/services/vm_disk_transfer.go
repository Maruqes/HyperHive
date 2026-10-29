package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"512SvMan/db"
	"512SvMan/nfs"
	"512SvMan/protocol"
	"512SvMan/virsh"
	vp "github.com/Maruqes/512SvMan/api/proto/virsh"
	pb "github.com/Maruqes/512SvMan/api/proto/vm_disk"
	"google.golang.org/grpc"
)

var diskWorkers sync.Map
var managedDiskName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

const MaxDiskImportBytes int64 = 1 << 40

func diskWorker(id string) (*sync.Mutex, error) {
	value, _ := diskWorkers.LoadOrStore(id, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	if !lock.TryLock() {
		return nil, fmt.Errorf("operation is still running")
	}
	return lock, nil
}
func diskPhase(ctx context.Context, op *db.DiskOperation, phase string) error {
	previous := op.Phase
	op.Phase = phase
	op.Error = ""
	if err := db.SaveDiskOperation(ctx, op, previous); err != nil {
		op.Phase = previous
		return err
	}
	return nil
}
func diskFailure(op *db.DiskOperation, err error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Reload after a transaction whose acknowledgement may have been interrupted.
	current, e := db.GetDiskOperation(ctx, op.ID)
	if e == nil {
		if current.Phase == "complete" || current.Phase == "aborted" {
			return fmt.Errorf("operation %s: %w", op.ID, err)
		}
		current.Error = err.Error()
		e = db.SaveDiskOperation(ctx, current, current.Phase)
	}
	if e != nil {
		return fmt.Errorf("operation %s: %v; status persistence failed: %w", op.ID, err, e)
	}
	return fmt.Errorf("operation %s requires reconciliation: %w", op.ID, err)
}
func transferClient(ctx context.Context, s *VMDiskService, nfsID int, expectedBase string, expected *db.NFSShare) (pb.VMDiskServiceClient, string, error) {
	share, err := db.GetNFSShareByID(ctx, nfsID)
	if err != nil {
		return nil, "", err
	}
	if share == nil {
		return nil, "", fmt.Errorf("registered share missing")
	}
	if expected.Id != 0 && *expected != *share {
		return nil, "", fmt.Errorf("registered share identity changed; manual reconciliation required")
	}
	*expected = *share
	conn, base, err := s.readyNFSConnection(ctx, nfsID)
	if err != nil {
		return nil, "", err
	}
	after, err := db.GetNFSShareByID(ctx, nfsID)
	if err != nil {
		return nil, "", err
	}
	if after == nil || *after != *expected || conn.MachineName != expected.MachineName || base != strings.TrimSuffix(expected.Target, "/") {
		return nil, "", fmt.Errorf("share identity changed during resolution")
	}
	if expectedBase != "" && base != expectedBase {
		return nil, "", fmt.Errorf("share mount path changed; manual reconciliation required")
	}
	return pb.NewVMDiskServiceClient(conn.Connection), base, nil
}
func unchangedDiskShare(ctx context.Context, expected db.NFSShare) error {
	current, err := db.GetNFSShareByID(ctx, expected.Id)
	if err != nil {
		return err
	}
	if current == nil || *current != expected {
		return fmt.Errorf("registered share identity changed; preserve both copies for reconciliation")
	}
	return nil
}

func transferRequest(op *db.DiskOperation, base, action string) *pb.DiskTransferRequest {
	return &pb.DiskTransferRequest{OperationID: op.ID, BasePath: base, Name: op.Name, Length: op.Length, Sha256: op.SHA256, Action: action}
}
func streamLength(stream grpc.ServerStreamingClient[pb.VMDiskChunk]) (int64, error) {
	header, err := stream.Header()
	if err != nil {
		return 0, err
	}
	values := header.Get("disk-length")
	if len(values) != 1 {
		return 0, fmt.Errorf("agent does not support framed disk downloads; upgrade agent")
	}
	length, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || length <= 0 {
		return 0, fmt.Errorf("invalid agent disk length")
	}
	return length, nil
}

func (s *VMDiskService) Import(ctx context.Context, name string, nfsID int, length int64, body io.Reader) (*VMDiskResult, error) {
	if length <= 0 || length > MaxDiskImportBytes || !managedDiskName.MatchString(name) {
		return nil, fmt.Errorf("valid name and file length between 1 byte and 1 TiB required")
	}
	exists, err := db.DoesVMDiskNameExist(ctx, name)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("disk name already exists")
	}
	destinationShare := db.NFSShare{}
	client, base, err := transferClient(ctx, s, nfsID, "", &destinationShare)
	if err != nil {
		return nil, err
	}
	op := &db.DiskOperation{Kind: "import", Name: name, DestinationNFS: nfsID, DestinationBase: base, Length: length, DestinationShare: destinationShare}
	if err = db.BeginDiskOperation(ctx, op); err != nil {
		return nil, err
	}
	lock, err := diskWorker(op.ID)
	if err != nil {
		if abortErr := db.AbortDiskOperation(context.Background(), op); abortErr != nil {
			return nil, fmt.Errorf("%v; failed to release reservation: %w", err, abortErr)
		}
		return nil, err
	}
	defer lock.Unlock()
	transferStarted := false
	result, err := func() (*VMDiskResult, error) {
		if err := diskPhase(ctx, op, "uploading"); err != nil {
			return nil, err
		}
		transferCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		transferStarted = true
		upload, err := client.UploadVMDisk(transferCtx)
		if err != nil {
			return nil, err
		}
		if err = upload.Send(&pb.DiskUploadChunk{Transfer: transferRequest(op, base, "")}); err != nil {
			return nil, err
		}
		h := sha256.New()
		buf := make([]byte, 256*1024)
		var count int64
		for {
			n, e := body.Read(buf)
			if n > 0 {
				if int64(n) > length-count {
					return nil, fmt.Errorf("upload exceeds declared length")
				}
				h.Write(buf[:n])
				count += int64(n)
				if err = upload.Send(&pb.DiskUploadChunk{Data: buf[:n]}); err != nil {
					_, e := upload.CloseAndRecv()
					if e != nil {
						return nil, e
					}
					return nil, err
				}
			}
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, e
			}
			if e = ctx.Err(); e != nil {
				return nil, e
			}
		}
		if count != length {
			return nil, fmt.Errorf("short upload: got %d of %d bytes", count, length)
		}
		op.SHA256 = hex.EncodeToString(h.Sum(nil))
		// Persist expected checksum before publication may occur on the agent.
		if err = db.SaveDiskOperation(ctx, op, op.Phase); err != nil {
			return nil, err
		}
		response, err := upload.CloseAndRecv()
		if err != nil {
			return nil, err
		}
		if response.Length != length || response.Sha256 != op.SHA256 {
			return nil, fmt.Errorf("import verification failed")
		}
		op.DestinationPath = response.GetDisk().GetDiskPath()
		if err = diskPhase(ctx, op, "published"); err != nil {
			return nil, err
		}
		return s.finishImport(ctx, op, response)
	}()
	if err != nil {
		if !transferStarted {
			if abortErr := db.AbortDiskOperation(context.Background(), op); abortErr == nil {
				return nil, err
			} else {
				return nil, fmt.Errorf("%v; failed to release reservation: %w", err, abortErr)
			}
		}
		return nil, diskFailure(op, err)
	}
	return result, nil
}
func (s *VMDiskService) finishImport(ctx context.Context, op *db.DiskOperation, response *pb.DiskTransferResponse) (*VMDiskResult, error) {
	if err := unchangedDiskShare(ctx, op.DestinationShare); err != nil {
		return nil, err
	}

	disk := response.GetDisk()
	if disk == nil || !disk.Ok || response.Sha256 != op.SHA256 || response.Length != op.Length {
		return nil, fmt.Errorf("published image does not match operation")
	}
	record := &db.VMDisk{NFSID: op.DestinationNFS, DiskPath: disk.DiskPath, FolderPath: disk.FolderPath, Format: disk.Format, SizeGB: disk.SizeGB}
	if err := db.CommitDiskImport(ctx, op, record); err != nil {
		return nil, err
	}
	result := convertVMDiskResponse(disk)
	result.Id = op.DiskID
	result.Name = op.Name
	result.NFSID = op.DestinationNFS
	return result, nil
}

func (s *VMDiskService) Move(ctx context.Context, id, nfsID int) (*db.DiskOperation, error) {
	disk, err := db.GetVMDiskByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if disk == nil {
		return nil, fmt.Errorf("disk not found")
	}
	if disk.NFSID == nfsID {
		return nil, fmt.Errorf("destination must be a different NFS share")
	}
	if _, err = stoppedDiskVM(ctx, disk); err != nil {
		return nil, err
	}
	sourceShare, destinationShare := db.NFSShare{}, db.NFSShare{}
	_, source, err := transferClient(ctx, s, disk.NFSID, "", &sourceShare)
	if err != nil {
		return nil, err
	}
	destination, base, err := transferClient(ctx, s, nfsID, "", &destinationShare)
	if err != nil {
		return nil, err
	}
	op := &db.DiskOperation{Kind: "move", Name: disk.Name, DiskID: id, SourceNFS: disk.NFSID, DestinationNFS: nfsID, SourceBase: source, DestinationBase: base, SourcePath: disk.DiskPath, VMName: disk.AttachedVMName, MachineName: disk.AttachedMachineName, SourceShare: sourceShare, DestinationShare: destinationShare}
	if err = db.BeginDiskOperation(ctx, op); err != nil {
		return nil, err
	}
	lock, err := diskWorker(op.ID)
	if err != nil {
		if abortErr := db.AbortDiskOperation(context.Background(), op); abortErr != nil {
			return nil, fmt.Errorf("%v; failed to release reservation: %w", err, abortErr)
		}
		return nil, err
	}
	defer lock.Unlock()
	transferStarted := false
	err = func() error {
		// Reload after reservation to close attachment/mutation races.
		current, err := db.GetVMDiskByID(ctx, id)
		if err != nil {
			return err
		}
		if current == nil || *current != *disk {
			return fmt.Errorf("disk changed before reservation")
		}
		if _, err = stoppedDiskVM(ctx, disk); err != nil {
			return err
		}
		if err = diskPhase(ctx, op, "copying"); err != nil {
			return err
		}
		transferCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		registered, download, err := s.Download(transferCtx, id)
		if err != nil {
			return err
		}
		if registered.DiskPath != op.SourcePath {
			return fmt.Errorf("source metadata changed")
		}
		length, err := streamLength(download)
		if err != nil {
			return err
		}
		op.Length = length
		if err = db.SaveDiskOperation(ctx, op, op.Phase); err != nil {
			return err
		}
		transferStarted = true
		upload, err := destination.UploadVMDisk(transferCtx)
		if err != nil {
			return err
		}
		if err = upload.Send(&pb.DiskUploadChunk{Transfer: transferRequest(op, base, "")}); err != nil {
			return err
		}
		h := sha256.New()
		var count int64
		for {
			chunk, e := download.Recv()
			if e == io.EOF {
				break
			}
			if e != nil {
				return e
			}
			if len(chunk.Data) > 256*1024 || int64(len(chunk.Data)) > length-count {
				return fmt.Errorf("invalid source chunk or length")
			}
			h.Write(chunk.Data)
			count += int64(len(chunk.Data))
			if err = upload.Send(&pb.DiskUploadChunk{Data: chunk.Data}); err != nil {
				_, e := upload.CloseAndRecv()
				if e != nil {
					return e
				}
				return err
			}
		}
		if count != length {
			return io.ErrUnexpectedEOF
		}
		op.SHA256 = hex.EncodeToString(h.Sum(nil))
		if err = db.SaveDiskOperation(ctx, op, op.Phase); err != nil {
			return err
		}
		result, err := upload.CloseAndRecv()
		if err != nil {
			return err
		}
		if result.Sha256 != op.SHA256 || result.Length != op.Length {
			return fmt.Errorf("destination checksum mismatch")
		}
		op.DestinationPath = result.GetDisk().GetDiskPath()
		if err = diskPhase(ctx, op, "published"); err != nil {
			return err
		}
		verified, err := destination.TransferVMDisk(ctx, transferRequest(op, base, "inspect"))
		if err != nil {
			return err
		}
		return s.finishMove(ctx, op, verified)
	}()
	if err != nil {
		current, reloadErr := db.GetDiskOperation(context.Background(), op.ID)
		if reloadErr == nil && (current.Phase == "reserved" || current.Phase == "copying") && current.SHA256 == "" {
			if abortErr := db.AbortDiskOperation(context.Background(), current); abortErr == nil {
				return current, err
			}
		}
		return op, diskFailure(op, err)
	}
	return op, nil
}

func stoppedDiskVM(ctx context.Context, disk *db.VMDisk) (*grpc.ClientConn, error) {
	if disk.AttachedVMName == "" {
		return nil, nil
	}
	if disk.AttachedMachineName == "" {
		return nil, fmt.Errorf("attached VM host is unknown")
	}
	conn := protocol.GetConnectionByMachineName(disk.AttachedMachineName)
	if conn == nil || conn.Connection == nil {
		return nil, fmt.Errorf("attached VM host is unreachable")
	}
	vm, err := vp.NewSlaveVirshServiceClient(conn.Connection).GetVmByName(ctx, &vp.GetVmByNameRequest{Name: disk.AttachedVMName})
	if err != nil {
		return nil, err
	}
	if vm == nil || vm.State != vp.VmState_SHUTOFF {
		return nil, fmt.Errorf("attached VM must be confirmed shut off")
	}
	return conn.Connection, nil
}

// Rewrite only the source attribute of one disk. Every other XML token survives,
// including unknown libvirt devices and secure settings.
func relocateDiskXML(input, source, destination string) (string, error) {
	decoder := xml.NewDecoder(strings.NewReader(input))
	var output strings.Builder
	encoder := xml.NewEncoder(&output)
	stack := []string{}
	matches := 0
	destinations := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Local == "source" && len(stack) >= 3 && stack[len(stack)-1] == "disk" && stack[len(stack)-2] == "devices" {
				for i := range t.Attr {
					if t.Attr[i].Name.Local == "file" {
						if t.Attr[i].Value == destination {
							destinations++
						}
						if t.Attr[i].Value == source {
							t.Attr[i].Value = destination
							matches++
						}
					}
				}
			}
			stack = append(stack, t.Name.Local)
			token = t
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
		if err = encoder.EncodeToken(token); err != nil {
			return "", err
		}
	}
	if matches != 1 || destinations != 0 {
		return "", fmt.Errorf("expected exactly one source disk and no destination disk")
	}
	if err := encoder.Flush(); err != nil {
		return "", err
	}
	return output.String(), nil
}
func verifyDiskXML(input, source, destination string) error {
	_, err := relocateDiskXML(input, destination, source)
	return err
}
func (s *VMDiskService) finishMove(ctx context.Context, op *db.DiskOperation, response *pb.DiskTransferResponse) error {
	if err := unchangedDiskShare(ctx, op.DestinationShare); err != nil {
		return err
	}
	if err := unchangedDiskShare(ctx, op.SourceShare); err != nil {
		return err
	}

	if response.Sha256 != op.SHA256 || response.Length != op.Length || response.GetDisk() == nil || !response.Disk.Ok {
		return fmt.Errorf("published image does not match operation")
	}
	op.DestinationPath = response.Disk.DiskPath
	disk, err := db.GetVMDiskByID(ctx, op.DiskID)
	if err != nil {
		return err
	}
	if disk == nil {
		return fmt.Errorf("disk metadata missing")
	}
	if op.Phase == "committed" || op.Phase == "cleanup-required" {
		if disk.NFSID != op.DestinationNFS || disk.DiskPath != op.DestinationPath {
			return fmt.Errorf("committed metadata differs; preserving both copies")
		}
		if op.VMName != "" {
			conn, e := stoppedDiskVM(ctx, disk)
			if e != nil {
				return e
			}
			snapshot, e := virsh.GetVMSnapshot(ctx, conn, op.VMName)
			if e != nil {
				return e
			}
			if e = verifyDiskXML(snapshot.InactiveXml, op.SourcePath, op.DestinationPath); e != nil {
				return e
			}
		}
	} else {
		if disk.NFSID != op.SourceNFS || disk.DiskPath != op.SourcePath || disk.AttachedVMName != op.VMName || disk.AttachedMachineName != op.MachineName {
			return fmt.Errorf("source metadata differs; preserving both copies")
		}
		if op.VMName != "" {
			conn, err := stoppedDiskVM(ctx, disk)
			if err != nil {
				return err
			}
			snapshot, err := virsh.GetVMSnapshot(ctx, conn, op.VMName)
			if err != nil {
				return err
			}
			if snapshot.Active {
				return fmt.Errorf("VM must remain stopped")
			}
			if err = nfs.CheckFileReadable(conn, op.DestinationPath); err != nil {
				return fmt.Errorf("destination is not readable on VM host: %w", err)
			}
			if verifyDiskXML(snapshot.InactiveXml, op.SourcePath, op.DestinationPath) != nil {
				rewritten, err := relocateDiskXML(snapshot.InactiveXml, op.SourcePath, op.DestinationPath)
				if err != nil {
					return err
				}
				// Write intent before RPC: an interrupted acknowledgement is reconciled by reading XML.
				if err = diskPhase(ctx, op, "definition-pending"); err != nil {
					return err
				}
				_, err = vp.NewSlaveVirshServiceClient(conn).UpdateVMXml(ctx, &vp.UpdateVMXmlRequest{Name: op.VMName, VmXML: rewritten})
				if err != nil {
					return err
				}
				snapshot, err = virsh.GetVMSnapshot(ctx, conn, op.VMName)
				if err != nil {
					return err
				}
				if err = verifyDiskXML(snapshot.InactiveXml, op.SourcePath, op.DestinationPath); err != nil {
					return err
				}
			}
		}
		if err = diskPhase(ctx, op, "definition-verified"); err != nil {
			return err
		}
		destination := &db.VMDisk{NFSID: op.DestinationNFS, DiskPath: response.Disk.DiskPath, FolderPath: response.Disk.FolderPath}
		if err = db.CommitDiskMove(ctx, op, disk, destination); err != nil {
			return err
		}
	}
	// Commit and definition checks precede every source deletion retry.
	source, _, err := transferClient(ctx, s, op.SourceNFS, op.SourceBase, &op.SourceShare)
	if err != nil {
		_ = diskPhase(ctx, op, "cleanup-required")
		return err
	}
	if _, err = source.TransferVMDisk(ctx, transferRequest(op, op.SourceBase, "remove-source")); err != nil {
		_ = diskPhase(ctx, op, "cleanup-required")
		return err
	}
	return diskPhase(ctx, op, "complete")
}

func (s *VMDiskService) Reconcile(ctx context.Context, id, action string) (*db.DiskOperation, error) {
	lock, err := diskWorker(id)
	if err != nil {
		return nil, err
	}
	defer lock.Unlock()
	op, err := db.GetDiskOperation(ctx, id)
	if err != nil {
		return nil, err
	}
	if op.Phase == "complete" || op.Phase == "aborted" {
		return op, nil
	}
	if op.Kind != "move" && op.Kind != "import" {
		return nil, fmt.Errorf("review uncertain %s outcome using the operator procedure", op.Kind)
	}
	err = func() error {
		client, _, err := transferClient(ctx, s, op.DestinationNFS, op.DestinationBase, &op.DestinationShare)
		if err != nil {
			return err
		}
		if action == "abort" {
			if op.Phase != "reserved" && op.Phase != "uploading" && op.Phase != "copying" {
				return fmt.Errorf("publication or commit may have occurred; finish reconciliation instead")
			}
			// The agent rejects abort while an upload worker is active and refuses if published.
			if _, err = client.TransferVMDisk(ctx, transferRequest(op, op.DestinationBase, "discard-stage")); err != nil {
				return err
			}
			return diskPhase(ctx, op, "aborted")
		}
		if action != "finish" {
			return fmt.Errorf("action must be finish or abort")
		}
		result, err := client.TransferVMDisk(ctx, transferRequest(op, op.DestinationBase, "inspect"))
		if err != nil {
			return err
		}
		if op.SHA256 == "" || result.Sha256 != op.SHA256 || result.Length != op.Length {
			return fmt.Errorf("expected checksum unavailable or differs; preserve copies for manual review")
		}
		if op.Kind == "import" {
			_, err = s.finishImport(ctx, op, result)
			return err
		}
		return s.finishMove(ctx, op, result)
	}()
	if err != nil {
		if !transferStarted {
			if abortErr := db.AbortDiskOperation(context.Background(), op); abortErr == nil {
				return op, err
			} else {
				return op, fmt.Errorf("%v; failed to release reservation: %w", err, abortErr)
			}
		}
		return op, diskFailure(op, err)
	}
	return op, nil
}

func finishDiskMutation(op *db.DiskOperation, err error) error {
	if err != nil {
		return diskFailure(op, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = diskPhase(ctx, op, "complete"); err != nil {
		return fmt.Errorf("operation %s completion persistence failed: %w", op.ID, err)
	}
	return nil
}

// Verify the catalog points at the conventional managed file resolved by the agent.
func checkManagedDiskPath(disk *db.VMDisk, base string) error {
	extension := disk.Format
	if extension == "qcow" {
		extension = "qcow2"
	}
	expected := filepath.Join(base, "vm_disk_"+disk.Name, disk.Name+"."+extension)
	if filepath.Clean(disk.DiskPath) != expected {
		return errors.New("registered disk path differs from managed disk location")
	}
	return nil
}
