package services

import (
	"512SvMan/db"
	"512SvMan/protocol"
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	grpcProtocol "github.com/Maruqes/512SvMan/api/proto/protocol"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"google.golang.org/grpc"
)

type dnsDeletionVMFixture struct {
	grpcVirsh.UnimplementedSlaveVirshServiceServer
	failRemoval atomic.Bool
	removed     atomic.Bool
}

func (f *dnsDeletionVMFixture) GetVmByName(context.Context, *grpcVirsh.GetVmByNameRequest) (*grpcVirsh.Vm, error) {
	return &grpcVirsh.Vm{Name: "dns-guest"}, nil
}

func (f *dnsDeletionVMFixture) RemoveVM(context.Context, *grpcVirsh.Vm) (*grpcVirsh.OkResponse, error) {
	if f.failRemoval.Load() {
		return nil, errors.New("VM removal failed")
	}
	f.removed.Store(true)
	return &grpcVirsh.OkResponse{Ok: true}, nil
}

type dnsDeletionPingFixture struct {
	grpcProtocol.UnimplementedClientServiceServer
}

func (dnsDeletionPingFixture) Notify(context.Context, *grpcProtocol.NotifyRequest) (*grpcProtocol.NotifyResponse, error) {
	return &grpcProtocol.NotifyResponse{Ok: "fixture"}, nil
}

func TestVMDeletionDNSCleanup(t *testing.T) {
	// Isolate the global slave registry from other service tests.
	if os.Getenv("HYPERHIVE_DNS_DELETE_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestVMDeletionDNSCleanup$", "-test.count=1")
		cmd.Env = append(os.Environ(), "HYPERHIVE_DNS_DELETE_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated deletion fixture: %v\n%s", err, out)
		}
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.2:50052")
	if err != nil {
		t.Fatal(err)
	}
	fixture := &dnsDeletionVMFixture{}
	rpc := grpc.NewServer()
	grpcVirsh.RegisterSlaveVirshServiceServer(rpc, fixture)
	grpcProtocol.RegisterClientServiceServer(rpc, dnsDeletionPingFixture{})
	go rpc.Serve(listener)
	defer rpc.Stop()
	if err := protocol.NewSlaveConnection("127.0.0.2", "dns-deletion-fixture"); err != nil {
		t.Fatal(err)
	}
	defer protocol.GetConnectionByMachineName("dns-deletion-fixture").Connection.Close()
	db.DB, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.DB.SetMaxOpenConns(1)
	defer db.DB.Close()
	ctx := context.Background()
	for _, create := range []func(context.Context) error{db.CreateVmLiveTable, db.CreateTableAutomaticBackup, db.CreateTableAutoStart, db.CreateVMDiskTable} {
		if err := create(ctx); err != nil {
			t.Fatal(err)
		}
	}
	service := &VirshService{}
	for _, tc := range []struct {
		name            string
		failVM, failDNS bool
	}{
		{name: "failed VM deletion keeps DNS", failVM: true},
		{name: "successful VM deletion cleans DNS"},
		{name: "DNS cleanup failure is reported", failDNS: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture.failRemoval.Store(tc.failVM)
			fixture.removed.Store(false)
			calls := 0
			cleanupErr := errors.New("DNS write failed")
			removeDNSAliasesForVM = func(name string) error {
				calls++
				if name != "dns-guest" || !fixture.removed.Load() {
					t.Fatal("DNS cleanup ran before successful removal or for a different VM")
				}
				if tc.failDNS {
					return cleanupErr
				}
				return nil
			}
			err := service.DeleteVM(ctx, "dns-guest")
			if tc.failVM {
				if err == nil || calls != 0 {
					t.Fatalf("failed removal changed DNS: calls=%d, error=%v", calls, err)
				}
			} else if tc.failDNS {
				if !errors.Is(err, cleanupErr) || !strings.Contains(err.Error(), "was deleted") || calls != 1 {
					t.Fatalf("partial failure lost: %v", err)
				}
			} else if err != nil || calls != 1 {
				t.Fatalf("successful removal skipped cleanup: calls=%d, error=%v", calls, err)
			}
		})
	}
}
