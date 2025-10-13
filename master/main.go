package main

import (
	"512SvMan/api"
	"512SvMan/db"
	"512SvMan/env512"
	"512SvMan/logs512"
	"512SvMan/protocol"
	"512SvMan/services"
	"fmt"
	"log"
	"os"
	"os/exec"

	logger "github.com/Maruqes/512SvMan/logger"
	"google.golang.org/grpc"
)

func newSlave(addr, machineName string, conn *grpc.ClientConn) error {

	nfsService := services.NFSService{}
	err := nfsService.UpdateNFSShit()
	if err != nil {
		logger.Error("SyncSharedFolder failed: %v", err)
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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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

func main() {
	askForSudo()

	if err := env512.Setup(); err != nil {
		log.Fatalf("env setup: %v", err)
	}
	logger.SetType(env512.Mode)

	//check if novnc folder exists, if not download it
	err := downloadNoVNC()
	if err != nil {
		log.Fatalf("download noVNC: %v", err)
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

	//listen and connects to gRPC
	logger.SetCallBack(logs512.LoggerCallBack)
	protocol.ListenGRPC(newSlave)

	api.StartApi()

	// webServer()

	// xml, err := virsh.CreateVMCustomCPU(
	// 	"qemu:///system",
	// 	"debian-kde",
	// 	8192, 6,
	// 	"/mnt/data/debian-live-13.1.0-amd64-kde.iso", 50, // relativo -> /var/512SvMan/qcow2/debian-kde.qcow2
	// 	"/mnt/data/debian.qcow2", // relativo -> /var/512SvMan/iso/...
	// 	"",                                 // machine (user decide; "" = auto)
	// 	"default", "0.0.0.0",
	// 	"Westmere", nil, // baseline portable
	// )
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// fmt.Println("XML gravado em:", xml)

	select {}
}
