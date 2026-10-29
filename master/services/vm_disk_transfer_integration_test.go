package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"512SvMan/db"
	"512SvMan/protocol"
	np "github.com/Maruqes/512SvMan/api/proto/nfs"
	pp "github.com/Maruqes/512SvMan/api/proto/protocol"
	vp "github.com/Maruqes/512SvMan/api/proto/virsh"
	dp "github.com/Maruqes/512SvMan/api/proto/vm_disk"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type diskAgentFixture struct {
	dp.UnimplementedVMDiskServiceServer
	np.UnimplementedNFSServiceServer
	vp.UnimplementedSlaveVirshServiceServer
	pp.UnimplementedClientServiceServer
	mu                                                            sync.Mutex
	base, host, definition                                        string
	data                                                          []byte
	publication                                                   *dp.DiskTransferResponse
	operation                                                     string
	running, failPublish, failDefinition, failCleanup, failCommit bool
	removed                                                       int
}

func (f *diskAgentFixture) Notify(context.Context, *pp.NotifyRequest) (*pp.NotifyResponse, error) {
	return &pp.NotifyResponse{Ok: "fixture"}, nil
}
func (f *diskAgentFixture) GetSharedFolderStatus(context.Context, *np.FolderMount) (*np.SharedFolderStatusResponse, error) {
	return &np.SharedFolderStatusResponse{Working: true}, nil
}
func (f *diskAgentFixture) CheckReadWrite(context.Context, *np.FolderPath) (*np.OkResponse, error) {
	return &np.OkResponse{Ok: true}, nil
}
func (f *diskAgentFixture) CheckFileReadable(context.Context, *np.FolderPath) (*np.OkResponse, error) {
	return &np.OkResponse{Ok: true}, nil
}
func (f *diskAgentFixture) GetVmByName(context.Context, *vp.GetVmByNameRequest) (*vp.Vm, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	state := vp.VmState_SHUTOFF
	if f.running {
		state = vp.VmState_RUNNING
	}
	return &vp.Vm{Name: "guest", MachineName: f.host, State: state}, nil
}
func (f *diskAgentFixture) GetVMSnapshot(context.Context, *vp.GetVmByNameRequest) (*vp.VMSnapshotResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &vp.VMSnapshotResponse{Name: "guest", Uuid: "fixture", Active: f.running, InactiveXml: f.definition}, nil
}
func (f *diskAgentFixture) UpdateVMXml(_ context.Context, req *vp.UpdateVMXmlRequest) (*vp.OkResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.definition = req.VmXML
	if f.failDefinition {
		return nil, errors.New("definition acknowledgement lost")
	}
	return &vp.OkResponse{Ok: true}, nil
}
func (f *diskAgentFixture) DownloadVMDisk(_ *dp.VMDiskByNameRequest, stream dp.VMDiskService_DownloadVMDiskServer) error {
	f.mu.Lock()
	data := append([]byte(nil), f.data...)
	f.mu.Unlock()
	if err := stream.SendHeader(metadata.Pairs("disk-length", strconv.Itoa(len(data)))); err != nil {
		return err
	}
	for len(data) > 0 {
		n := min(len(data), 64*1024)
		if err := stream.Send(&dp.VMDiskChunk{Data: data[:n]}); err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}
func (f *diskAgentFixture) UploadVMDisk(stream dp.VMDiskService_UploadVMDiskServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	req := first.Transfer
	var data []byte
	for {
		chunk, e := stream.Recv()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		data = append(data, chunk.Data...)
	}
	if int64(len(data)) != req.Length {
		return errors.New("short upload")
	}
	hash := sha256.Sum256(data)
	path := filepath.Join(f.base, "vm_disk_"+req.Name, req.Name+".raw")
	response := &dp.DiskTransferResponse{Disk: &dp.VMDiskResponse{Ok: true, DiskPath: path, FolderPath: filepath.Dir(path), Format: "raw", SizeGB: 1}, Length: req.Length, Sha256: hex.EncodeToString(hash[:]), Phase: "published"}
	f.mu.Lock()
	f.publication = response
	f.operation = req.OperationID
	f.data = data
	fail := f.failPublish
	commit := f.failCommit
	f.mu.Unlock()
	if commit {
		if _, err = db.DB.Exec(`CREATE TRIGGER fail_disk_move BEFORE UPDATE OF nfs_id ON vm_disks BEGIN SELECT RAISE(ABORT,'injected commit failure'); END`); err != nil {
			return err
		}
	}
	if fail {
		return errors.New("publication acknowledgement lost")
	}
	return stream.SendAndClose(response)
}
func (f *diskAgentFixture) TransferVMDisk(_ context.Context, req *dp.DiskTransferRequest) (*dp.DiskTransferResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch req.Action {
	case "inspect":
		if f.publication == nil || req.OperationID != f.operation {
			return nil, errors.New("publication missing")
		}
		return f.publication, nil
	case "remove-source":
		if f.failCleanup {
			return nil, errors.New("source unavailable")
		}
		hash := sha256.Sum256(f.data)
		if hex.EncodeToString(hash[:]) != req.Sha256 {
			return nil, errors.New("source changed")
		}
		f.removed++
		return &dp.DiskTransferResponse{Phase: "removed"}, nil
	case "discard-stage":
		if f.publication != nil {
			return nil, errors.New("already published")
		}
		return &dp.DiskTransferResponse{Phase: "discarded"}, nil
	default:
		return nil, errors.New("unexpected action")
	}
}
func startDiskFixture(t *testing.T, address, host, base string) *diskAgentFixture {
	t.Helper()
	listener, err := net.Listen("tcp", address+":50052")
	if err != nil {
		t.Fatal("disposable local fixture could not bind; no connection attempted", err)
	}
	fixture := &diskAgentFixture{base: base, host: host}
	server := grpc.NewServer()
	dp.RegisterVMDiskServiceServer(server, fixture)
	np.RegisterNFSServiceServer(server, fixture)
	vp.RegisterSlaveVirshServiceServer(server, fixture)
	pp.RegisterClientServiceServer(server, fixture)
	go server.Serve(listener)
	t.Cleanup(func() { server.Stop(); listener.Close() })
	if err = protocol.NewSlaveConnection(address, host); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { protocol.GetConnectionByMachineName(host).Connection.Close() })
	return fixture
}
func TestManagedDiskMoveRecoveryChain(t *testing.T) {
	// The application registry is global, so keep these fake agents in a subprocess.
	if os.Getenv("HYPERHIVE_DISK_TRANSFER_CHILD") != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestManagedDiskMoveRecoveryChain$", "-test.count=1")
		command.Env = append(os.Environ(), "HYPERHIVE_DISK_TRANSFER_CHILD=1")
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("isolated disk transfer fixture: %v\n%s", err, out)
		}
		return
	}
	source := startDiskFixture(t, "127.0.0.7", "disk-source", "/source")
	destination := startDiskFixture(t, "127.0.0.8", "disk-destination", "/destination")
	original := db.DB
	defer func() { db.DB = original }()
	for _, scenario := range []string{"browser import", "import registration failure", "short browser upload", "detached", "attached", "publish acknowledgement", "definition acknowledgement", "database commit", "source cleanup", "running VM", "overlapping mutation"} {
		t.Run(scenario, func(t *testing.T) {
			connection, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "disk.db"))
			if err != nil {
				t.Fatal(err)
			}
			db.DB = connection
			defer connection.Close()
			ctx := context.Background()
			for _, create := range []func(context.Context) error{db.CreateVMDiskTable, db.CreateNFSTable} {
				if err = create(ctx); err != nil {
					t.Fatal(err)
				}
			}
			for _, share := range []struct{ host, base string }{{"disk-source", "/source"}, {"disk-destination", "/destination"}} {
				if err = db.AddNFSShare(ctx, share.host, share.base, "fixture:"+share.base, share.base, share.host, false); err != nil {
					t.Fatal(err)
				}
			}
			id, err := db.AddVMDisk(ctx, "data", 1, "/source/vm_disk_data/data.raw", "/source/vm_disk_data", "raw", 1)
			if err != nil {
				t.Fatal(err)
			}
			attached := scenario != "detached"
			if attached {
				if ok, e := db.ReserveVMDiskAttachment(ctx, id, "guest", "disk-source"); e != nil || !ok {
					t.Fatal(e)
				}
			}
			source.mu.Lock()
			source.data = bytes.Repeat([]byte("disk"), 10000)
			source.definition = `<domain><name>guest</name><devices><disk><source file="/source/vm_disk_data/data.raw"/><target dev="vdb"/></disk><disk><source file="/boot.qcow2"/><target dev="vda"/></disk></devices></domain>`
			source.running = scenario == "running VM"
			source.failDefinition = scenario == "definition acknowledgement"
			source.failCleanup = scenario == "source cleanup"
			source.removed = 0
			source.mu.Unlock()
			destination.mu.Lock()
			destination.publication = nil
			destination.failPublish = scenario == "publish acknowledgement"
			destination.failCommit = scenario == "database commit"
			destination.mu.Unlock()
			service := &VMDiskService{}
			if scenario == "browser import" || scenario == "import registration failure" || scenario == "short browser upload" {
				image := []byte("browser raw image")
				length := int64(len(image))
				if scenario == "short browser upload" {
					length++
				}
				if scenario == "import registration failure" {
					if _, err = db.DB.Exec(`CREATE TRIGGER fail_disk_import BEFORE INSERT ON vm_disks BEGIN SELECT RAISE(ABORT,'injected import commit failure'); END`); err != nil {
						t.Fatal(err)
					}
				}
				imported, importErr := service.Import(ctx, "browser", 2, length, bytes.NewReader(image))
				if scenario == "browser import" {
					if importErr != nil || imported.Id == id || imported.NFSID != 2 {
						t.Fatalf("import failed: %+v %v", imported, importErr)
					}
					return
				}
				if importErr == nil {
					t.Fatal("expected import failure")
				}
				operations, e := db.ListDiskOperations(ctx, 100)
				if e != nil || len(operations) != 1 {
					t.Fatalf("missing recoverable import: %+v %v", operations, e)
				}
				if exists, e := db.DoesVMDiskNameExist(ctx, "browser"); e != nil || exists {
					t.Fatal("registered failed upload", e)
				}
				action := "abort"
				if scenario == "import registration failure" {
					if _, err = db.DB.Exec("DROP TRIGGER fail_disk_import"); err != nil {
						t.Fatal(err)
					}
					action = "finish"
				}
				recovered, e := (&VMDiskService{}).Reconcile(ctx, operations[0].ID, action)
				if e != nil {
					t.Fatal("import recovery failed", e)
				}
				if action == "finish" && recovered.Phase != "complete" {
					t.Fatal("import not committed")
				}
				if action == "abort" && recovered.Phase != "aborted" {
					t.Fatal("short upload not aborted")
				}
				return
			}
			if scenario == "overlapping mutation" {
				op := &db.DiskOperation{Kind: "move", DiskID: id, Name: "data", VMName: "guest"}
				if err = db.BeginDiskOperation(ctx, op); err != nil {
					t.Fatal(err)
				}
				if _, err = service.Move(ctx, id, 2); err == nil {
					t.Fatal("overlapping move allowed")
				}
				return
			}
			op, err := service.Move(ctx, id, 2)
			if scenario == "running VM" {
				if err == nil {
					t.Fatal("running VM allowed")
				}
				return
			}
			needsRecovery := scenario != "detached" && scenario != "attached"
			if needsRecovery {
				if err == nil || op == nil {
					t.Fatalf("expected recoverable failure: %v", err)
				}
				before, _ := db.GetVMDiskByID(ctx, id)
				source.mu.Lock()
				removed := source.removed
				source.failDefinition = false
				source.failCleanup = false
				source.mu.Unlock()
				if removed != 0 {
					t.Fatal("source deleted before recovery")
				}
				if scenario != "source cleanup" && before.NFSID != 1 {
					t.Fatal("metadata committed before verification")
				}
				if scenario == "database commit" {
					if _, err = db.DB.Exec("DROP TRIGGER fail_disk_move"); err != nil {
						t.Fatal(err)
					}
				}
				destination.mu.Lock()
				destination.failPublish = false
				destination.failCommit = false
				destination.mu.Unlock()
				// A new service instance has no volatile operation state to rely on.
				op, err = (&VMDiskService{}).Reconcile(ctx, op.ID, "finish")
				if err != nil {
					t.Fatal("recovery failed", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			got, err := db.GetVMDiskByID(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if got.Id != id || got.NFSID != 2 || got.DiskPath != "/destination/vm_disk_data/data.raw" {
				t.Fatalf("wrong committed disk: %+v", got)
			}
			if attached && got.AttachedVMName != "guest" {
				t.Fatal("attachment lost")
			}
			if op.Phase != "complete" {
				t.Fatalf("phase=%s", op.Phase)
			}
			source.mu.Lock()
			removed := source.removed
			definition := source.definition
			source.mu.Unlock()
			if removed != 1 {
				t.Fatalf("source cleanup count=%d", removed)
			}
			if attached && (!strings.Contains(definition, "/destination/vm_disk_data/data.raw") || !strings.Contains(definition, "/boot.qcow2")) {
				t.Fatal("persistent devices corrupted")
			}
			if _, err = service.Reconcile(ctx, op.ID, "finish"); err != nil {
				t.Fatal("terminal retry failed", err)
			}
			source.mu.Lock()
			defer source.mu.Unlock()
			if source.removed != 1 {
				t.Fatal("terminal reconciliation repeated deletion")
			}
		})
	}
}
