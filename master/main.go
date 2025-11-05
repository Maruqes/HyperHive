package main

import (
	"512SvMan/api"
	"512SvMan/db"
	"512SvMan/env512"
	"512SvMan/logs512"
	"512SvMan/protocol"
	"512SvMan/services"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	logger "github.com/Maruqes/512SvMan/logger"
	"google.golang.org/grpc"
)

func newSlave(addr, machineName string, conn *grpc.ClientConn) error {

	logger.Info("Mounting all NFS")
	nfsService := services.NFSService{}
	err := nfsService.UpdateNFSShit()
	if err != nil {
		logger.Error("UpdateNFS failed: %v", err)
		return err
	}

	time.Sleep(time.Second * 15)

	logger.Info("Auto starting vms")
	virshServices := services.VirshService{}
	err = virshServices.StartAutoStartVms()
	if err != nil {
		logger.Error("UpdateNFS failed: %v", err)
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
	if _, err := exec.LookPath("goaccess"); err != nil {
		if _, err := exec.LookPath("dnf"); err != nil {
			return fmt.Errorf("dnf package manager not found: %w", err)
		}

		fmt.Println("Installing GoAccess via dnf...")
		if err := execCommand("dnf", "-y", "install", "goaccess"); err != nil {
			return fmt.Errorf("install GoAccess: %w", err)
		}
	} else {
		fmt.Println("GoAccess already installed, ensuring configuration is up to date.")
	}

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

	db.InitDB()
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

	//listen and connects to gRPC
	logger.SetCallBack(logs512.LoggerCallBack)
	go protocol.PingAllSlavesLoop()
	protocol.ListenGRPC(newSlave)

	virshService := services.VirshService{}
	virshService.LoopAutomaticBaks()

	api.StartApi()

	select {}
}
