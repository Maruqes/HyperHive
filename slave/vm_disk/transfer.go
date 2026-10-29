package vmdisk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	pb "github.com/Maruqes/512SvMan/api/proto/vm_disk"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

var transferWorkers sync.Map

func lockTransfer(id string) (*sync.Mutex, error) {
	value, _ := transferWorkers.LoadOrStore(id, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	if !lock.TryLock() {
		return nil, fmt.Errorf("transfer worker is still active")
	}
	return lock, nil
}

type transferReceipt struct {
	OperationID string `json:"operation_id"`
	Name        string `json:"name"`
	Length      int64  `json:"length"`
	SHA256      string `json:"sha256"`
	Format      string `json:"format"`
}

func transferPaths(req *pb.DiskTransferRequest) (base, stage, final string, err error) {
	if _, err = uuid.Parse(req.OperationID); err != nil {
		return "", "", "", fmt.Errorf("invalid operation ID")
	}
	if err = validateVMDiskName(req.Name); err != nil {
		return
	}
	base, err = cleanExistingBasePath(req.BasePath)
	if err != nil {
		return
	}
	stage = filepath.Join(base, ".vm-transfer-"+req.OperationID)
	final = filepath.Join(base, "vm_disk_"+req.Name)
	return
}

func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func regularFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("not a regular disk file: %s", path)
	}
	return f, nil
}

func hashFile(ctx context.Context, path string) (string, int64, error) {
	f, err := regularFile(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 256*1024)
	var length int64
	for {
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
		n, e := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			length += int64(n)
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", 0, e
		}
	}
	return hex.EncodeToString(h.Sum(nil)), length, nil
}

// Reject resource-bearing qcow2 headers before asking QEMU to parse the image.
// QEMU receives an explicit local file driver, never a user-provided protocol URL.
func validateTransferImage(ctx context.Context, path string) (*qemuImgInfo, error) {
	info, err := transferImageInfo(ctx, path)
	if err != nil {
		return nil, err
	}
	if info.Format == "qcow2" {
		if out, err := exec.CommandContext(ctx, "qemu-img", "check", "-f", "qcow2", path).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("qcow2 consistency check failed: %s: %w", strings.TrimSpace(string(out)), err)
		}
	}
	return info, nil
}
func transferImageInfo(ctx context.Context, path string) (*qemuImgInfo, error) {
	f, err := regularFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	header := make([]byte, 512)
	n, err := f.ReadAt(header, 0)
	if err != nil && err != io.EOF {
		return nil, err
	}
	header = header[:n]
	format := "raw"
	if len(header) >= 4 && bytes.Equal(header[:4], []byte{'Q', 'F', 'I', 0xfb}) {
		if len(header) < 72 {
			return nil, fmt.Errorf("truncated qcow2 header")
		}
		version := binary.BigEndian.Uint32(header[4:8])
		if version != 2 && version != 3 {
			return nil, fmt.Errorf("unsupported qcow2 version")
		}
		if binary.BigEndian.Uint64(header[8:16]) != 0 || binary.BigEndian.Uint32(header[16:20]) != 0 || binary.BigEndian.Uint32(header[32:36]) != 0 {
			return nil, fmt.Errorf("backing files and encryption are not supported")
		}
		if version == 3 && (len(header) < 104 || binary.BigEndian.Uint64(header[72:80]) & ^uint64(1) != 0) {
			return nil, fmt.Errorf("external data files or unsupported qcow2 features")
		}
		format = "qcow2"
	} else {
		// Raw has no signature; reject known container formats rather than importing
		// them as raw and later attaching them with the wrong driver.
		for _, magic := range [][]byte{[]byte("KDMV"), []byte("COWD"), []byte("QED"), []byte("LUKS"), []byte("# Disk DescriptorFile"), []byte("<<< Oracle VM VirtualBox Disk Image >>>"), []byte("vhdxfile")} {
			if bytes.HasPrefix(header, magic) {
				return nil, fmt.Errorf("unsupported image format")
			}
		}
		st, e := f.Stat()
		if e != nil {
			return nil, e
		}
		if st.Size() >= 512 {
			tail := make([]byte, 8)
			if _, e = f.ReadAt(tail, st.Size()-512); e == nil && string(tail) == "conectix" {
				return nil, fmt.Errorf("VHD images are not supported")
			}
		}
	}
	out, err := exec.CommandContext(ctx, "qemu-img", "info", "--output=json", "-f", format, path).Output()
	if err != nil {
		return nil, fmt.Errorf("invalid %s image: %w", format, err)
	}
	var info qemuImgInfo
	if err = json.Unmarshal(out, &info); err != nil {
		return nil, err
	}
	if info.Format != format || info.VirtualSize <= 0 {
		return nil, fmt.Errorf("invalid image format or virtual size")
	}

	return &info, nil
}

