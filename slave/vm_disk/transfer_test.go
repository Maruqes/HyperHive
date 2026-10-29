package vmdisk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	pb "github.com/Maruqes/512SvMan/api/proto/vm_disk"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func transferTestClient(t *testing.T) pb.VMDiskServiceClient {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	pb.RegisterVMDiskServiceServer(server, &Service{})
	go server.Serve(listener)
	conn, err := grpc.NewClient("passthrough:///disk-test", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(); server.Stop(); listener.Close() })
	return pb.NewVMDiskServiceClient(conn)
}
func uploadTest(t *testing.T, client pb.VMDiskServiceClient, req *pb.DiskTransferRequest, data []byte) (*pb.DiskTransferResponse, error) {
	t.Helper()
	stream, err := client.UploadVMDisk(context.Background())
	if err != nil {
		return nil, err
	}
	if err = stream.Send(&pb.DiskUploadChunk{Transfer: req}); err != nil {
		return nil, err
	}
	for len(data) > 0 {
		n := min(len(data), 256*1024)
		if err = stream.Send(&pb.DiskUploadChunk{Data: data[:n]}); err != nil {
			break
		}
		data = data[n:]
	}
	return stream.CloseAndRecv()
}
func TestTransferRoundTripAndOwnership(t *testing.T) {
	client := transferTestClient(t)
	base := t.TempDir()
	data := bytes.Repeat([]byte("disk-content"), 100000)
	req := &pb.DiskTransferRequest{OperationID: uuid.NewString(), BasePath: base, Name: "imported", Length: int64(len(data))}
	result, err := uploadTest(t, client, req, data)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	if result.Sha256 != hex.EncodeToString(hash[:]) {
		t.Fatal("bad digest")
	}
	req.Action = "inspect"
	inspected, err := client.TransferVMDisk(context.Background(), req)
	if err != nil || inspected.Sha256 != result.Sha256 {
		t.Fatalf("inspect: %v", err)
	}
	stream, err := client.DownloadVMDisk(context.Background(), &pb.VMDiskByNameRequest{BasePath: base, Name: req.Name})
	if err != nil {
		t.Fatal(err)
	}
	header, err := stream.Header()
	if err != nil || len(header.Get("disk-length")) != 1 {
		t.Fatal("missing framing", err)
	}
	var got []byte
	for {
		chunk, e := stream.Recv()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		got = append(got, chunk.Data...)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("download content differs")
	}
	other := &pb.DiskTransferRequest{OperationID: uuid.NewString(), BasePath: base, Name: req.Name, Length: req.Length}
	if _, err = uploadTest(t, client, other, data); err == nil {
		t.Fatal("overwrote destination")
	}
	other.Action = "inspect"
	if _, err = client.TransferVMDisk(context.Background(), other); err == nil {
		t.Fatal("accepted wrong owner")
	}
	req.Action = "discard-stage"
	if _, err = client.TransferVMDisk(context.Background(), req); err == nil {
		t.Fatal("aborted published image")
	}
}
func TestShortUploadAndAbort(t *testing.T) {
	client := transferTestClient(t)
	base := t.TempDir()
	req := &pb.DiskTransferRequest{OperationID: uuid.NewString(), BasePath: base, Name: "short", Length: 100}
	if _, err := uploadTest(t, client, req, []byte("short")); err == nil {
		t.Fatal("accepted short body")
	}
	if _, err := os.Stat(filepath.Join(base, "vm_disk_short")); !os.IsNotExist(err) {
		t.Fatal("published partial disk")
	}
	req.Action = "discard-stage"
	if _, err := client.TransferVMDisk(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := client.TransferVMDisk(context.Background(), req); err != nil {
		t.Fatal("abort not idempotent", err)
	}
}
func TestImagePolicy(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "image")
	ctx := context.Background()
	if out, err := exec.Command("qemu-img", "create", "-f", "qcow2", path, "1M").CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	if info, err := validateTransferImage(ctx, path); err != nil || info.Format != "qcow2" {
		t.Fatalf("qcow2 validation: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, offset := range []int{8, 16, 32, 72} {
		t.Run(string(rune('A'+offset)), func(t *testing.T) {
			hostile := append([]byte(nil), data...)
			if offset == 72 {
				binary.BigEndian.PutUint64(hostile[72:], 4)
			} else {
				hostile[offset+3] = 1
			}
			os.WriteFile(path, hostile, 0600)
			if _, err := validateTransferImage(ctx, path); err == nil {
				t.Fatal("accepted hostile qcow2 header")
			}
		})
	}
	os.WriteFile(path, []byte("KDMVunsupported"), 0600)
	if _, err := validateTransferImage(ctx, path); err == nil {
		t.Fatal("accepted VMDK")
	}
}
func TestTransferRejectsTraversalAndActiveAbort(t *testing.T) {
	client := transferTestClient(t)
	req := &pb.DiskTransferRequest{OperationID: uuid.NewString(), BasePath: t.TempDir(), Name: "../escape", Length: 100}
	if _, err := uploadTest(t, client, req, []byte("x")); err == nil {
		t.Fatal("accepted traversal")
	}
	req.Name = "active"
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.UploadVMDisk(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = stream.Send(&pb.DiskUploadChunk{Transfer: req}); err != nil {
		t.Fatal(err)
	}
	// Wait for durable staging to establish that the server holds worker ownership.
	deadline := time.Now().Add(time.Second)
	for {
		if _, err = os.Stat(filepath.Join(req.BasePath, ".vm-transfer-"+req.OperationID, ".transfer.json")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not stage")
		}
		time.Sleep(time.Millisecond)
	}
	req.Action = "discard-stage"
	if _, err = client.TransferVMDisk(context.Background(), req); err == nil {
		t.Fatal("aborted active worker")
	}
	cancel()
}

func TestSourceRemovalIsIdempotentAfterNameReuse(t *testing.T) {
	client := transferTestClient(t)
	base := t.TempDir()
	data := []byte("original disk bytes")
	req := &pb.DiskTransferRequest{OperationID: uuid.NewString(), BasePath: base, Name: "source", Length: int64(len(data))}
	result, err := uploadTest(t, client, req, data)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := &pb.DiskTransferRequest{OperationID: uuid.NewString(), BasePath: base, Name: "source", Length: result.Length, Sha256: result.Sha256, Action: "remove-source"}
	if _, err = client.TransferVMDisk(context.Background(), cleanup); err != nil {
		t.Fatal(err)
	}
	req.OperationID = uuid.NewString()
	if _, err = uploadTest(t, client, req, data); err != nil {
		t.Fatal(err)
	}
	if _, err = client.TransferVMDisk(context.Background(), cleanup); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(base, "vm_disk_source", "source.raw")); err != nil {
		t.Fatal("retry removed new source", err)
	}
}
func TestRemovalPreservesChangedSourceAndUnknownFiles(t *testing.T) {
	client := transferTestClient(t)
	base := t.TempDir()
	data := []byte("original bytes")
	req := &pb.DiskTransferRequest{OperationID: uuid.NewString(), BasePath: base, Name: "changed", Length: int64(len(data))}
	result, err := uploadTest(t, client, req, data)
	if err != nil {
		t.Fatal(err)
	}
	path := result.Disk.DiskPath
	if err = os.WriteFile(path, []byte("changed bytes!"), 0600); err != nil {
		t.Fatal(err)
	}
	req.OperationID = uuid.NewString()
	req.Action = "remove-source"
	req.Sha256 = result.Sha256
	if _, err = client.TransferVMDisk(context.Background(), req); err == nil {
		t.Fatal("removed modified source")
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal("lost source")
	}
}
func TestAgentRestartCanInspectPublication(t *testing.T) {
	base := t.TempDir()
	req := &pb.DiskTransferRequest{OperationID: uuid.NewString(), BasePath: base, Name: "restart", Length: 1000}
	data := make([]byte, 1000)
	first := transferTestClient(t)
	result, err := uploadTest(t, first, req, data)
	if err != nil {
		t.Fatal(err)
	}
	second := transferTestClient(t)
	req.Action = "inspect"
	recovered, err := second.TransferVMDisk(context.Background(), req)
	if err != nil || recovered.Sha256 != result.Sha256 {
		t.Fatal("could not recover receipt", err)
	}
}
func TestCapacityAndInsufficientSpace(t *testing.T) {
	client := transferTestClient(t)
	req := &pb.DiskTransferRequest{OperationID: uuid.NewString(), BasePath: t.TempDir(), Name: "capacity", Action: "capacity"}
	capacity, err := client.TransferVMDisk(context.Background(), req)
	if err != nil || capacity.FreeBytes == 0 {
		t.Fatal("capacity unavailable", err)
	}
	req.Length = int64(capacity.FreeBytes) + 1
	if _, err = uploadTest(t, client, req, []byte("x")); err == nil {
		t.Fatal("accepted upload larger than free space")
	}
	if _, err = os.Stat(filepath.Join(req.BasePath, "vm_disk_capacity")); !os.IsNotExist(err) {
		t.Fatal("published insufficient-space upload")
	}
}
func TestSparseTransferAndCrossAgentHash(t *testing.T) {
	source := transferTestClient(t)
	destination := transferTestClient(t)
	base := t.TempDir()
	data := make([]byte, 8*1024*1024)
	copy(data, "header")
	copy(data[len(data)-4:], "tail")
	req := &pb.DiskTransferRequest{OperationID: uuid.NewString(), BasePath: base, Name: "sparse", Length: int64(len(data))}
	original, err := uploadTest(t, source, req, data)
	if err != nil {
		t.Fatal(err)
	}
	download, err := source.DownloadVMDisk(context.Background(), &pb.VMDiskByNameRequest{BasePath: base, Name: "sparse"})
	if err != nil {
		t.Fatal(err)
	}
	upload, err := destination.UploadVMDisk(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	next := &pb.DiskTransferRequest{OperationID: uuid.NewString(), BasePath: dest, Name: "sparse", Length: req.Length, Sha256: original.Sha256}
	if err = upload.Send(&pb.DiskUploadChunk{Transfer: next}); err != nil {
		t.Fatal(err)
	}
	for {
		chunk, e := download.Recv()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		if e = upload.Send(&pb.DiskUploadChunk{Data: chunk.Data}); e != nil {
			t.Fatal(e)
		}
	}
	copied, err := upload.CloseAndRecv()
	if err != nil || copied.Sha256 != original.Sha256 {
		t.Fatal("copy differs", err)
	}
	if copied.Disk.OccupiedBytes >= req.Length/2 {
		t.Fatalf("sparse image inflated: %d", copied.Disk.OccupiedBytes)
	}
}

func TestImportedDiskRemainsGrowableAndDeletable(t *testing.T) {
	client := transferTestClient(t)
	base := t.TempDir()
	req := &pb.DiskTransferRequest{OperationID: uuid.NewString(), BasePath: base, Name: "managed", Length: 1024}
	if _, err := uploadTest(t, client, req, make([]byte, 1024)); err != nil {
		t.Fatal(err)
	}
	if _, err := GrowVMDisk(base, req.Name, 2); err != nil {
		t.Fatal("imported disk cannot grow", err)
	}
	if _, err := DeleteVMDisk(base, req.Name); err != nil {
		t.Fatal("imported disk cannot delete", err)
	}
	if _, err := os.Stat(filepath.Join(base, "vm_disk_managed")); !os.IsNotExist(err) {
		t.Fatal("owned disk folder retained")
	}
}

func TestNFSPublicationFallbackAndRecovery(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "published", true: "interrupted"}[partial], func(t *testing.T) {
			base := t.TempDir()
			req := &pb.DiskTransferRequest{OperationID: uuid.NewString(), BasePath: base, Name: "linked", Length: 4096, Action: "inspect"}
			_, stage, final, err := transferPaths(req)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Mkdir(stage, 0700); err != nil {
				t.Fatal(err)
			}
			data := make([]byte, 4096)
			hash := sha256.Sum256(data)
			receipt := transferReceipt{OperationID: req.OperationID, Name: req.Name, Length: req.Length, SHA256: hex.EncodeToString(hash[:]), Format: "raw"}
			if err = writeReceipt(stage, receipt); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(stage, "linked.raw"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if partial {
				if err = os.Mkdir(final, 0777); err != nil {
					t.Fatal(err)
				}
				if err = os.Link(filepath.Join(stage, ".transfer.json"), filepath.Join(final, ".transfer.json")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err = publishTransferLinks(base, stage, final, "linked.raw"); err != nil {
					t.Fatal(err)
				}
			}
			service := &Service{}
			response, err := service.TransferVMDisk(context.Background(), req)
			if err != nil || response.Sha256 != receipt.SHA256 {
				t.Fatal("fallback recovery failed", err)
			}
			if _, err = os.Stat(stage); !os.IsNotExist(err) {
				t.Fatal("published staging retained")
			}
			if err = publishTransferLinks(base, stage, final, "linked.raw"); err == nil {
				t.Fatal("replaced existing publication")
			}
		})
	}
}
