package main

import (
	"fmt"
	"log"
	"os"
	"slave/env512"
	"slave/logs512"
	"slave/nfs"
	"slave/protocol"

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

	// services, err := info.ServicesInfo.GetServices()
	// if err != nil {
	// 	fmt.Println("Error:", err)
	// 	return
	// }

	// for _, service := range services {
	// 	fmt.Printf("Name: %s, Description: %s, LoadState: %s, ActiveState: %s, SubState: %s\n",
	// 		service.Name, service.Description, service.LoadState, service.ActiveState, service.SubState)
	// }

	// return
	if err := env512.Setup(); err != nil {
		log.Fatalf("env setup: %v", err)
	}

	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)

	askForSudo()
	err := nfs.InstallNFS()
	if err != nil {
		log.Fatalf("failed to install NFS: %v", err)
	}
	conn := protocol.ConnectGRPC()
	defer conn.Close()
	select {}
}
