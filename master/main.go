package main

import (
	"512SvMan/api"
	"512SvMan/db"
	"512SvMan/env512"
	"512SvMan/info"
	"512SvMan/logs512"
	"512SvMan/protocol"
	"512SvMan/services"
	"512SvMan/wireguard"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	logger "github.com/Maruqes/512SvMan/logger"
	"google.golang.org/grpc"
)

// Provide access to newSlaveCount for the API
func getNewSlaveCount() int {
	return int(atomic.LoadInt32(&newSlaveCount))
}

var newSlaveCount int32

var (
	newSlaveMu       sync.Mutex
	newSlaveInFlight = map[string]struct{}{}
)

func newSlave(addr, machineName string, conn *grpc.ClientConn) error {

	if machineName == "" {
		return fmt.Errorf("machineName cannot be empty")
	}

	newSlaveMu.Lock()
	if _, ok := newSlaveInFlight[machineName]; ok {
		newSlaveMu.Unlock()
		logger.Warnf("newSlave already running for %s, skipping duplicate", machineName)
		return nil
	}
	newSlaveInFlight[machineName] = struct{}{}
	newSlaveMu.Unlock()

	atomic.AddInt32(&newSlaveCount, 1)
	logger.Infof("newSlave started for %s (%s). In-flight=%d", machineName, addr, atomic.LoadInt32(&newSlaveCount))
	defer func() {
		atomic.AddInt32(&newSlaveCount, -1)
		newSlaveMu.Lock()
		delete(newSlaveInFlight, machineName)
		newSlaveMu.Unlock()
		logger.Infof("newSlave finished for %s (%s). In-flight=%d", machineName, addr, atomic.LoadInt32(&newSlaveCount))
	}()

	btrfsService := services.BTRFSService{}
	err := btrfsService.AutoMountRaid(machineName)
	if err != nil {
		logger.Errorf("AutoMountRaid failed for %s: %v", machineName, err)
		// Continue anyway - RAID mount failure shouldn't block other initialization
	}

	logger.Info("Mounting all NFS for", machineName)
	nfsService := services.NFSService{}
	err = nfsService.UpdateNFSShit(context.Background())
	if err != nil {
		logger.Errorf("UpdateNFS failed for %s: %v", machineName, err)
		// Continue anyway - NFS mount failure shouldn't block VM startup
	}

	time.Sleep(time.Second * 60)

	logger.Info("Auto starting vms for", machineName)
	virshServices := services.VirshService{}
	err = virshServices.StartAutoStartVms(context.Background())
	if err != nil {
		logger.Errorf("StartAutoStartVms failed for %s: %v", machineName, err)
	}

	k8sService := services.K8sService{}
	err = k8sService.ConnectSlaveToCluster()
	if err != nil && err != services.ErrSlaveMasterNotConnected {
		logger.Errorf("k8s startup failed for %s: %v", machineName, err)
	}

	dockerService := services.DockerService{}
	err = dockerService.StartAlwaysContainers()
	if err != nil {
		logger.Errorf("dockerService startup failed for %s: %v", machineName, err)
	}

	return nil
}

func askForSudo() {
	//if current program is not sudo terminate
	if os.Geteuid() != 0 {
		fmt.Println("This program needs to be run as root.")
		os.Exit(0)
	}
}

