package main

import (
	"fmt"
	"log"
	"os"
	"slave/env512"
	"slave/nfs"
	"slave/protocol"

	logger "github.com/Maruqes/512SvMan/logger"
)

func askForSudo() {
	//if current program is not sudo terminate
	if os.Geteuid() != 0 {
		fmt.Println("This program needs to be run as root.")
		os.Exit(0)
	}
}

func main() {

	if err := env512.Setup(); err != nil {
		log.Fatalf("env setup: %v", err)
	}

	logger.SetType(env512.Mode)

	askForSudo()
	err := nfs.InstallNFS()
	if err != nil {
		log.Fatalf("failed to install NFS: %v", err)
	}
	conn := protocol.ConnectGRPC()
	defer conn.Close()
	select {}
}
