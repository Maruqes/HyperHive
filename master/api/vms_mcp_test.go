package api

import (
	"512SvMan/internal/testnpm"
	"512SvMan/protocol"
	"context"
	"encoding/json"
	grpcProtocol "github.com/Maruqes/512SvMan/api/proto/protocol"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type snapshotRPCFixture struct {
	grpcVirsh.UnimplementedSlaveVirshServiceServer
	writes atomic.Int32
	backup string
	t      *testing.T
}

func (f *snapshotRPCFixture) GetVmByName(context.Context, *grpcVirsh.GetVmByNameRequest) (*grpcVirsh.Vm, error) {
	return &grpcVirsh.Vm{Name: "fixture-guest", MachineName: "vm-snapshot-fixture", State: grpcVirsh.VmState_SHUTOFF}, nil
}
func (f *snapshotRPCFixture) GetVMXml(context.Context, *grpcVirsh.GetVmByNameRequest) (*grpcVirsh.VMXMLResponse, error) {
	return &grpcVirsh.VMXMLResponse{VmXML: "<domain><name>fixture-guest</name><uuid>fixture-uuid</uuid></domain>"}, nil
}
func (f *snapshotRPCFixture) GetVMSnapshot(context.Context, *grpcVirsh.GetVmByNameRequest) (*grpcVirsh.VMSnapshotResponse, error) {
	return &grpcVirsh.VMSnapshotResponse{Name: "fixture-guest", Uuid: "fixture-uuid", InactiveXml: "<domain><name>fixture-guest</name><uuid>fixture-uuid</uuid><devices><graphics type='vnc' passwd='exact-old-secret'/></devices></domain>"}, nil
}
func (f *snapshotRPCFixture) GetHostBootSnapshot(context.Context, *grpcVirsh.Empty) (*grpcVirsh.HostBootSnapshotResponse, error) {
	return &grpcVirsh.HostBootSnapshotResponse{GrubbyInfo: "index=0\nkernel=\"/boot/vmlinuz-fixture\"\nargs=\"ro quiet\"\n", CurrentCmdline: "ro quiet\n", DefaultKernel: "/boot/vmlinuz-fixture", Entries: []*grpcVirsh.HostBootEntry{{Index: 0, Kernel: "/boot/vmlinuz-fixture", Args: "ro quiet"}}}, nil
}

func (f *snapshotRPCFixture) UpdateVMXml(_ context.Context, req *grpcVirsh.UpdateVMXmlRequest) (*grpcVirsh.OkResponse, error) {
	files, _ := filepath.Glob(filepath.Join(f.backup, "change-*", "before.json"))
	if len(files) != 1 {
		f.t.Errorf("actual RPC write before one persisted backup: %v", files)
	} else {
		data, err := os.ReadFile(files[0])
		if err != nil {
			f.t.Error(err)
		}
		var record struct {
			Snapshots map[string]json.RawMessage `json:"snapshots"`
		}
		json.Unmarshal(data, &record)
		var secure struct {
			Inactive string `json:"inactive_xml"`
		}
		if json.Unmarshal(record.Snapshots["secure_domain"], &secure) != nil || !strings.Contains(secure.Inactive, "passwd='exact-old-secret'") {
			f.t.Error("secure actual prior XML not retained")
		}
	}
	f.writes.Add(1)
	return &grpcVirsh.OkResponse{Ok: true}, nil
}

type snapshotPingFixture struct {
	grpcProtocol.UnimplementedClientServiceServer
}

func (snapshotPingFixture) Notify(context.Context, *grpcProtocol.NotifyRequest) (*grpcProtocol.NotifyResponse, error) {
	return &grpcProtocol.NotifyResponse{Ok: "fixture"}, nil
}

// This fixture exercises the real authenticated handler/service/RPC path and
// the real MCP backup-before-mutation sequence without any hypervisor or host.
func TestVMMCPActualSecureSnapshotChain(t *testing.T) {
	if os.Getenv("HYPERHIVE_VM_CHAIN_CHILD") != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestVMMCPActualSecureSnapshotChain$", "-test.count=1")
		command.Env = append(os.Environ(), "HYPERHIVE_VM_CHAIN_CHILD=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("isolated VM chain fixture failed: %v\n%s", err, output)
		}
		return
	}

	listener, err := net.Listen("tcp", "127.0.0.1:50052")
	if err != nil {
		t.Fatal("required disposable slave fixture could not bind 127.0.0.1:50052; no connection attempted")
	}
	defer listener.Close()
	backup := filepath.Join(t.TempDir(), "private")
	fixture := &snapshotRPCFixture{backup: backup, t: t}
	rpc := grpc.NewServer()
	grpcVirsh.RegisterSlaveVirshServiceServer(rpc, fixture)
	grpcProtocol.RegisterClientServiceServer(rpc, snapshotPingFixture{})
	go rpc.Serve(listener)
	defer rpc.Stop()
	if err := protocol.NewSlaveConnection("127.0.0.1", "vm-snapshot-fixture"); err != nil {
		t.Fatal(err)
	}
	defer protocol.GetConnectionByMachineName("vm-snapshot-fixture").Connection.Close()
	t.Setenv("MCP_TOKEN", "client-key")
	t.Setenv("MCP_VM_BACKUP_DIR", backup)
	t.Chdir(t.TempDir())
	testnpm.State(t, filepath.Join("npm-data"))
	router := chi.NewRouter()
	server := httptest.NewUnstartedServer(router)
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	setupMCPAPI(router, port)
	router.Group(func(r chi.Router) { r.Use(authMiddleware); setupVirshAPI(r) })
	server.Start()
	defer server.Close()
	req, _ := http.NewRequest("GET", server.URL+"/virsh/snapshot/fixture-guest?machine_name=vm-snapshot-fixture", nil)
	req.Header.Set("Authorization", "Bearer client-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-store" || !strings.Contains(string(data), "exact-old-secret") {
		t.Fatalf("snapshot API failed %d", resp.StatusCode)
	}

	hostRequest, _ := http.NewRequest("GET", server.URL+"/virsh/hostsnapshot/vm-snapshot-fixture", nil)
	hostRequest.Header.Set("Authorization", "Bearer client-key")
	hostResponse, err := http.DefaultClient.Do(hostRequest)
	if err != nil {
		t.Fatal(err)
	}
	hostBody, _ := io.ReadAll(hostResponse.Body)
	hostResponse.Body.Close()
	var hostSnapshot struct {
		Machine string `json:"machine_name"`
		Raw     string `json:"grubby_info"`
		Default string `json:"default_kernel"`
	}
	if hostResponse.StatusCode != 200 || hostResponse.Header.Get("Cache-Control") != "no-store" || json.Unmarshal(hostBody, &hostSnapshot) != nil || hostSnapshot.Machine != "vm-snapshot-fixture" || hostSnapshot.Raw != "index=0\nkernel=\"/boot/vmlinuz-fixture\"\nargs=\"ro quiet\"\n" || hostSnapshot.Default != "/boot/vmlinuz-fixture" {
		t.Fatal("host boot snapshot bridge changed or omitted actual data")
	}
	resp, err = http.Get(server.URL + "/virsh/snapshot/fixture-guest?machine_name=vm-snapshot-fixture")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal("secret snapshot exposed without auth")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "actual-vm-snapshot"}, &mcp.ClientOptions{ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve"}}, nil
	}})
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: mcpBearerTransport{}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	out, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "update_vm_xml", Arguments: map[string]any{"vm_name": "fixture-guest", "machine_name": "vm-snapshot-fixture", "vm_xml": "<domain><name>fixture-guest</name><uuid>fixture-uuid</uuid><memory>2048</memory></domain>"}})
	if err != nil || out.IsError || fixture.writes.Load() != 1 {
		t.Fatalf("actual handler chain failed: %v %#v", err, out)
	}
}