func execCommand(name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	var stderr bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func ensureIPForwarding() error {
	const sysctlConf = "/etc/sysctl.d/99-wireguard.conf"
	if err := os.WriteFile(sysctlConf, []byte("net.ipv4.ip_forward=1\n"), 0644); err != nil {
		return fmt.Errorf("write %s: %w", sysctlConf, err)
	}
	if err := execCommand("sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		return fmt.Errorf("enable ipv4 forwarding: %w", err)
	}
	return nil
}

func downloadNoVNC() error {
	//check if folder novnc exists
	if _, err := os.Stat("./novnc"); os.IsNotExist(err) {
		//download novnc from github
		fmt.Println("Downloading noVNC...")
		//use "git clone https://github.com/novnc/noVNC.git novnc"
		err := execCommand("git", "clone", "https://github.com/novnc/noVNC.git", "novnc")
		if err != nil {
			return err
		}
	} else {
		fmt.Println("noVNC folder already exists, trying to update...")
		//update novnc to latest version
		err := execCommand("git", "-C", "novnc", "pull")
		if err != nil {
			return err
		}
	}

	return nil
}

func GoAccess() error {
	const requiredVersion = "1.9.3"
	const downloadURL = "https://tar.goaccess.io/goaccess-1.9.3.tar.gz"

	install := func() error {
		tmpDir, err := os.MkdirTemp("", "goaccess-build-")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		archive := fmt.Sprintf("%s/goaccess-%s.tar.gz", tmpDir, requiredVersion)
		if err := execCommand("curl", "-L", "-o", archive, downloadURL); err != nil {
			return fmt.Errorf("download GoAccess %s: %w", requiredVersion, err)
		}

		if err := execCommand("tar", "-xzf", archive, "-C", tmpDir); err != nil {
			return fmt.Errorf("extract GoAccess %s: %w", requiredVersion, err)
		}

		sourceDir := fmt.Sprintf("%s/goaccess-%s", tmpDir, requiredVersion)
		runInDir := func(dir, name string, args ...string) error {
			cmd := exec.Command(name, args...)
			cmd.Dir = dir
			var stderr bytes.Buffer
			cmd.Stdout = os.Stdout
			cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
			if err := cmd.Run(); err != nil {
				msg := strings.TrimSpace(stderr.String())
				if msg != "" {
					return fmt.Errorf("%w: %s", err, msg)
				}
				return err
			}
			return nil
		}

		if err := runInDir(sourceDir, "./configure", "--enable-utf8", "--enable-geoip=mmdb"); err != nil {
			return fmt.Errorf("configure GoAccess %s: %w", requiredVersion, err)
		}
		if err := runInDir(sourceDir, "make"); err != nil {
			return fmt.Errorf("build GoAccess %s: %w", requiredVersion, err)
		}
		if err := runInDir(sourceDir, "make", "install"); err != nil {
			return fmt.Errorf("install GoAccess %s: %w", requiredVersion, err)
		}
		return nil
	}

	if _, err := exec.LookPath("goaccess"); err != nil {
		return install()
	}

	var out bytes.Buffer
	cmd := exec.Command("goaccess", "-V")
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("check GoAccess version: %w", err)
	}

	if !strings.Contains(out.String(), requiredVersion) {
		fmt.Printf("Current GoAccess version mismatch, forcing %s...\n", requiredVersion)
		return install()
	}

	fmt.Printf("GoAccess %s already installed.\n", requiredVersion)
	return nil
}

func installWireGuard() error {
	if _, err := exec.LookPath("wg"); err != nil {
		fmt.Println("Installing WireGuard...")
		if err := execCommand("dnf", "install", "-y", "wireguard-tools"); err != nil {
			return fmt.Errorf("failed to install WireGuard: %w", err)
		}
	} else {
		fmt.Println("WireGuard already installed, ensuring configuration...")
	}

	if err := ensureIPForwarding(); err != nil {
		return err
	}

	fmt.Println("WireGuard ready with  and routing configured.")
	return nil
}

func installIpset() error {
	var missing []string
	if _, err := exec.LookPath("ipset"); err != nil {
		missing = append(missing, "ipset")
	}
	if _, err := exec.LookPath("iptables"); err != nil {
		missing = append(missing, "iptables")
	}
	if len(missing) == 0 {
		return nil
	}

	fmt.Printf("Installing %s...\n", strings.Join(missing, ", "))
	args := append([]string{"install", "-y"}, missing...)
	if err := execCommand("dnf", args...); err != nil {
		return fmt.Errorf("failed to install %s: %w", strings.Join(missing, ", "), err)
	}
	return nil
}

