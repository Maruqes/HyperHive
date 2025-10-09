package main

import (
	"fmt"
	"log"
	"os"
	"slave/env512"
	"slave/logs512"
	"slave/nfs"
	"slave/protocol"
	"slave/virsh"

	"github.com/Maruqes/512SvMan/logger"
)

func askForSudo() {
	//if current program is not sudo terminate
	if os.Geteuid() != 0 {
		fmt.Println("This program needs to be run as root.")
		os.Exit(0)
	}
}

func main() {
	// params := virsh.VMCreationParams{
	// 	ConnURI:        "qemu:///system",
	// 	Name:           "testvm",
	// 	MemoryMB:       2048,
	// 	VCPUs:          2,
	// 	DiskPath:       "testvm.qcow2",
	// 	DiskSizeGB:     10,
	// 	ISOPath:        "test.iso",
	// 	Machine:        "",
	// 	Network:        "default",
	// 	GraphicsListen: "127.0.0.1",
	// }
	// virsh.CreateVMHostPassthrough(params)

	// info, err := info.CPUInfo.GetCPUInfo()
	// if err != nil {
	// 	fmt.Println("Error:", err)
	// 	return
	// }
	// fmt.Printf("CPU Info: %+v\n", info.FeatureSet())

	// return

	askForSudo()

	if err := env512.Setup(); err != nil {
		log.Fatalf("env setup: %v", err)
	}

	if err := virsh.SetVNCPorts(env512.VNC_MIN_PORT, env512.VNC_MAX_PORT); err != nil {
		log.Fatalf("set vnc ports: %v", err)
	}

	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)

	err := nfs.InstallNFS()
	if err != nil {
		log.Fatalf("failed to install NFS: %v", err)
	}
	conn := protocol.ConnectGRPC()
	defer conn.Close()
	select {}
}
