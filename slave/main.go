package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
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

/*
INSTALL THINGS THAT ARE NEEDED TO THE FULL APP FUNCTIONALITY
sudo dnf install -y xmlstarlet
*/
func setupAll() error {
	// Install xmlstarlet
	if err := exec.Command("dnf", "install", "-y", "xmlstarlet").Run(); err != nil {
		return fmt.Errorf("failed to install xmlstarlet: %w", err)
	}

	return nil
}

func main() {
	askForSudo()
	// err := virsh.MigrateVM(virsh.MigrateOptions{
	// 	ConnURI: "qemu:///system",
	// 	Name:    "testvm",
	// 	DestURI: "qemu+ssh://root@192.168.1.125:22/system",
	// 	Live:    false,
	// 	SSH: virsh.SSHOptions{
	// 		IdentityFile:       "/root/.ssh/id_rsa_512svman",
	// 		SkipHostKeyCheck:   true,
	// 		UserKnownHostsFile: "/dev/null",
	// 	},
	// })

	// if err != nil {
	// 	log.Fatalf("failed to migrate VM: %v", err)
	// }
	// return

	//build vm CreateVMCustomCPU

	// _, err := virsh.CreateVMCustomCPU("qemu:///system", "testvm", 2048, 2, "/mnt/512SvMan/shared/slave1_ola/testvm.qcow2", 20, "/mnt/512SvMan/shared/slave1_ola/tiny.iso", "", "default", "", "", []string{
	// 	"mpx",
	// 	"rfds-no",
	// 	"rsba",
	// 	"sgx",
	// })
	// if err != nil {
	// 	log.Fatalf("failed to create VM: %v", err)
	// }
	// return

	if err := setupAll(); err != nil {
		log.Fatalf("setup all: %v", err)
	}

	if err := env512.Setup(); err != nil {
		log.Fatalf("env setup: %v", err)
	}

	if err := virsh.SetVNCPorts(env512.VNC_MIN_PORT, env512.VNC_MAX_PORT); err != nil {
		log.Fatalf("set vnc ports: %v", err)
	}

	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	if err := nfs.InstallNFS(); err != nil {
		log.Fatalf("failed to install NFS: %v", err)
	}

	conn := protocol.ConnectGRPC()
	defer conn.Close()
	select {}
}