func main() {

	// Set the function in api package to access newSlaveCount
	api.GetNewSlaveCountValue = getNewSlaveCount
	askForSudo()
	ctx := context.Background()
	exitAfterStart := false
	for _, arg := range os.Args[1:] {
		if arg == "--exit-after-start" {
			exitAfterStart = true
			break
		}
	}

	if err := env512.Setup(); err != nil {
		log.Fatalf("env setup: %v", err)
	}
	logger.SetType(env512.Mode)

	if err := installIpset(); err != nil {
		log.Fatalf("install ipset: %v", err)
	}

	db.InitDB(ctx)
	if err := db.CreateSPAPortsTable(ctx); err != nil {
		log.Fatalf("create spa table: %v", err)
	}
	SpaService := services.SPAService{}
	if err := SpaService.Reapply(ctx); err != nil {
		log.Fatalf("%s", err.Error())
	}

	//check if novnc folder exists, if not download it
	err := downloadNoVNC()
	if err != nil {
		log.Fatalf("download noVNC: %v", err)
	}

	err = GoAccess()
	if err != nil {
		log.Fatalf("install GoAccess: %v", err)
	}

	err = installWireGuard()
	if err != nil {
		log.Fatalf("install wireguard: %v", err)
	}

	err = db.CreateWireguardPeerTable(ctx)
	if err != nil {
		log.Fatalf("create wireguard peer table: %v", err)
	}

	if err := wireguard.AutoStartVPN(ctx); err != nil {
		log.Fatalf("start wireguard vpn: %v", err)
	}

	//create all tables if not exists
	err = db.CreateNFSTable(ctx)
	if err != nil {
		log.Fatalf("create NFS table: %v", err)
	}

	err = db.CreateVmLiveTable(ctx)
	if err != nil {
		log.Fatalf("create vm_live table: %v", err)
	}

	err = db.CreateLogsTable(ctx)
	if err != nil {
		log.Fatalf("create logs table: %v", err)
	}

	err = db.CreateISOTable(ctx)
	if err != nil {
		log.Fatalf("create ISO table: %v", err)
	}

	err = db.CreateCPUSnapshotsTable(ctx)
	if err != nil {
		log.Fatalf("create cpu snapshots table: %v", err)
	}

	err = db.CreateMemSnapshotsTable(ctx)
	if err != nil {
		log.Fatalf("create mem snapshots table: %v", err)
	}

	err = db.CreateDiskSnapshotsTable(ctx)
	if err != nil {
		log.Fatalf("create disk snapshots table: %v", err)
	}

	err = db.CreateNetworkSnapshotsTable(ctx)
	if err != nil {
		log.Fatalf("create network snapshots table: %v", err)
	}

	err = db.CreateStreamDailyMetricsTable(ctx)
	if err != nil {
		log.Fatalf("create stream metrics table: %v", err)
	}

	err = db.InitSmartDiskDB(ctx)
	if err != nil {
		log.Fatalf("create stream metrics table: %v", err)
	}

	infoCollector := &services.InfoService{}
	go infoCollector.GetSlaveData(ctx)

	err = db.CreateTableBackups(ctx)
	if err != nil {
		log.Fatalf("create backups table: %v", err)
	}

	err = db.CreateTableAutoStart(ctx)
	if err != nil {
		log.Fatalf("create autostart table: %v", err)
	}

	err = db.CreateTableAutomaticBackup(ctx)
	if err != nil {
		log.Fatalf("create autostart table: %v", err)
	}

	err = db.CreateBtrfsTable(ctx)
	if err != nil {
		log.Fatalf("create btrfs auto table: %v", err)
	}

	err = db.CreateDockerRepoTable(ctx)
	if err != nil {
		log.Fatalf("create docker repo table: %v", err)
	}

	err = db.DbCreatePushSubscriptionsTable(ctx)
	if err != nil {
		log.Fatalf("create push subs table: %v", err)
	}

	err = db.DbCreateNotsTable(ctx)
	if err != nil {
		log.Fatalf("create nots table: %v", err)
	}

	db.StartSQLiteBackupLoop(ctx)

	if err := setupFrontendContainer(); err != nil {
		fmt.Fprintf(os.Stderr, "error on frontend container: %v\n", err)
		os.Exit(1)
	}
	logger.Info("Frontend container ready at http://localhost:" + hostPort)

	//listen and connects to gRPC
	logger.SetCallBack(logs512.LoggerCallBack)
	go protocol.PingAllSlavesLoop()
	protocol.ListenGRPC(newSlave)

	virshService := services.VirshService{}
	smartDiskService := services.SmartDiskService{}
	nfsService := services.NFSService{}
	go nfsService.MaintainNFS()

	virshService.LoopAutomaticBaks(context.Background())
	smartDiskService.DoAutomaticTest()
	info.LoopNots()
	go SpaService.Maintain(ctx, 30*time.Second)

	// go func() {
	// 	addr := "127.0.0.1:6060"
	// 	logger.Info("pprof listening on http://" + addr + "/debug/pprof/")
	// 	if err := http.ListenAndServe(addr, nil); err != nil {
	// 		logger.Errorf("pprof server error: %v", err)
	// 	}
	// }()

	api.StartApi(exitAfterStart)

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	logger.Info("Application started. Press Ctrl+C to shutdown gracefully.")

	// Wait for interrupt signal
	<-sigChan

	logger.Info("Shutting down gracefully...")

	// Stop GoAccess background process
	api.StopGoAccess()

	logger.Info("Cleanup complete. Goodbye!")
	os.Exit(0)
}
