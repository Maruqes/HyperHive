package main

import (
	"fmt"
	"os"
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
	logger.SetType("dev")

	askForSudo()
	conn := protocol.ConnectGRPC()
	defer conn.Close()
	select {}
}