func writeReceipt(folder string, receipt transferReceipt) error {
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(folder, ".transfer.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func readReceipt(folder string, req *pb.DiskTransferRequest) (*transferReceipt, error) {
	st, err := os.Lstat(folder)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("invalid transfer folder")
	}
	f, err := regularFile(filepath.Join(folder, ".transfer.json"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r transferReceipt
	if err = json.NewDecoder(io.LimitReader(f, 4096)).Decode(&r); err != nil {
		return nil, err
	}
	if r.OperationID != req.OperationID || r.Name != req.Name {
		return nil, fmt.Errorf("transfer ownership mismatch")
	}
	if r.Length <= 0 || (r.Format != "" && r.Format != "raw" && r.Format != "qcow2") {
		return nil, fmt.Errorf("invalid transfer receipt")
	}
	if r.SHA256 != "" {
		decoded, err := hex.DecodeString(r.SHA256)
		if err != nil || len(decoded) != sha256.Size {
			return nil, fmt.Errorf("invalid receipt digest")
		}
	}
	return &r, nil
}

func (s *Service) UploadVMDisk(stream pb.VMDiskService_UploadVMDiskServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	req := first.GetTransfer()
	if req == nil || len(first.Data) != 0 || req.Length <= 0 {
		return fmt.Errorf("transfer metadata and positive length required")
	}
	lock, err := lockTransfer(req.OperationID)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	base, stage, final, err := transferPaths(req)
	if err != nil {
		return err
	}
	var stat syscall.Statfs_t
	if err = syscall.Statfs(base, &stat); err != nil {
		return err
	}
	if uint64(req.Length) > stat.Bavail*uint64(stat.Bsize) {
		return fmt.Errorf("insufficient free space for %d file bytes", req.Length)
	}
	if err = os.Mkdir(stage, 0700); err != nil {
		return err
	}
	// Retain owned staging on every failure; reconciliation is explicit and cannot
	// race an old upload worker after a timeout or master restart.
	receipt := transferReceipt{OperationID: req.OperationID, Name: req.Name, Length: req.Length}
	if err = writeReceipt(stage, receipt); err != nil {
		return err
	}
	if err = syncDir(base); err != nil {
		return err
	}
	path := filepath.Join(stage, "image")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	var received int64
	for {
		chunk, e := stream.Recv()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if chunk.Transfer != nil || len(chunk.Data) == 0 || len(chunk.Data) > 256*1024 || int64(len(chunk.Data)) > req.Length-received {
			return fmt.Errorf("invalid upload chunk or length")
		}
		h.Write(chunk.Data)
		if bytes.Count(chunk.Data, []byte{0}) == len(chunk.Data) {
			_, err = f.Seek(int64(len(chunk.Data)), io.SeekCurrent)
		} else {
			var n int
			n, err = f.Write(chunk.Data)
			if err == nil && n != len(chunk.Data) {
				err = io.ErrShortWrite
			}
		}
		if err != nil {
			return err
		}
		received += int64(len(chunk.Data))
	}
	if received != req.Length {
		return fmt.Errorf("short upload: got %d of %d bytes", received, req.Length)
	}
	if err = f.Truncate(received); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	receipt.SHA256 = hex.EncodeToString(h.Sum(nil))
	if req.Sha256 != "" && receipt.SHA256 != req.Sha256 {
		return fmt.Errorf("disk checksum mismatch")
	}
	info, err := validateTransferImage(stream.Context(), path)
	if err != nil {
		return err
	}
	receipt.Format = info.Format
	imageName := req.Name + "." + info.Format
	if err = os.Rename(path, filepath.Join(stage, imageName)); err != nil {
		return err
	}
	if err = f.Chmod(0666); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Replace only this operation's receipt inside its exclusively created folder.
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	receiptFile, err := os.OpenFile(filepath.Join(stage, ".receipt-new"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = receiptFile.Write(data)
	if err == nil {
		err = receiptFile.Sync()
	}
	closeErr := receiptFile.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(filepath.Join(stage, ".receipt-new"), filepath.Join(stage, ".transfer.json")); err != nil {
		return err
	}
	if err = os.Chmod(stage, 0777); err != nil {
		return err
	}
	if err = syncDir(stage); err != nil {
		return err
	}
	if err = publishTransfer(base, stage, final, imageName); err != nil {
		return err
	}
	if err = syncDir(base); err != nil {
		return err
	}
	if err = cleanupPublishedStage(stage, final, req); err != nil {
		return err
	}
	disk := diskFromQemuInfo(filepath.Join(final, imageName), final, info)
	return stream.SendAndClose(&pb.DiskTransferResponse{Disk: vmDiskResponse(disk), Length: received, Sha256: receipt.SHA256, Phase: "published"})
}

func (s *Service) TransferVMDisk(ctx context.Context, req *pb.DiskTransferRequest) (*pb.DiskTransferResponse, error) {
	lock, err := lockTransfer(req.OperationID)
	if err != nil {
		return nil, err
	}
	defer lock.Unlock()
	base, stage, final, err := transferPaths(req)
	if err != nil {
		return nil, err
	}
	switch req.Action {
	case "capacity":
		var st syscall.Statfs_t
		if err = syscall.Statfs(base, &st); err != nil {
			return nil, err
		}
		return &pb.DiskTransferResponse{FreeBytes: st.Bavail * uint64(st.Bsize)}, nil
	case "inspect":
		r, e := readReceipt(final, req)
		if e != nil {
			return nil, e
		}
		path := filepath.Join(final, req.Name+"."+r.Format)
		if _, e := os.Lstat(path); os.IsNotExist(e) {
			staged, e := readReceipt(stage, req)
			if e != nil {
				return nil, e
			}
			if *staged != *r {
				return nil, fmt.Errorf("staging receipt differs")
			}
			source := filepath.Join(stage, req.Name+"."+r.Format)
			hash, length, e := hashFile(ctx, source)
			if e != nil {
				return nil, e
			}
			if hash != r.SHA256 || length != r.Length {
				return nil, fmt.Errorf("staging content differs")
			}
			if e = os.Link(source, path); e != nil {
				return nil, e
			}
			if e = syncDir(final); e != nil {
				return nil, e
			}
			if e = syncDir(base); e != nil {
				return nil, e
			}
		}
		hash, length, e := hashFile(ctx, path)
		if e != nil {
			return nil, e
		}
		if hash != r.SHA256 || length != r.Length {
			return nil, fmt.Errorf("published content differs from receipt")
		}
		info, e := validateTransferImage(ctx, path)
		if e != nil {
			return nil, e
		}
		if e = cleanupPublishedStage(stage, final, req); e != nil {
			return nil, e
		}
		return &pb.DiskTransferResponse{Disk: vmDiskResponse(diskFromQemuInfo(path, final, info)), Length: length, Sha256: hash, Phase: "published"}, nil
	case "discard-stage":
		if _, e := os.Lstat(final); e == nil {
			return nil, fmt.Errorf("destination exists; inspect publication before abort")
		} else if !os.IsNotExist(e) {
			return nil, e
		}
		if _, err = readReceipt(stage, req); errors.Is(err, os.ErrNotExist) {
			if _, e := os.Lstat(stage); os.IsNotExist(e) {
				return &pb.DiskTransferResponse{Phase: "absent"}, nil
			}
			return nil, fmt.Errorf("staging folder exists without ownership receipt; manual review required")
		}
		if err != nil {
			return nil, err
		}
		entries, e := os.ReadDir(stage)
		if e != nil {
			return nil, e
		}
		allowed := map[string]bool{"image": true, req.Name + ".raw": true, req.Name + ".qcow2": true, ".receipt-new": true, ".transfer.json": true}
		for _, entry := range entries {
			if !allowed[entry.Name()] || entry.IsDir() {
				return nil, fmt.Errorf("unknown staging contents; manual review required")
			}
		}
		// Caller must first establish the upload worker has ended; no expiry grants ownership.
		for _, file := range []string{"image", req.Name + ".raw", req.Name + ".qcow2", ".receipt-new", ".transfer.json"} {
			if err = os.Remove(filepath.Join(stage, file)); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		}
		if err = os.Remove(stage); err != nil {
			return nil, err
		}
		if err = syncDir(base); err != nil {
			return nil, err
		}
		return &pb.DiskTransferResponse{Phase: "discarded"}, nil
	case "remove-source":
		if err = removeTransferSource(ctx, base, final, req); err != nil {
			return nil, err
		}
		return &pb.DiskTransferResponse{Phase: "removed"}, nil
	default:
		return nil, fmt.Errorf("unknown transfer action %q", strings.TrimSpace(req.Action))
	}
}

// The per-operation source journal survives removal and prevents delayed cleanup
// retries from deleting a newly created disk that reuses the old name and bytes.
type sourceRemoval struct {
	OperationID    string
	Name           string
	SHA256         string
	Length         int64
	DirectoryInode uint64
	Device         uint64
	Inode          uint64
	Path           string
}

func removeTransferSource(ctx context.Context, base, folder string, req *pb.DiskTransferRequest) error {
	if req.Sha256 == "" || req.Length <= 0 {
		return fmt.Errorf("verified source checksum and length required")
	}
	journal := filepath.Join(base, ".vm-source-"+req.OperationID+".json")
	var record sourceRemoval
	file, err := regularFile(journal)
	if err == nil {
		err = json.NewDecoder(io.LimitReader(file, 4096)).Decode(&record)
		file.Close()
		if err != nil {
			return err
		}
	} else if os.IsNotExist(err) {
		st, e := os.Lstat(folder)
		if e != nil || !st.IsDir() {
			return fmt.Errorf("source folder unavailable before ownership recorded")
		}
		var path string
		for _, format := range []string{"raw", "qcow2"} {
			p := filepath.Join(folder, req.Name+"."+format)
			if _, e = os.Lstat(p); e == nil {
				if path != "" {
					return fmt.Errorf("ambiguous source")
				}
				path = p
			}
		}
		if path == "" {
			return fmt.Errorf("source missing before ownership recorded")
		}
		hash, length, e := hashFile(ctx, path)
		if e != nil {
			return e
		}
		if hash != req.Sha256 || length != req.Length {
			return fmt.Errorf("source changed; preserving both copies")
		}
		st, e = os.Lstat(path)
		if e != nil {
			return e
		}
		identity := st.Sys().(*syscall.Stat_t)
		directoryInfo, e := os.Lstat(folder)
		if e != nil {
			return e
		}
		directoryIdentity := directoryInfo.Sys().(*syscall.Stat_t)
		record = sourceRemoval{OperationID: req.OperationID, Name: req.Name, SHA256: req.Sha256, Length: req.Length, DirectoryInode: directoryIdentity.Ino, Device: uint64(identity.Dev), Inode: identity.Ino, Path: path}
		data, e := json.Marshal(record)
		if e != nil {
			return e
		}
		f, e := os.OpenFile(journal, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		_, e = f.Write(data)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		if e = syncDir(base); e != nil {
			return e
		}
	} else {
		return err
	}
	if record.OperationID != req.OperationID || record.Name != req.Name || record.SHA256 != req.Sha256 || record.Length != req.Length || (record.Path != filepath.Join(folder, req.Name+".raw") && record.Path != filepath.Join(folder, req.Name+".qcow2")) {
		return fmt.Errorf("source ownership mismatch")
	}
	// A durable done marker makes all future retries harmless, even after name reuse.
	done := journal + ".done"
	if st, e := os.Lstat(done); e == nil {
		if !st.Mode().IsRegular() {
			return fmt.Errorf("invalid removal marker")
		}
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	if directoryInfo, e := os.Lstat(folder); e == nil {
		if !directoryInfo.IsDir() || directoryInfo.Sys().(*syscall.Stat_t).Ino != record.DirectoryInode {
			return fmt.Errorf("source directory identity changed")
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if st, e := os.Lstat(record.Path); e == nil {
		identity := st.Sys().(*syscall.Stat_t)
		if !st.Mode().IsRegular() || uint64(identity.Dev) != record.Device || identity.Ino != record.Inode {
			return fmt.Errorf("source file identity changed")
		}
		hash, length, e := hashFile(ctx, record.Path)
		if e != nil {
			return e
		}
		if hash != record.SHA256 || length != record.Length {
			return fmt.Errorf("source content changed")
		}
		if e = os.Remove(record.Path); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if err = removeManagedReceipt(folder, req.Name); err != nil {
		return err
	}

	if err = os.Remove(folder); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err = syncDir(base); err != nil {
		return err
	}
	f, err := os.OpenFile(done, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return syncDir(base)
}

// Imported disks keep their publication receipt through growth and attachment.
// The digest may therefore be stale; ownership comes from its validated name,
// format and UUID, while transfer cleanup verifies current content separately.
func removeManagedReceipt(folder, name string) error {
	path := filepath.Join(folder, ".transfer.json")
	f, err := regularFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var receipt transferReceipt
	err = json.NewDecoder(io.LimitReader(f, 4096)).Decode(&receipt)
	f.Close()
	if err != nil {
		return err
	}
	if _, err = uuid.Parse(receipt.OperationID); err != nil || receipt.Name != name || (receipt.Format != "raw" && receipt.Format != "qcow2") {
		return fmt.Errorf("unrecognized disk receipt; manual cleanup required")
	}
	return os.Remove(path)
}

func publishTransfer(base, stage, final, imageName string) error {
	err := unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, final, unix.RENAME_NOREPLACE)
	if err == nil {
		return nil
	}
	if !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.EOPNOTSUPP) && !errors.Is(err, unix.ENOSYS) {
		return fmt.Errorf("publish without replacement: %w", err)
	}
	// NFS versions without RENAME_NOREPLACE still offer exclusive mkdir and link.
	// A crash leaves an owned receipt and staging image for explicit reconciliation.
	return publishTransferLinks(base, stage, final, imageName)
}
func publishTransferLinks(base, stage, final, imageName string) error {
	if err := os.Mkdir(final, 0777); err != nil {
		return err
	}
	if err := os.Link(filepath.Join(stage, ".transfer.json"), filepath.Join(final, ".transfer.json")); err != nil {
		return err
	}
	if err := syncDir(final); err != nil {
		return err
	}
	if err := syncDir(base); err != nil {
		return err
	}
	if err := os.Link(filepath.Join(stage, imageName), filepath.Join(final, imageName)); err != nil {
		return err
	}
	if err := syncDir(final); err != nil {
		return err
	}
	return syncDir(base)
}
func cleanupPublishedStage(stage, final string, req *pb.DiskTransferRequest) error {
	r, err := readReceipt(stage, req)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, name := range []string{req.Name + "." + r.Format, ".transfer.json"} {
		source, e := os.Lstat(filepath.Join(stage, name))
		if e != nil {
			if os.IsNotExist(e) {
				continue
			}
			return e
		}
		destination, e := os.Lstat(filepath.Join(final, name))
		if e != nil {
			return e
		}
		if !os.SameFile(source, destination) {
			return fmt.Errorf("staging publication identity differs")
		}
		if e = os.Remove(filepath.Join(stage, name)); e != nil {
			return e
		}
	}
	if err = os.Remove(stage); err != nil {
		return err
	}
	return syncDir(filepath.Dir(stage))
}
