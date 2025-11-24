package main

import (
	"512SvMan/api"
	"512SvMan/db"
	"512SvMan/env512"
	"512SvMan/logs512"
	"512SvMan/protocol"
	"512SvMan/services"
	"512SvMan/wireguard"
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	logger "github.com/Maruqes/512SvMan/logger"
	"google.golang.org/grpc"
)

func newSlave(addr, machineName string, conn *grpc.ClientConn) error {

	btrfsService := services.BTRFSService{}
	err := btrfsService.AutoMountRaid(machineName)
	if err != nil {
		logger.Errorf("UpdateNFS failed: %v", err)
		return err
	}

	logger.Info("Mounting all NFS")
	nfsService := services.NFSService{}
	err = nfsService.UpdateNFSShit()
	if err != nil {
		logger.Errorf("UpdateNFS failed: %v", err)
		return err
	}

	time.Sleep(time.Second * 15)

	logger.Info("Auto starting vms")
	virshServices := services.VirshService{}
	err = virshServices.StartAutoStartVms()
	if err != nil {
		logger.Errorf("UpdateNFS failed: %v", err)
		return err
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

func wireguardNetworkCIDR() (string, error) {
	_, network, err := net.ParseCIDR(wireguard.ServerCIDRValue())
	if err != nil {
		return "", fmt.Errorf("parse wireguard network: %w", err)
	}
	return network.String(), nil
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

func setupFirewallD() error {
	if err := execCommand("firewall-cmd", "--permanent", "--add-service=http"); err != nil {
		return fmt.Errorf("failed to add http service: %w", err)
	}

	if err := execCommand("firewall-cmd", "--permanent", "--add-service=https"); err != nil {
		return fmt.Errorf("failed to add https service: %w", err)
	}

	if err := execCommand("firewall-cmd", "--reload"); err != nil {
		return fmt.Errorf("failed to reload firewall: %w", err)
	}

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

	networkCIDR, err := wireguardNetworkCIDR()
	if err != nil {
		return err
	}

	// Open our wireguard port (default 51512/udp) in firewalld
	if err := execCommand("firewall-cmd", "--permanent", "--add-port=51512/udp"); err != nil {
		return fmt.Errorf("failed to add WireGuard UDP port: %w", err)
	}

	if err := execCommand("firewall-cmd", "--permanent", "--add-port=51512/tcp"); err != nil {
		return fmt.Errorf("failed to add WireGuard TCP port: %w", err)
	}

	if err := execCommand("firewall-cmd", "--permanent", "--add-masquerade"); err != nil {
		return fmt.Errorf("failed to enable masquerade: %w", err)
	}

	richRule := fmt.Sprintf("rule family=\"ipv4\" source address=\"%s\" masquerade", networkCIDR)
	if err := execCommand("firewall-cmd", "--permanent", "--zone=public", "--add-rich-rule", richRule); err != nil {
		return fmt.Errorf("failed to add WireGuard rich rule: %w", err)
	}

	if err := execCommand("firewall-cmd", "--reload"); err != nil {
		return fmt.Errorf("failed to reload firewall: %w", err)
	}

	fmt.Println("WireGuard ready with firewall and routing configured.")
	return nil
}

func main() {
	askForSudo()

	err := setupFirewallD()
	if err != nil {
		panic(err)
	}

	if err := env512.Setup(); err != nil {
		log.Fatalf("env setup: %v", err)
	}
	logger.SetType(env512.Mode)

	//check if novnc folder exists, if not download it
	err = downloadNoVNC()
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

	db.InitDB()
	err = db.CreateWireguardPeerTable()
	if err != nil {
		log.Fatalf("create wireguard peer table: %v", err)
	}

	if err := wireguard.AutoStartVPN(); err != nil {
		log.Fatalf("start wireguard vpn: %v", err)
	}

	//create all tables if not exists
	err = db.CreateNFSTable()
	if err != nil {
		log.Fatalf("create NFS table: %v", err)
	}

	err = db.CreateVmLiveTable()
	if err != nil {
		log.Fatalf("create vm_live table: %v", err)
	}

	err = db.CreateLogsTable()
	if err != nil {
		log.Fatalf("create logs table: %v", err)
	}

	err = db.CreateISOTable()
	if err != nil {
		log.Fatalf("create ISO table: %v", err)
	}

	err = db.CreateCPUSnapshotsTable()
	if err != nil {
		log.Fatalf("create cpu snapshots table: %v", err)
	}

	err = db.CreateMemSnapshotsTable()
	if err != nil {
		log.Fatalf("create mem snapshots table: %v", err)
	}

	err = db.CreateDiskSnapshotsTable()
	if err != nil {
		log.Fatalf("create disk snapshots table: %v", err)
	}

	err = db.CreateNetworkSnapshotsTable()
	if err != nil {
		log.Fatalf("create network snapshots table: %v", err)
	}

	err = db.CreateStreamDailyMetricsTable()
	if err != nil {
		log.Fatalf("create stream metrics table: %v", err)
	}

	err = db.InitSmartDiskDB()
	if err != nil {
		log.Fatalf("create stream metrics table: %v", err)
	}

	infoCollector := &services.InfoService{}
	go infoCollector.GetSlaveData()

	err = db.CreateTableBackups()
	if err != nil {
		log.Fatalf("create backups table: %v", err)
	}

	err = db.CreateTableAutoStart()
	if err != nil {
		log.Fatalf("create autostart table: %v", err)
	}

	err = db.CreateTableAutomaticBackup()
	if err != nil {
		log.Fatalf("create autostart table: %v", err)
	}

	err = db.CreateBtrfsTable()
	if err != nil {
		log.Fatalf("create btrfs auto table: %v", err)
	}

	err = db.DbCreatePushSubscriptionsTable()
	if err != nil {
		log.Fatalf("create btrfs auto table: %v", err)
	}

	if err := setupFrontendContainer(); err != nil {
		fmt.Fprintf(os.Stderr, "erro a preparar frontend container: %v\n", err)
		os.Exit(1)
	}
	logger.Info("Frontend container ready at http://localhost:" + hostPort)

	//listen and connects to gRPC
	logger.SetCallBack(logs512.LoggerCallBack)
	go protocol.PingAllSlavesLoop()
	protocol.ListenGRPC(newSlave)

	virshService := services.VirshService{}
	virshService.LoopAutomaticBaks()
	smartDiskService := services.SmartDiskService{}
	smartDiskService.DoAutomaticTest()

	api.StartApi()

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
